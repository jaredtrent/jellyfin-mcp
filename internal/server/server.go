package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/prompts"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/resources"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

// version is set at build time via -ldflags.
var version = "dev"

// Config holds the CLI configuration passed from main.
type Config struct {
	Toolsets           string
	ReadOnly           bool
	DisableDestructive bool
	HTTPMode           bool
	HTTPAddr           string
	HTTPToken          string
}

// ToolsetNames returns the sorted list of available toolset names and their tools.
// Used by the CLI usage printer.
func ToolsetNames() map[string][]string {
	return tools.ToolsetMap
}

// Run initialises the MCP server and starts the selected transport.
func Run(cfg Config) {
	client := jf.NewJellyfinClient()
	for _, line := range timeZoneReport(os.Getenv("TZ"), time.Local, time.Now()) {
		log.Print(line)
	}
	go logServerVersion(client)

	tracker := &subscriptionTracker{}
	pollerCtx, pollerCancel := context.WithCancel(context.Background())
	defer pollerCancel()

	// Run via selected transport
	if cfg.HTTPMode {
		pool := newServerPool(cfg, client, tracker)
		startPoolResourcePoller(pollerCtx, pool, client, tracker, sessionPollInterval, contentPollInterval)
		runHTTPPool(pool, cfg.HTTPAddr, cfg.HTTPToken)
	} else {
		srv := newServer(cfg, client, tracker)
		startResourcePoller(pollerCtx, srv, client, tracker, sessionPollInterval, contentPollInterval)
		runStdio(srv)
	}
}

// newServer builds the MCP server with every resource, prompt, and enabled
// tool registered against client.
func newServer(cfg Config, client jf.Client, tracker *subscriptionTracker) *mcp.Server {
	if strings.TrimSpace(cfg.Toolsets) == "" && !cfg.ReadOnly {
		access, _ := tools.ResolveToolAccess("", false)
		return newServerWithAccess(cfg, access, client, tracker, jf.NewConfirmationLedger(elicitationTimeout))
	}
	access, unknown := tools.ResolveToolAccess(cfg.Toolsets, cfg.ReadOnly)
	tools.LogUnknownToolsets(unknown)
	return newServerWithAccess(cfg, access, client, tracker, jf.NewConfirmationLedger(elicitationTimeout))
}

func newServerWithAccess(cfg Config, access map[string]tools.ToolAccess, client jf.Client, tracker *subscriptionTracker, ledger *jf.ConfirmationLedger) *mcp.Server {
	effectiveRO := cfg.ReadOnly || !tools.HasWriteAccess(access)
	if ledger == nil {
		ledger = jf.NewConfirmationLedger(elicitationTimeout)
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:        "jellyfin",
		Title:       "Jellyfin Media Server",
		Description: "Search, play, and manage the media on a Jellyfin server",
		Version:     version,
		WebsiteURL:  "https://github.com/jaredtrent/jellyfin-mcp",
	}, &mcp.ServerOptions{
		// An explicit, empty capability set keeps the SDK from advertising
		// logging; tools, prompts, resources, and completions are inferred from
		// what is registered.
		Capabilities:       &mcp.ServerCapabilities{},
		SetCacheable:       setCacheable,
		CompletionHandler:  completionHandler(client),
		SubscribeHandler:   subscribeHandler(tracker),
		UnsubscribeHandler: unsubscribeHandler(tracker),
	})
	srv.AddReceivingMiddleware(
		timingMiddleware(),
		modeInstructionsMiddleware(time.Now, effectiveRO),
		plainErrors(),
		tools.DestructivePolicy(cfg.DisableDestructive),
		tools.ConfirmationLedgerMiddleware(ledger),
		tools.ConfirmationNote(),
	)
	srv.AddSendingMiddleware(boundElicitation(elicitationTimeout))
	resources.RegisterResources(srv, client)
	prompts.RegisterPrompts(srv, client)
	tools.RegisterTools(srv, client, tools.FilterFromAccess(access))
	return srv
}

type pooledEntry struct {
	server  *mcp.Server
	handler http.Handler
}

