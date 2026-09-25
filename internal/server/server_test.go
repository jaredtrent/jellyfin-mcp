package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

// fakeClient implements the jf.Client methods these tests exercise; calling
// any other method panics through the nil embedded interface.
type fakeClient struct {
	jf.Client
	get func(endpoint string, params url.Values, dest any) error
}

func (f fakeClient) Get(_ context.Context, endpoint string, params url.Values, dest any) error {
	return f.get(endpoint, params, dest)
}

func (f fakeClient) GetUserID(context.Context) (string, error) { return "user-1", nil }

// TestMain runs the package's tests in UTC+9, a fixed zone away from UTC, so
// tests that go through the server's own clock see which zone it states
// whatever machine runs them. The zone is set before any server starts:
// changing time.Local while a server goroutine reads the clock is a data race.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC+9", 9*60*60)
	os.Exit(m.Run())
}

func TestCompletionValuesAreNeverNull(t *testing.T) {
	failing := fakeClient{get: func(string, url.Values, any) error { return errors.New("unreachable") }}
	handler := completionHandler(failing)

	tests := []struct {
		name   string
		params *mcp.CompleteParams
	}{
		{name: "no reference", params: &mcp.CompleteParams{Argument: mcp.CompleteParamsArgument{Name: "x"}}},
		{name: "library lookup fails", params: &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: "jellyfin://libraries/{libraryId}/latest"},
			Argument: mcp.CompleteParamsArgument{Name: "libraryId"},
		}},
		{name: "unknown prompt", params: &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "no-such-prompt"},
			Argument: mcp.CompleteParamsArgument{Name: "x"},
		}},
		{name: "no candidate matches", params: &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "movie-night"},
			Argument: mcp.CompleteParamsArgument{Name: "genre", Value: "zzz"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handler(context.Background(), &mcp.CompleteRequest{Params: tt.params})
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"values":[]`) {
				t.Errorf("result %s, want an empty values array", b)
			}
		})
	}
}

// The item and user resource lookups match names on the server and return
// IDs, so the typed text must not be applied to the IDs as a prefix.
func TestCompletionNameLookupsReturnIDs(t *testing.T) {
	client := fakeClient{get: func(endpoint string, params url.Values, dest any) error {
		switch endpoint {
		case "/Users":
			return jsonInto([]map[string]any{
				{"Id": "u-harper", "Name": "Harper"},
				{"Id": "u-mika", "Name": "Mika"},
			}, dest)
		case "/Items":
			if params.Get("searchTerm") != "neon" {
				t.Errorf("searchTerm = %q, want neon", params.Get("searchTerm"))
			}
			return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "i-neon-cascade"}}}, dest)
		case "/Library/VirtualFolders":
			return jsonInto([]map[string]any{
				{"ItemId": "lib-1", "Name": "Movies"},
				{"ItemId": "lib-2", "Name": "Shows"},
			}, dest)
		}
		return errors.New("unexpected endpoint " + endpoint)
	}}
	handler := completionHandler(client)

	tests := []struct {
		name   string
		params *mcp.CompleteParams
		want   []string
	}{
		{"user by name", &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: "jellyfin://users/{userId}"},
			Argument: mcp.CompleteParamsArgument{Name: "userId", Value: "har"},
		}, []string{"u-harper"}},
		{"item by name", &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: "jellyfin://items/{itemId}"},
			Argument: mcp.CompleteParamsArgument{Name: "itemId", Value: "neon"},
		}, []string{"i-neon-cascade"}},
		{"library id by prefix", &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: "jellyfin://libraries/{libraryId}/latest"},
			Argument: mcp.CompleteParamsArgument{Name: "libraryId", Value: "lib-2"},
		}, []string{"lib-2"}},
		{"prompt library by name", &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "codec-optimize"},
			Argument: mcp.CompleteParamsArgument{Name: "library", Value: "sh"},
		}, []string{"Shows"}},
		{"prompt username", &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "watch-history"},
			Argument: mcp.CompleteParamsArgument{Name: "user", Value: "mi"},
		}, []string{"Mika"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handler(context.Background(), &mcp.CompleteRequest{Params: tt.params})
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Completion.Values; !slices.Equal(got, tt.want) {
				t.Errorf("completion = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletionFiltersByPrefix(t *testing.T) {
	handler := completionHandler(fakeClient{})
	res, err := handler(context.Background(), &mcp.CompleteRequest{Params: &mcp.CompleteParams{
		Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "movie-night"},
		Argument: mcp.CompleteParamsArgument{Name: "mood", Value: "ex"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Completion.Values; len(got) != 1 || got[0] != "exciting" || res.Completion.Total != 1 {
		t.Errorf("completion = %+v, want [exciting]", res.Completion)
	}
}

// initializeOfSize returns an initialize request of exactly size bytes, padded
// through the client name.
func initializeOfSize(id, size int) string {
	prefix := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"version":"0","name":"`
	suffix := `"}}}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

// discoverOfSize returns a server/discover request of the stateless protocol
// of exactly size bytes, padded through the client name.
func discoverOfSize(id, size int) string {
	prefix := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"server/discover","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"` + statelessProtocolVersion + `",` +
		`"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"version":"0","name":"`
	suffix := `"}}}}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

// postMCP sends body to handler as an MCP POST. When version is set, the
// request carries the headers of that protocol version for method.
func postMCP(handler http.Handler, version, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
		req.Header.Set("Mcp-Method", method)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The HTTP endpoint reads a request of up to tools.MaxMessageBytes, well past
// the SDK's 4 MiB default, and refuses a larger one with 413, in both
// protocol eras.
func TestHTTPHandlerRequestSizeLimit(t *testing.T) {
	handler := newHTTPHandler(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil))
	eras := []struct {
		version, method string
		request         func(id, size int) string
	}{
		{"", "initialize", initializeOfSize},
		{statelessProtocolVersion, "server/discover", discoverOfSize},
	}
	for _, era := range eras {
		for _, tt := range []struct {
			size int
			want int
		}{
			{tools.MaxMessageBytes, http.StatusOK},
			{tools.MaxMessageBytes + 1, http.StatusRequestEntityTooLarge},
		} {
			body := era.request(1, tt.size)
			rec := postMCP(handler, era.version, era.method, body)
			if rec.Code != tt.want || (tt.want == http.StatusOK && !strings.Contains(rec.Body.String(), `"result"`)) {
				t.Errorf("%q: %d-byte request: status %d, want %d: %.200s", era.version, len(body), rec.Code, tt.want, rec.Body.String())
			}
		}
	}
}

// newGatedServer serves one tool that performs its work only after the
// confirmation gate lets it through.
func newGatedServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	type input struct {
		Confirm *bool `json:"confirm,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "gated"}, func(ctx context.Context, req *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		if res := jf.ConfirmationGate(ctx, req, in.Confirm, "This will delete everything."); res != nil {
			return res, nil, nil
		}
		return jf.TextResult("done"), nil, nil
	})
	return srv
}

