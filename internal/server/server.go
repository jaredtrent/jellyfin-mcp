package server

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os/signal"
	"strings"
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
	includeAdminResources := resources.AdminResourcesEnabled(cfg.ReadOnly, cfg.Toolsets)

	tracker := &subscriptionTracker{}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "jellyfin",
		Title:   "Jellyfin Media Server",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions:       serverInstructions(),
		CompletionHandler:  completionHandler(client, includeAdminResources),
		SubscribeHandler:   subscribeHandler(tracker),
		UnsubscribeHandler: unsubscribeHandler(tracker),
	})

	// Add middleware for timing and MCP log notifications
	srv.AddReceivingMiddleware(timingAndLoggingMiddleware())

	// Build tool filter from CLI flags
	enabled := tools.BuildToolFilter(cfg.Toolsets, cfg.ReadOnly, cfg.DisableDestructive)

	// Register all components
	resources.RegisterResources(srv, client, includeAdminResources)
	prompts.RegisterPrompts(srv, client)
	tools.RegisterTools(srv, client, enabled)

	// Start resource subscription poller
	pollerCtx, pollerCancel := context.WithCancel(context.Background())
	defer pollerCancel()
	startResourcePoller(pollerCtx, srv, client, tracker)

	// Run via selected transport
	if cfg.HTTPMode {
		runHTTP(srv, cfg.HTTPAddr, cfg.HTTPToken)
	} else {
		runStdio(srv)
	}
}

func serverInstructions() string {
	now := time.Now()
	return fmt.Sprintf(`Current server date/time: %s (use this as "today" for any date calculations).

You are connected to a live Jellyfin media server through the tools below. This IS the user's media library — when they ask about movies, shows, music, what they've watched, what's unwatched, or want recommendations, USE THESE TOOLS to query their actual library. Never say you lack access or ask for credentials. Even if the user doesn't say "Jellyfin", any request about their media collection or viewing history should go through these tools. Always recommend from the user's own library — do NOT fetch external sites (IMDb, TMDB, etc.) for suggestions when the user's Jellyfin library can answer the question.

IMPORTANT — Safety rules for write operations:
- Before calling any tool that modifies data (metadata updates, user creation, library scans, subtitle downloads, playlist changes, image downloads), ALWAYS describe the intended action to the user and get explicit confirmation before proceeding.
- Destructive operations (delete, restart, shutdown, uninstall, batch operations) require confirm=true parameter. Without it, the tool returns a warning — present this to the user.

Key conventions:
- Most tools require item IDs (UUIDs). Use jellyfin_search or jellyfin_browse to discover items first, then pass their IDs to other tools.
- Genre/attribute filtering: use jellyfin_browse with genre, year, studio, is_played, min_community_rating, etc. — do NOT use jellyfin_search with genre names as keywords.
- Compact vs. detailed results: search, browse, recommendations, and analytics tools return compact item summaries (name, type, year, overview truncated to 200 chars, community rating, runtime, play status). Fields like genres, studios, cast/crew, provider IDs, taglines, and media stream details are only available via jellyfin_get_item. Do not assess metadata completeness from compact results — a missing field may simply not be included in the compact view.
- Troubleshoot issues: jellyfin_system_info (ping) → (activity_log, tasks, logs in parallel) → (log_file)
- Use prompts for guided multi-step workflows.

Resources (use for quick lookups without tool calls):
- jellyfin://server/info, jellyfin://libraries, jellyfin://sessions/now-playing, jellyfin://sessions, jellyfin://items/{itemId}, jellyfin://users/{userId}
- Dashboard: jellyfin://resume, jellyfin://next-up, jellyfin://favorites, jellyfin://latest, jellyfin://recently-played, jellyfin://users
- Per-library: jellyfin://libraries/{libraryId}/latest
- Reference guides: jellyfin://guides/transcoding, jellyfin://guides/file-naming, jellyfin://guides/remote-access, jellyfin://guides/troubleshooting, jellyfin://guides/library-setup, jellyfin://guides/docker, jellyfin://guides/users-and-access, jellyfin://guides/plugins, jellyfin://guides/migration, jellyfin://guides/performance

External links:
- When showing media details from jellyfin_get_item, always include the external_urls links (IMDb, TMDb, TVDB) so the user can navigate to the external page.`, now.Format("2006-01-02 15:04 MST"))
}