// serverPool caches MCP server and Streamable HTTP handler instances keyed by
// canonical effective tool access so HTTP /mcp?toolsets=...&read_only=...
// connections receive true server-side schema projections, isolated stateful
// session maps, and read-only instructions while strictly narrowing the
// operator's base CLI configuration.
type serverPool struct {
	baseCfg      Config
	baseAccess   map[string]tools.ToolAccess
	client       jf.Client
	tracker      *subscriptionTracker
	ledger       *jf.ConfirmationLedger
	defaultEntry pooledEntry

	mu      sync.Mutex
	entries map[string]pooledEntry
}

func newServerPool(cfg Config, client jf.Client, tracker *subscriptionTracker) *serverPool {
	baseAccess, unknown := tools.ResolveToolAccess(cfg.Toolsets, cfg.ReadOnly)
	tools.LogUnknownToolsets(unknown)
	defaultRO := cfg.ReadOnly || !tools.HasWriteAccess(baseAccess)
	ledger := jf.NewConfirmationLedger(elicitationTimeout)
	defaultSrv := newServerWithAccess(cfg, baseAccess, client, tracker, ledger)
	entry := pooledEntry{
		server:  defaultSrv,
		handler: newHTTPHandler(defaultSrv),
	}
	defaultKey := accessCacheKey(baseAccess, defaultRO)
	return &serverPool{
		baseCfg:      cfg,
		baseAccess:   baseAccess,
		client:       client,
		tracker:      tracker,
		ledger:       ledger,
		defaultEntry: entry,
		entries:      map[string]pooledEntry{defaultKey: entry},
	}
}

func accessCacheKey(access map[string]tools.ToolAccess, readOnly bool) string {
	entries := make([]string, 0, len(access))
	for name, acc := range access {
		if acc != tools.AccessNone {
			entries = append(entries, fmt.Sprintf("%s=%d", name, acc))
		}
	}
	sort.Strings(entries)
	return fmt.Sprintf("ro=%t;%s", readOnly, strings.Join(entries, ","))
}

// parseRawQueryScoping parses rawQuery strictly with url.ParseQuery (never
// discarding malformed percent-escapes or semicolons the way URL.Query does)
// and validates optional toolsets and read_only scoping parameters on /mcp.
// When hasScoping is false, the request uses the pool's default server. A
// non-empty errMsg indicates a 400 Bad Request.
func parseRawQueryScoping(rawQuery string) (hasScoping bool, queryAccess map[string]tools.ToolAccess, queryRO bool, errMsg string) {
	if rawQuery == "" {
		return false, nil, false, ""
	}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return true, nil, false, "invalid URL query string"
	}
	return parseQueryScoping(q)
}

func parseQueryScoping(q url.Values) (hasScoping bool, queryAccess map[string]tools.ToolAccess, queryRO bool, errMsg string) {
	if len(q) == 0 {
		return false, nil, false, ""
	}
	var unknownKeys []string
	for k := range q {
		if k != "read_only" && k != "toolsets" {
			unknownKeys = append(unknownKeys, k)
		}
	}
	if len(unknownKeys) > 0 {
		sort.Strings(unknownKeys)
		return true, nil, false, fmt.Sprintf("unsupported query parameter %q on /mcp (allowed: toolsets, read_only)", unknownKeys[0])
	}
	valsRO, hasRO := q["read_only"]
	valsTS, hasTS := q["toolsets"]
	if !hasRO && !hasTS {
		return false, nil, false, ""
	}
	if hasRO {
		if len(valsRO) != 1 {
			return true, nil, false, "invalid read_only query parameter"
		}
		switch strings.ToLower(strings.TrimSpace(valsRO[0])) {
		case "true", "1":
			queryRO = true
		case "false", "0":
			queryRO = false
		default:
			return true, nil, false, fmt.Sprintf("invalid read_only query parameter %q (use true, false, 1, or 0)", valsRO[0])
		}
	}

	rawToolsets := ""
	if hasTS {
		var parts []string
		for _, v := range valsTS {
			for part := range strings.SplitSeq(v, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					parts = append(parts, p)
				}
			}
		}
		if len(parts) == 0 {
			return true, nil, false, "toolsets query parameter must name at least one valid toolset"
		}
		rawToolsets = strings.Join(parts, ",")
	}

	access, unknown := tools.ResolveToolAccess(rawToolsets, queryRO)
	if len(unknown) > 0 {
		return true, nil, false, fmt.Sprintf("unknown toolset %q in toolsets query parameter", unknown[0])
	}
	return true, access, queryRO, ""
}