// Clients of both protocol eras share one endpoint: a current client is
// served without a session, and an older one gets the session it needs to be
// asked for confirmation.
func TestHTTPHandlerServesBothEras(t *testing.T) {
	ts := httptest.NewServer(newHTTPHandler(newGatedServer()))
	t.Cleanup(ts.Close)
	for _, version := range []string{statelessProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			var elicited atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, &mcp.ClientOptions{
				ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					elicited.Add(1)
					return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
				},
			})
			cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: ts.URL}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cs.Close() })
			if got := cs.InitializeResult().ProtocolVersion; got != version {
				t.Fatalf("negotiated %q, want %q", got, version)
			}
			if stateless := version >= statelessProtocolVersion; (cs.ID() == "") != stateless {
				t.Errorf("session ID %q; want a session only for an older client", cs.ID())
			}
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "gated", Arguments: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Content) != 1 || res.Content[0].(*mcp.TextContent).Text != "done" || elicited.Load() != 1 {
				t.Errorf("result %+v after %d forms, want done after one", res.Content, elicited.Load())
			}
		})
	}
}

// A current client's tool call stops when its HTTP request is abandoned.
func TestHTTPHandlerCancelsAnAbandonedCall(t *testing.T) {
	started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "wait"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(stopped)
		case <-release:
		}
		return jf.TextResult("finished"), nil, nil
	})
	ts := httptest.NewServer(newHTTPHandler(srv))
	t.Cleanup(ts.Close)
	t.Cleanup(func() { close(release) })

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wait","arguments":{},"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"` + statelessProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{}}}}`
	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", statelessProtocolVersion)
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "wait")
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the tool was not called")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the tool kept running after its request was abandoned")
	}
}