// runStdio runs the server over stdin/stdout. This is the original transport
// used by MetaMCP and Claude Desktop in subprocess mode.
func runStdio(server *mcp.Server) {
	transport := &mcp.StdioTransport{}
	if err := server.Run(context.Background(), transport); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runHTTP runs the server as a Streamable HTTP endpoint.
func runHTTP(server *mcp.Server, addr, token string) {
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 30 * time.Minute,
			Logger:         slog.Default(),
		},
	)

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

	srv := &http.Server{Addr: addr, Handler: mux}

	// Signal handling for graceful shutdown (HTTP-only — stdio must never have this)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown error: %v", err)
		}
	}()

	if err := validateHTTPAuth(addr, token); err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	if token == "" {
		log.Printf("WARNING: HTTP mode without --http-token — MCP endpoint has no authentication (localhost only)")
	}
	log.Printf("Jellyfin MCP server listening on %s (HTTP)", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		stop()
		log.Fatalf("server error: %v", err)
	}
	stop()
}

// completionHandler provides auto-completion for prompt arguments and resource template URIs.
func completionHandler(client jf.Client, includeAdmin bool) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	// Prompt argument completions
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
			"type": {"Movie", "Series", "Episode", "Audio"},
		},
		"parental-controls": {
			"max_rating": {"G", "PG", "PG-13", "TV-Y", "TV-G", "TV-PG", "TV-14"},
		},
		"codec-optimize": {
			"library": {},
		},
	}

	return func(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		ref := req.Params.Ref
		if ref == nil {
			return &mcp.CompleteResult{}, nil
		}

		var values []string

		switch ref.Type {
		case "ref/prompt":
			// Complete prompt arguments
			if argMap, ok := promptCompletions[ref.Name]; ok {
				if candidates, ok := argMap[req.Params.Argument.Name]; ok {
					values = candidates
				}
			}

		case "ref/resource":
			// Complete resource template variables
			uri := ref.URI
			switch {
			case strings.HasPrefix(uri, "jellyfin://libraries/"):
				var libs []map[string]any
				if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
					return &mcp.CompleteResult{}, nil
				}
				for _, lib := range libs {
					if id := jf.GetString(lib, "ItemId"); id != "" {
						values = append(values, id)
					}
				}
			case strings.HasPrefix(uri, "jellyfin://items/"):
				// Search for items matching the partial value
				partial := req.Params.Argument.Value
				if len(partial) >= 2 {
					userID, err := client.GetUserID(ctx)
					if err == nil {
						params := url.Values{
							"searchTerm": {partial},
							"Limit":      {"10"},
							"Recursive":  {"true"},
						}
						var result map[string]any
						endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
						if err := client.Get(ctx, endpoint, params, &result); err == nil {
							for _, raw := range jf.ToSlice(result["Items"]) {
								m := jf.ToMap(raw)
								if id := jf.GetString(m, "Id"); id != "" {
									values = append(values, id)
								}
							}
						}
					}
				}
			case strings.HasPrefix(uri, "jellyfin://users/"):
				if !includeAdmin {
					break
				}
				partial := strings.ToLower(req.Params.Argument.Value)
				var users []map[string]any
				if err := client.Get(ctx, "/Users", nil, &users); err == nil {
					for _, u := range users {
						name := jf.GetString(u, "Name")
						id := jf.GetString(u, "Id")
						if id != "" && (partial == "" || strings.Contains(strings.ToLower(name), partial)) {
							values = append(values, id)
						}
					}
				}
			}
		}

		// Filter by the partial value typed so far
		filtered := jf.FilterPrefix(values, req.Params.Argument.Value)
		// Ensure non-nil slice so JSON marshals to [] instead of null
		if filtered == nil {
			filtered = []string{}
		}

		return &mcp.CompleteResult{
			Completion: mcp.CompletionResultDetails{
				Values:  filtered,
				HasMore: false,
				Total:   len(filtered),
			},
		}, nil
	}
}