func (p *serverPool) entryForRequest(r *http.Request) (pooledEntry, string) {
	hasScoping, queryAccess, queryRO, errMsg := parseRawQueryScoping(r.URL.RawQuery)
	if errMsg != "" {
		return pooledEntry{}, errMsg
	}
	if !hasScoping {
		return p.defaultEntry, ""
	}

	narrowed := tools.NarrowToolAccess(p.baseAccess, queryAccess)
	effectiveRO := p.baseCfg.ReadOnly || queryRO || !tools.HasWriteAccess(narrowed)
	key := accessCacheKey(narrowed, effectiveRO)

	p.mu.Lock()
	if entry, ok := p.entries[key]; ok {
		p.mu.Unlock()
		return entry, ""
	}
	p.mu.Unlock()

	narrowCfg := p.baseCfg
	narrowCfg.ReadOnly = effectiveRO
	srv := newServerWithAccess(narrowCfg, narrowed, p.client, p.tracker, p.ledger)
	built := pooledEntry{
		server:  srv,
		handler: newHTTPHandler(srv),
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.entries[key]; ok {
		return entry, ""
	}
	p.entries[key] = built
	return built, ""
}

func (p *serverPool) forRequest(r *http.Request) *mcp.Server {
	entry, _ := p.entryForRequest(r)
	return entry.server
}

func (p *serverPool) allServers() []*mcp.Server {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*mcp.Server, 0, len(p.entries))
	for _, entry := range p.entries {
		out = append(out, entry.server)
	}
	return out
}

func (p *serverPool) totalSessions() int {
	total := 0
	for _, srv := range p.allServers() {
		for range srv.Sessions() {
			total++
		}
	}
	return total
}

// staticTTL is how long a client may cache what does not change while the
// server runs: the lists of tools, prompts, and resources, and the guides.
const staticTTL = time.Hour

// setCacheable sets the cache hints of list and resource results. The lists
// and the guides are fixed for the life of the process and the same for every
// client. Every other resource reads live data as the configured user, so it
// is private and never fresh.
func setCacheable(_ context.Context, req mcp.Request, c *mcp.Cacheable) {
	switch r := req.(type) {
	case *mcp.ListToolsRequest, *mcp.ListPromptsRequest, *mcp.ListResourcesRequest, *mcp.ListResourceTemplatesRequest:
		c.TTLMs, c.CacheScope = int(staticTTL.Milliseconds()), "public"
	case *mcp.ReadResourceRequest:
		if strings.HasPrefix(r.Params.URI, "jellyfin://guides/") {
			c.TTLMs, c.CacheScope = int(staticTTL.Milliseconds()), "public"
		} else {
			c.TTLMs, c.CacheScope = 0, "private"
		}
	}
}

// logServerVersion reports the connected Jellyfin version on stderr and warns
// when it is older than jf.MinSupportedVersion. Run calls it in a goroutine,
// so an unreachable or slow server never delays or stops startup.
func logServerVersion(client jf.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, err := client.ServerVersion(ctx)
	switch {
	case err != nil:
		log.Printf("WARNING: could not reach the Jellyfin server or read its version, so the version is unknown: %v", err)
	case !v.Supported():
		log.Printf("WARNING: Jellyfin %s is older than the minimum supported version %s; some tools may fail.", v, jf.MinSupportedVersion)
	default:
		log.Printf("Connected to Jellyfin %s", v)
	}
}