// A request without the token is refused with the RFC 6750 challenge, which
// names the error only when a bearer token was presented and is wrong.
func TestBearerAuth(t *testing.T) {
	handler := bearerAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}), "secret")
	for _, tt := range []struct {
		header    string
		want      int
		challenge string
	}{
		{"", http.StatusUnauthorized, `Bearer realm="jellyfin-mcp"`},
		{"Basic c2VjcmV0", http.StatusUnauthorized, `Bearer realm="jellyfin-mcp"`},
		{"Bearer wrong", http.StatusUnauthorized, `Bearer realm="jellyfin-mcp", error="invalid_token"`},
		{"Bearer secret", http.StatusOK, ""},
		{"bearer secret", http.StatusOK, ""},
	} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tt.header != "" {
			req.Header.Set("Authorization", tt.header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tt.want || rec.Header().Get("WWW-Authenticate") != tt.challenge {
			t.Errorf("Authorization %q: status %d with challenge %q, want %d with %q",
				tt.header, rec.Code, rec.Header().Get("WWW-Authenticate"), tt.want, tt.challenge)
		}
	}
}

// Both protocol eras reach the MCP endpoint only with the token and only from
// the same origin, while the health check needs neither.
func TestHTTPMux(t *testing.T) {
	mux := newHTTPMux(newGatedServer(), "secret")
	eras := []struct {
		version, method, body string
	}{
		{"", "initialize", initializeOfSize(1, 1024)},
		{statelessProtocolVersion, "server/discover", discoverOfSize(1, 1024)},
	}
	for _, era := range eras {
		for _, tt := range []struct {
			name      string
			token     string
			crossSite bool
			want      int
		}{
			{"no token", "", false, http.StatusUnauthorized},
			{"token", "secret", false, http.StatusOK},
			{"cross-site", "secret", true, http.StatusForbidden},
		} {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(era.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if era.version != "" {
				req.Header.Set("MCP-Protocol-Version", era.version)
				req.Header.Set("Mcp-Method", era.method)
			}
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.crossSite {
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				req.Header.Set("Origin", "https://attacker.example")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("%s %q: status %d, want %d: %.200s", era.method, tt.name, rec.Code, tt.want, rec.Body.String())
			}
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("/health: status %d: %s", rec.Code, rec.Body.String())
	}
}

// A request of a protocol version later than any the server knows reaches the
// stateless handler, whose refusal offers the current version, rather than the
// stateful one, which would steer the client to a legacy session.
func TestHTTPHandlerRoutesLaterVersionsToStateless(t *testing.T) {
	handler := newHTTPHandler(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil))
	const later = "2099-01-01"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"` + later + `","io.modelcontextprotocol/clientCapabilities":{}}}}`
	rec := postMCP(handler, later, "tools/list", body)
	var resp struct {
		Error struct {
			Data struct{ Supported []string }
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !slices.Contains(resp.Error.Data.Supported, statelessProtocolVersion) {
		t.Errorf("response %d %s, want a refusal offering %s", rec.Code, rec.Body.String(), statelessProtocolVersion)
	}
}

// The stateful handler keeps an idle session for 30 minutes, and the
// stateless handler cancels a call its client abandons.
func TestHTTPOptions(t *testing.T) {
	if o := httpOptions(false); o.Stateless || o.SessionTimeout != 30*time.Minute || o.MaxRequestBodyBytes != tools.MaxMessageBytes {
		t.Errorf("stateful options = %+v", o)
	}
	if o := httpOptions(true); !o.Stateless || !o.PropagateRequestCancellation || o.MaxRequestBodyBytes != tools.MaxMessageBytes {
		t.Errorf("stateless options = %+v", o)
	}
}

// The stdio transport reads a message of up to tools.MaxMessageBytes, well past
// the SDK's 16 MiB default, and a larger one ends the session.
func TestStdioTransportMessageSizeLimit(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{stdinR, stdinW, stdoutR, stdoutW} {
			_ = f.Close()
		}
	})

	// A StdioTransport binds os.Stdin and os.Stdout when it connects, so the
	// process globals are swapped only around Connect.
	savedIn, savedOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdinR, stdoutW
	ss, err := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(t.Context(), newStdioTransport(), nil)
	os.Stdin, os.Stdout = savedIn, savedOut
	if err != nil {
		t.Fatal(err)
	}

	ended := make(chan error, 1)
	go func() { ended <- ss.Wait() }()
	responses := make(chan string, 2)
	go func() {
		r := bufio.NewReader(stdoutR)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			responses <- line
		}
	}()
	// Writes run apart from the test because a pipe write blocks until the
	// server reads it, and the server stops reading a message over the limit.
	send := func(msg string) { go func() { _, _ = stdinW.WriteString(msg + "\n") }() }

	send(initializeOfSize(1, tools.MaxMessageBytes))
	select {
	case line := <-responses:
		if !strings.Contains(line, `"result"`) {
			t.Fatalf("response to a %d-byte message: %.200s", tools.MaxMessageBytes, line)
		}
	case err := <-ended:
		t.Fatalf("a %d-byte message ended the session: %v", tools.MaxMessageBytes, err)
	case <-time.After(time.Minute):
		t.Fatalf("no response to a %d-byte message", tools.MaxMessageBytes)
	}

	send(initializeOfSize(2, tools.MaxMessageBytes+1))
	select {
	case <-ended:
	case line := <-responses:
		t.Fatalf("a %d-byte message was read: %.200s", tools.MaxMessageBytes+1, line)
	case <-time.After(time.Minute):
		t.Fatalf("a %d-byte message did not end the session", tools.MaxMessageBytes+1)
	}
}

// countingTransport counts the log notifications a client reads. Frames are
// read in order, so every notification sent before a reply is counted by the
// time the call returns.
type countingTransport struct {
	mcp.Transport
	logs *atomic.Int32
}

func (t countingTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	return countingConn{conn, t.logs}, err
}

type countingConn struct {
	mcp.Connection
	logs *atomic.Int32
}

func (c countingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if req, ok := msg.(*jsonrpc.Request); ok && req.Method == "notifications/message" {
		c.logs.Add(1)
	}
	return msg, err
}

// The server advertises what it registers and not logging, and sends no log
// notifications even to a client that asks for them at the debug level.
func TestServerAdvertisesNoLogging(t *testing.T) {
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			srv := newServer(Config{}, fakeClient{}, &subscriptionTracker{})
			ct, st := mcp.NewInMemoryTransports()
			if _, err := srv.Connect(t.Context(), st, nil); err != nil {
				t.Fatal(err)
			}
			var logs atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
			cs, err := client.Connect(t.Context(), countingTransport{ct, &logs}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cs.Close() })

			// This test uses the deprecated logging API on purpose: it checks
			// that the server does not offer the feature.
			caps := cs.InitializeResult().Capabilities
			if caps.Logging != nil { //nolint:staticcheck // checks the deprecated capability is absent
				t.Errorf("logging is advertised: %+v", caps.Logging) //nolint:staticcheck // as above
			}
			if caps.Tools == nil || caps.Prompts == nil || caps.Completions == nil || caps.Resources == nil || !caps.Resources.Subscribe {
				t.Errorf("capabilities = %+v, want tools, prompts, completions, and subscribable resources", caps)
			}

			// An older client sets the level for its session, and a current one
			// sends it with each request.
			call := &mcp.CallToolParams{Name: "no_such_tool", Meta: mcp.Meta{mcp.MetaKeyLogLevel: "debug"}} //nolint:staticcheck // asks for the deprecated notifications
			if version < statelessProtocolVersion {
				if err := cs.SetLoggingLevel(t.Context(), &mcp.SetLoggingLevelParams{Level: "debug"}); err != nil { //nolint:staticcheck // asks for the deprecated notifications
					t.Fatal(err)
				}
				call.Meta = nil
			}
			_, _ = cs.CallTool(t.Context(), call)
			if n := logs.Load(); n != 0 {
				t.Errorf("received %d log notifications", n)
			}
		})
	}
}

// Each client receives the instructions as of the moment it connects, in
// both protocol eras, with the zone stated by abbreviation and offset.
func TestInstructionsStateTheDateOfEachConnection(t *testing.T) {
	day := time.Date(2026, 3, 5, 9, 30, 0, 0, time.FixedZone("UTC+9", 9*60*60))
	for _, version := range []string{statelessProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			clock := day
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			srv.AddReceivingMiddleware(instructionsMiddleware(func() time.Time { return clock }))
			first := connectClient(t, srv, version, nil)
			clock = clock.AddDate(0, 0, 40)
			second := connectClient(t, srv, version, nil)
			for cs, want := range map[*mcp.ClientSession]string{first: "2026-03-05 09:30 UTC+9 (UTC+09:00).", second: "2026-04-14 09:30 UTC+9 (UTC+09:00)."} {
				got := cs.InitializeResult().Instructions
				if !strings.Contains(got, "Server date/time when this connection began: "+want) {
					t.Errorf("instructions %.100q, want the date %s", got, want)
				}
				if !strings.Contains(got, "Dates such as 2024-01-31 in tool inputs are read in this time zone") {
					t.Errorf("instructions %.300q do not say which zone dates are read in", got)
				}
			}
		})
	}
}

// The server states the current date in its own time zone, the one
// parseDateArg reads dates in. TestMain sets that zone to UTC+9.
func TestServerInstructionsStateToday(t *testing.T) {
	before := time.Now().In(time.Local).Format(jf.DateOnlyFormat)
	cs := connectClient(t, newServer(Config{}, fakeClient{}, &subscriptionTracker{}), "2025-11-25", nil)
	after := time.Now().In(time.Local).Format(jf.DateOnlyFormat)
	got := cs.InitializeResult().Instructions
	const prefix = "Server date/time when this connection began: "
	if !strings.Contains(got, prefix+before) && !strings.Contains(got, prefix+after) {
		t.Errorf("instructions %.100q, want today's date", got)
	}
	if !strings.Contains(got, " UTC+9 (UTC+09:00). Dates such as 2024-01-31 in tool inputs are read in this time zone") {
		t.Errorf("instructions %.300q, want the server's zone UTC+9", got)
	}
}

// The startup log names the zone with its offset, and warns when TZ names a
// zone Go could not load and replaced with UTC.
func TestTimeZoneReport(t *testing.T) {
	now := time.Date(2026, 3, 5, 9, 30, 0, 0, time.UTC)
	named := func(name string, offset int) *time.Location { return time.FixedZone(name, offset) }
	for _, tt := range []struct {
		tz   string
		loc  *time.Location
		want []string
	}{
		{"", time.UTC, []string{"Time zone: UTC (UTC+00:00)"}},
		{"UTC", time.UTC, []string{"Time zone: UTC (UTC+00:00)"}},
		{"Asia/Tokyo", named("JST", 9*60*60), []string{"Time zone: JST (UTC+09:00)"}},
		{"America/New_Yrok", time.UTC, []string{`WARNING: TZ="America/New_Yrok" is not a time zone this system knows, so dates are read and shown in UTC.`, "Time zone: UTC (UTC+00:00)"}},
	} {
		if got := timeZoneReport(tt.tz, tt.loc, now); !slices.Equal(got, tt.want) {
			t.Errorf("TZ=%q: %q, want %q", tt.tz, got, tt.want)
		}
	}
}

// The server identifies itself, titles every prompt, and tells clients how
// long each list and resource may be cached.
func TestServerMetadataAndCacheHints(t *testing.T) {
	emptyList := fakeClient{get: func(_ string, _ url.Values, dest any) error { return json.Unmarshal([]byte(`[]`), dest) }}
	cs := connectClient(t, newServer(Config{}, emptyList, &subscriptionTracker{}), statelessProtocolVersion, nil)

	info := cs.InitializeResult().ServerInfo
	if info.Description == "" || info.WebsiteURL != "https://github.com/jaredtrent/jellyfin-mcp" {
		t.Errorf("server info = %+v, want a description and the project URL", info)
	}

	static := mcp.Cacheable{TTLMs: int(staticTTL.Milliseconds()), CacheScope: "public"}
	live := mcp.Cacheable{TTLMs: 0, CacheScope: "private"}
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prompts, err := cs.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := cs.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := cs.ListResourceTemplates(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	guide, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://guides/docker"})
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://libraries"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct{ got, want mcp.Cacheable }{
		"tools/list":               {tools.Cacheable, static},
		"prompts/list":             {prompts.Cacheable, static},
		"resources/list":           {resources.Cacheable, static},
		"resources/templates/list": {templates.Cacheable, static},
		"a guide":                  {guide.Cacheable, static},
		"jellyfin://libraries":     {libraries.Cacheable, live},
	} {
		if tt.got != tt.want {
			t.Errorf("%s cache hints = %+v, want %+v", name, tt.got, tt.want)
		}
	}
	for _, p := range prompts.Prompts {
		if p.Title == "" {
			t.Errorf("prompt %s has no title", p.Name)
		}
	}
}

// Config.DisableDestructive reaches the tools as the policy that refuses
// destructive actions.
func TestDisableDestructiveRefusesAtTheTool(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		cs := connectClient(t, newServer(Config{DisableDestructive: disabled}, fakeClient{}, &subscriptionTracker{}), statelessProtocolVersion, nil)
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "jellyfin_system_control", Arguments: map[string]any{"action": "restart"}})
		if err != nil {
			t.Fatal(err)
		}
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		if refused := res.IsError && strings.Contains(text, "--disable-destructive"); refused != disabled {
			t.Errorf("disabled=%v: refused=%v, result %s", disabled, refused, text)
		}
		if !disabled && !strings.Contains(text, "CONFIRMATION REQUIRED") {
			t.Errorf("enabled: expected the confirmation warning, got %s", text)
		}
	}
}

// A failed call from a tool with an output type carries its error text and no
// structured content, so a client that prefers structured content still shows
// the error rather than an empty success.
func TestErrorResultsCarryNoStructuredContent(t *testing.T) {
	client := fakeClient{get: func(string, url.Values, any) error { return errors.New("API error 500: boom") }}
	cs := connectClient(t, newServer(Config{}, client, &subscriptionTracker{}), statelessProtocolVersion, nil)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "jellyfin_libraries", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("want an error result, got %+v", res)
	}
	if res.StructuredContent != nil {
		t.Errorf("error result carries structured content: %v", res.StructuredContent)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %+v", res.Content)
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); !ok || !strings.Contains(tc.Text, "API error 500: boom") {
		t.Errorf("error text = %+v", res.Content[0])
	}
}

func TestHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 5*time.Minute || srv.IdleTimeout != 60*time.Second || srv.WriteTimeout != 0 {
		t.Errorf("timeouts = header %v read %v idle %v write %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout, srv.WriteTimeout)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true, "127.5.5.5:1": true, "LOCALHOST:8080": true,
		"0.0.0.0:8080": false, "192.168.1.2:8080": false, "localhost.example:8080": false, ":8080": false, "[::]:8080": false,
	} {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// At most maxLargeRequests large bodies are in flight; the next gets 503.
func TestLimitLargeBodies(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	handler := limitLargeBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
	}), 2)
	ts := httptest.NewServer(handler)
	defer ts.Close()
	big := strings.Repeat("x", largeBodyBytes)
	codes := make(chan int, 3)
	for range 2 {
		go func() {
			resp, err := http.Post(ts.URL, "application/json", strings.NewReader(big))
			if err == nil {
				codes <- resp.StatusCode
				_ = resp.Body.Close()
			}
		}()
	}
	<-started
	<-started
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Errorf("third large request: %d %v", resp.StatusCode, resp.Header)
	}
	_ = resp.Body.Close()
	// A small request is admitted while both slots are taken: it reaches
	// the handler, which is what the third arrival proves by unblocking.
	smallCode := make(chan int, 1)
	go func() {
		resp, err := http.Post(ts.URL, "application/json", strings.NewReader("{}"))
		if err == nil {
			smallCode <- resp.StatusCode
			_ = resp.Body.Close()
		}
	}()
	<-started
	close(release)
	for range 2 {
		if code := <-codes; code != http.StatusOK {
			t.Errorf("admitted request: %d", code)
		}
	}
	if code := <-smallCode; code != http.StatusOK {
		t.Errorf("small request while full: %d", code)
	}
}

// A legacy initialize past the session cap gets 503; requests on an existing
// session and stateless requests are not counted.
func TestLimitSessions(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	for range 3 {
		ct, st := mcp.NewInMemoryTransports()
		if _, err := srv.Connect(t.Context(), st, nil); err != nil {
			t.Fatal(err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0.1"}, nil).Connect(t.Context(), ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
	}
	handler := limitSessions(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }), srv, 3)
	for name, tt := range map[string]struct {
		headers map[string]string
		want    int
	}{
		"legacy initialize":      {map[string]string{}, http.StatusServiceUnavailable},
		"existing session":       {map[string]string{"Mcp-Session-Id": "abc"}, http.StatusOK},
		"stateless protocol":     {map[string]string{"MCP-Protocol-Version": statelessProtocolVersion}, http.StatusOK},
		"older protocol no sess": {map[string]string{"MCP-Protocol-Version": "2025-11-25"}, http.StatusServiceUnavailable},
	} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		for k, v := range tt.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s: %d, want %d", name, rec.Code, tt.want)
		}
	}
	get := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Errorf("GET: %d", rec.Code)
	}
}

// jsonInto decodes v into dest through JSON, as the client would decode a
// server response.
func jsonInto(v any, dest any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}