// timeZoneReport returns the startup lines that name the time zone the server
// states and reads dates in, given the TZ variable and the zone Go derived from
// it. Go replaces a TZ it cannot load with UTC and says nothing, so that case
// gets a warning.
func timeZoneReport(tz string, loc *time.Location, now time.Time) []string {
	var lines []string
	if name := strings.TrimPrefix(tz, ":"); name != "" && name != "UTC" && loc.String() == "UTC" {
		lines = append(lines, fmt.Sprintf("WARNING: TZ=%q is not a time zone this system knows, so dates are read and shown in UTC.", tz))
	}
	return append(lines, "Time zone: "+now.In(loc).Format(zoneLayout))
}

// zoneLayout names a time zone by its abbreviation and its offset. The offset
// is what makes it unambiguous: IST and CST each name more than one zone, and
// some zones have only a numeric abbreviation.
const zoneLayout = "MST (UTC-07:00)"

// serverInstructions returns the instructions the server gives a client that
// connects at now.
func serverInstructions(now time.Time, readOnly bool) string {
	safetySection := `Write safety:
- Gated actions (named in a tool's confirm field): once target IDs are known (search/browse first; never guess IDs), call WITHOUT confirm instead of asking in plain text first, and never set confirm to true before CONFIRMATION REQUIRED. Form-elicitation clients prompt the user directly; other clients return CONFIRMATION REQUIRED—present that server preview to the user, and set confirm to true only on a follow-up turn after they approve it.
- Ungated writes (creating users/playlists/collections/timers, scans, non-replacing refreshes, single subtitle/lyric/image downloads or uploads, adding playlist/collection items): describe the change and get explicit user agreement before calling.`
	if readOnly {
		safetySection = `Read-only mode:
- Only read-only tools and actions are enabled on this connection, so all write, edit, delete, scan, and power actions are omitted and disabled. Fulfill any read-only parts of the user's request once, then clearly state that the requested write or destructive actions cannot be performed because read-only mode is enabled.`
	}
	return fmt.Sprintf(`Server date/time when this connection began: %s. Dates such as 2024-01-31 in tool inputs are read in this time zone, except min_premiere_date and max_premiere_date, which Jellyfin reads in UTC, and tool results give dates and times in it. Use this date as "today" for date calculations unless you know a more current one.

You are connected to the user's live Jellyfin media server. Always query these tools for movies, shows, music, watch history, or recommendations (even when the user doesn't mention "Jellyfin"), recommend from their library rather than external sites, and never claim you lack access or ask for credentials.

%s`, now.Format("2006-01-02 15:04 "+zoneLayout), safetySection) + `

Conventions:
- Discovery & IDs: Use jellyfin_search (local title/person/studio/genre lookup) or jellyfin_browse (structured filters such as genre, year, resolution, is_played, min_community_rating—never put genres or resolutions into jellyfin_search). parent_id accepts library names or IDs; series_id, playlist_id, and collection_id accept names or IDs; user_id accepts usernames or IDs; session_id accepts session IDs or device names.
- Compact vs full item data: Search, browse, recommendations, and analytics return compact summaries (overview is included in search/browse when present, truncated to 200 chars). Call jellyfin_get_item for genres, studios, cast/crew, provider IDs, media streams, and external_urls (always include IMDb/TMDb/TVDB links when presenting jellyfin_get_item details).
- History & play state: Use jellyfin_playback_stats (playback_history for verified plays, played_status for per-user watch state, most_played/unwatched_by_all for household stats), or jellyfin_browse (is_played=true, sort_by=DatePlayed); pass user_id (username or ID) for another user. played=true means finished at least once or marked played; progress is the latest resume position.
- Downloads, space & diagnostics: Use jellyfin_item_extras action=download_link for web download links (never invent URLs). Use jellyfin_analytics (library_size, size_report) for media sizes and jellyfin_system_info (storage) for free disk space. Troubleshoot with jellyfin_system_info (health_check, activity_log, logs/log_file) and jellyfin_server (list_tasks).

Resources: Read jellyfin:// live resources (server, libraries, sessions, playback queues, users, items) and jellyfin://guides/<topic> reference guides when your client supports MCP resources.`
}

// runStdio runs the server over stdin/stdout, the subprocess transport MCP
// clients such as Claude Desktop use.
func runStdio(server *mcp.Server) {
	if err := server.Run(context.Background(), newStdioTransport()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// newStdioTransport serves MCP over stdin and stdout. A message larger than
// tools.MaxMessageBytes ends the session.
func newStdioTransport() *mcp.StdioTransport {
	return &mcp.StdioTransport{MaxLineLength: tools.MaxMessageBytes}
}

// statelessProtocolVersion is the first MCP protocol version whose clients
// send each request on its own, with no session. Protocol versions are dates,
// so they compare as strings.
const statelessProtocolVersion = "2026-07-28"

// newHTTPHandler serves MCP over Streamable HTTP to clients of both protocol
// eras. A request whose MCP-Protocol-Version header is statelessProtocolVersion
// or later goes to a stateless handler, as the SDK requires for those versions.
// Every other request, including a legacy initialize, which carries no header,
// goes to a stateful handler that keeps the session an older client needs for
// elicitation and resource subscriptions. Both serve the same server and refuse
// a request body larger than tools.MaxMessageBytes with 413.
func newHTTPHandler(server *mcp.Server) http.Handler {
	getServer := func(*http.Request) *mcp.Server { return server }
	stateful := mcp.NewStreamableHTTPHandler(getServer, httpOptions(false))
	stateless := mcp.NewStreamableHTTPHandler(getServer, httpOptions(true))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("MCP-Protocol-Version") >= statelessProtocolVersion {
			stateless.ServeHTTP(w, r)
			return
		}
		stateful.ServeHTTP(w, r)
	})
}

func newHTTPPoolHandler(pool *serverPool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry, errMsg := pool.entryForRequest(r)
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		entry.handler.ServeHTTP(w, r)
	})
}

// httpOptions configures the stateless or the stateful handler.
func httpOptions(stateless bool) *mcp.StreamableHTTPOptions {
	opts := &mcp.StreamableHTTPOptions{
		Logger:              slog.Default(),
		MaxRequestBodyBytes: tools.MaxMessageBytes,
	}
	if stateless {
		opts.Stateless = true
		// The whole life of a stateless request is its POST, so a tool call
		// stops when its client goes away.
		opts.PropagateRequestCancellation = true
	} else {
		opts.SessionTimeout = 30 * time.Minute
	}
	return opts
}

// largeBodyBytes is the request size from which a request counts against
// maxLargeRequests. A message this large is an upload, and each one in flight
// costs several times its size in memory while it is parsed and decoded.
const largeBodyBytes = 1 << 20

// maxLargeRequests is how many large requests may be in flight at once. The
// rest get 503 with Retry-After, which keeps a burst of uploads from taking the
// process past a container's memory limit.
const maxLargeRequests = 2

// maxSessions is how many stateful sessions may exist at once. A legacy
// initialize past this gets 503. Each session lives until its client closes
// it or 30 minutes pass, so an unbounded number could exhaust memory.
const maxSessions = 256

// limitLargeBodies admits at most limit requests of largeBodyBytes or more, or
// of unknown length, at a time.
func limitLargeBodies(next http.Handler, limit int) http.Handler {
	slots := make(chan struct{}, limit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength >= 0 && r.ContentLength < largeBodyBytes {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "2")
			http.Error(w, "too many large requests in flight", http.StatusServiceUnavailable)
		}
	})
}

// limitSessions refuses a request that would create a stateful session once
// the server has limit sessions. Such a request is a POST without a session
// header from a client on a protocol before statelessProtocolVersion; every
// other request either belongs to an existing session or creates none.
func limitSessions(next http.Handler, server *mcp.Server, limit int) http.Handler {
	return limitSessionCount(next, func() int {
		n := 0
		for range server.Sessions() {
			n++
		}
		return n
	}, limit)
}

func limitPoolSessions(next http.Handler, pool *serverPool, limit int) http.Handler {
	return limitSessionCount(next, pool.totalSessions, limit)
}

func limitSessionCount(next http.Handler, countSessions func() int, limit int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("Mcp-Session-Id") == "" && r.Header.Get("MCP-Protocol-Version") < statelessProtocolVersion {
			if countSessions() >= limit {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many sessions", http.StatusServiceUnavailable)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// newHTTPMux serves the MCP endpoint at /mcp, behind the bearer token when one
// is set and behind cross-origin protection, and an unauthenticated health
// check at /health.
func newHTTPMux(server *mcp.Server, token string) http.Handler {
	return buildHTTPMux(limitSessions(limitLargeBodies(newHTTPHandler(server), maxLargeRequests), server, maxSessions), token)
}

func newHTTPPoolMux(pool *serverPool, token string) http.Handler {
	return buildHTTPMux(limitPoolSessions(limitLargeBodies(newHTTPPoolHandler(pool), maxLargeRequests), pool, maxSessions), token)
}

func buildHTTPMux(handler http.Handler, token string) http.Handler {
	// Defense-in-depth against cross-site tool execution (CVE-2026-33252): reject
	// browser cross-site POSTs by checking Origin / Sec-Fetch-Site. Non-browser
	// clients (MetaMCP, curl) send neither header and pass through unaffected.
	crossOrigin := http.NewCrossOriginProtection()

	mux := http.NewServeMux()
	mux.Handle("/mcp", bearerAuth(crossOrigin.Handler(handler), token))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// newHTTPServer builds the listener. The read timeouts keep a peer,
// authenticated or not, from holding a connection open by sending slowly:
// headers within 10 seconds, and the whole request within 5 minutes, which
// admits the largest message at about 110 KB/s. No write timeout, because a
// stateful session's event stream stays open for as long as the client
// listens.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
}

// runHTTPPool runs the pooled server as a Streamable HTTP endpoint.
func runHTTPPool(pool *serverPool, addr, token string) {
	serveHTTPMux(newHTTPPoolMux(pool, token), addr, token)
}

func serveHTTPMux(mux http.Handler, addr, token string) {
	srv := newHTTPServer(addr, mux)

	// Signal handling for graceful shutdown (HTTP only; stdio must never have this)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown error: %v", err)
		}
	}()

	if token == "" && !isLoopbackAddr(addr) {
		log.Fatalf("FATAL: an HTTP token (--http-token or HTTP_TOKEN) is required when listening on non-localhost address %s", addr)
	}
	if token == "" {
		log.Printf("WARNING: HTTP mode without a token. The MCP endpoint has no authentication, so it listens on localhost only.")
	}
	log.Printf("Jellyfin MCP server listening on %s (HTTP)", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		stop()
		log.Fatalf("server error: %v", err)
	}
	stop()
}

// isLoopbackAddr reports whether a listen address binds only the loopback
// interface: the host is "localhost" or a loopback IP such as 127.0.0.1 or ::1.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// bearerAuth wraps an http.Handler with bearer token authentication.
// If token is empty, the handler is returned unwrapped (no auth). A refused
// request gets the challenge RFC 6750 describes: the realm alone when it
// presents no bearer token, and an invalid_token error when its token is wrong.
func bearerAuth(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, credentials, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jellyfin-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(credentials), expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jellyfin-mcp", error="invalid_token"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// completionHandler provides auto-completion for prompt arguments and resource template URIs.
func completionHandler(client jf.Client) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	// Fixed vocabularies for prompt arguments. A "library" or "user" argument
	// completes from the server instead.
	promptCompletions := map[string]map[string][]string{
		"find-and-play": {
			"type": {"Movie", "Series", "Episode", "Audio", "MusicAlbum"},
		},
		"movie-night": {
			"genre": {"Action", "Comedy", "Drama", "Horror", "Sci-Fi", "Thriller", "Romance", "Documentary", "Animation", "Fantasy"},
			"mood":  {"relaxing", "exciting", "thought-provoking", "funny", "scary"},
		},
		"music-listen": {
			"type": {"artist", "album", "song", "genre", "playlist"},
		},
		"fix-subtitles": {
			"language": {"en", "es", "fr", "de", "ja", "pt", "it", "zh", "ko", "ru"},
		},
		"bulk-metadata-fix": {
			"issue": {"missing_overview", "wrong_year", "missing_genres", "wrong_title", "re_identify"},
		},
		"subtitle-audit": {
			"language": {"en", "es", "fr", "de", "ja", "pt", "it", "zh", "ko", "ru"},
		},
		"duplicate-finder": {
			"type": {"Movie", "Series"},
		},
		"parental-controls": {
			"max_rating": {"G", "PG", "PG-13", "TV-Y", "TV-G", "TV-PG", "TV-14"},
		},
	}

	// libraries returns each library's ItemId or Name. A failed request
	// completes nothing, as the protocol asks, rather than failing the
	// completion.
	libraries := func(ctx context.Context, field string) []string {
		var libs []map[string]any
		if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
			return nil
		}
		var values []string
		for _, lib := range libs {
			if v := jf.GetString(lib, field); v != "" {
				values = append(values, v)
			}
		}
		return values
	}

	return func(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		ref := req.Params.Ref
		// The SDK rejects a request without a ref unless
		// MCPGODEBUG=disablecompleteparamsvalidation=1 lets it through.
		if ref == nil {
			return completionResult(nil, ""), nil
		}

		partial := req.Params.Argument.Value
		var values []string

		switch ref.Type {
		case "ref/prompt":
			switch req.Params.Argument.Name {
			case "library":
				// Every prompt that takes a library takes it by name.
				values = libraries(ctx, "Name")
			case "user":
				// watch-history takes a username.
				var users []map[string]any
				if err := client.Get(ctx, "/Users", nil, &users); err == nil {
					for _, u := range users {
						if name := jf.GetString(u, "Name"); name != "" {
							values = append(values, name)
						}
					}
				}
			default:
				if argMap, ok := promptCompletions[ref.Name]; ok {
					values = argMap[req.Params.Argument.Name]
				}
			}

		case "ref/resource":
			// Complete resource template variables. The item and user
			// lookups match the typed text against names on the server and
			// return IDs, which the typed text never prefixes, so their
			// results are returned as matched.
			uri := ref.URI
			switch {
			case strings.HasPrefix(uri, "jellyfin://libraries/"):
				values = libraries(ctx, "ItemId")
			case strings.HasPrefix(uri, "jellyfin://items/"):
				if len(partial) < 2 {
					break
				}
				userID, err := client.GetUserID(ctx)
				if err != nil {
					break
				}
				params := url.Values{
					"UserId":     {userID},
					"searchTerm": {partial},
					"Limit":      {"10"},
					"Recursive":  {"true"},
				}
				var result map[string]any
				if err := client.Get(ctx, "/Items", params, &result); err != nil {
					break
				}
				for _, raw := range jf.ToSlice(result["Items"]) {
					if id := jf.GetString(jf.ToMap(raw), "Id"); id != "" {
						values = append(values, id)
					}
				}
				return completionResult(values, ""), nil
			case strings.HasPrefix(uri, "jellyfin://users/"):
				lower := strings.ToLower(partial)
				var users []map[string]any
				if err := client.Get(ctx, "/Users", nil, &users); err == nil {
					for _, u := range users {
						name := jf.GetString(u, "Name")
						id := jf.GetString(u, "Id")
						if id != "" && strings.Contains(strings.ToLower(name), lower) {
							values = append(values, id)
						}
					}
				}
				return completionResult(values, ""), nil
			}
		}

		return completionResult(values, partial), nil
	}
}

// completionResult keeps the candidates that start with the partial value
// typed so far. Values is always a JSON array, never null, because the
// protocol requires an array even when nothing matches.
func completionResult(candidates []string, partial string) *mcp.CompleteResult {
	values := jf.FilterPrefix(candidates, partial)
	if values == nil {
		values = []string{}
	}
	return &mcp.CompleteResult{
		Completion: mcp.CompletionResultDetails{
			Values: values,
			Total:  len(values),
		},
	}
}
