package tools_test

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaredtrent/jellyfin-mcp/internal/server/prompts"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/resources"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

// TestMain runs the package's tests in UTC, so results that depend on the
// server's time zone do not depend on the machine running them. Tests about
// the zone set their own with inZone.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func TestBuildToolFilter_AllToolsets(t *testing.T) {
	enabled := tools.BuildToolFilter("", false)
	// With no filters, all tools should be enabled
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_search to be enabled with no filters")
	}
	if !enabled("jellyfin_metadata", tools.AnnotWriteOp) {
		t.Error("expected jellyfin_metadata to be enabled with no filters")
	}
}

func TestBuildToolFilter_SpecificToolset(t *testing.T) {
	enabled := tools.BuildToolFilter("discovery", false)
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_search to be enabled in discovery toolset")
	}
	if !enabled("jellyfin_libraries", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_libraries to be enabled in discovery toolset")
	}
	// A tool from a different toolset should be disabled
	if enabled("jellyfin_tv_shows", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_tv_shows to be disabled when only discovery is enabled")
	}
}

func TestBuildToolFilter_MultipleToolsets(t *testing.T) {
	enabled := tools.BuildToolFilter("discovery,media", false)
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_search enabled")
	}
	if !enabled("jellyfin_tv_shows", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_tv_shows enabled")
	}
	if enabled("jellyfin_user_data", tools.AnnotWriteOp) {
		t.Error("expected jellyfin_user_data disabled")
	}
}

func TestBuildToolFilter_ReadOnly(t *testing.T) {
	enabled := tools.BuildToolFilter("", true)
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected read-only tool to be enabled in read-only mode")
	}
	if enabled("jellyfin_metadata", tools.AnnotWriteOp) {
		t.Error("expected write tool to be disabled in read-only mode")
	}
	if enabled("jellyfin_system_control", tools.AnnotDestructive) {
		t.Error("expected destructive tool to be disabled in read-only mode")
	}
}

func TestBuildToolFilter_NilAnnotations(t *testing.T) {
	enabled := tools.BuildToolFilter("", false)
	// Tools with nil annotations should be enabled when no filter is active
	if !enabled("some_tool", nil) {
		t.Error("expected tool with nil annotations to be enabled with no filters")
	}

	enabledReadOnly := tools.BuildToolFilter("", true)
	// Tools with nil annotations are not read-only, so should be disabled
	if enabledReadOnly("some_tool", nil) {
		t.Error("expected tool with nil annotations to be disabled in read-only mode")
	}
}

func TestRegisterTools_AllRegistered(t *testing.T) {
	mc := &mockClient{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	filter := tools.BuildToolFilter("", false)
	tools.RegisterTools(srv, mc, filter)

	// Connect and list tools
	ct, st := newTransports()
	_, err := srv.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal("server connect:", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	defer func() { _ = cs.Close() }()

	result, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal("ListTools:", err)
	}

	// Count expected tools from ToolsetMap
	var expectedCount int
	for _, toolNames := range tools.ToolsetMap {
		expectedCount += len(toolNames)
	}

	if len(result.Tools) != expectedCount {
		got := make([]string, len(result.Tools))
		for i, tool := range result.Tools {
			got[i] = tool.Name
		}
		t.Errorf("got %d tools, want %d. Tools: %v", len(result.Tools), expectedCount, got)
	}
}

// newTransports connects a client and a server in memory. Both read messages
// of up to tools.MaxMessageBytes, the limit the server applies in production.
func newTransports() (client, server mcp.Transport) {
	c, s := net.Pipe()
	return &mcp.IOTransport{Reader: c, Writer: c, MaxLineLength: tools.MaxMessageBytes},
		&mcp.IOTransport{Reader: s, Writer: s, MaxLineLength: tools.MaxMessageBytes}
}

// newTestSession registers the tools against mc and returns a connected
// client session, so successive calls reach the same server and handlers.
func newTestSession(t *testing.T, mc *mockClient, toolsets string) *mcp.ClientSession {
	t.Helper()
	return newClientSession(t, mc, toolsets, nil, nil)
}

// newClientSession is newTestSession for a client with the given options,
// such as the capabilities it declares or the protocol version it asks for.
func newClientSession(t *testing.T, mc *mockClient, toolsets string, opts *mcp.ClientOptions, sessOpts *mcp.ClientSessionOptions) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	tools.RegisterTools(srv, mc, tools.BuildToolFilter(toolsets, false))
	srv.AddReceivingMiddleware(tools.DestructivePolicy(mc.disableDestructive), tools.ConfirmationNote())

	ct, st := newTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal("server connect:", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, opts)
	cs, err := client.Connect(t.Context(), ct, sessOpts)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// newFullSession registers resources, prompts, and every tool against mc, as
// the server does at startup, and returns a connected client session.
func newFullSession(t *testing.T, mc *mockClient) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	resources.RegisterResources(srv, mc)
	prompts.RegisterPrompts(srv, mc)
	tools.RegisterTools(srv, mc, tools.BuildToolFilter("", false))

	ct, st := newTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal("server connect:", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callTool is a test helper that registers tools, connects client/server,
// and calls a specific tool with the given arguments.
func callTool(t *testing.T, mc *mockClient, toolsets string, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	cs := newTestSession(t, mc, toolsets)
	result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", toolName, err)
	}
	return result
}

// resultText extracts text content from a CallToolResult.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// structured is a successful result's structured content decoded into T,
// the output struct of the tool that produced it. It fails the test on an
// error result, so a caller reads facts from the struct and never from the
// result's text.
func structured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	if result.StructuredContent == nil {
		t.Fatal("result has no structured content")
	}
	var out T
	if err := jsonInto(result.StructuredContent, &out); err != nil {
		t.Fatalf("decoding structured output: %v", err)
	}
	return out
}

// hasNote reports whether one of a result's notes contains substr.
func hasNote(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

// Destructive actions confirm through the confirmation gate, which asks the
// user when the call leaves confirm unset. No tool text may tell the model to
// set confirm=true up front, and every confirm input says to leave it unset.
func TestToolTextsLeaveConfirmationToTheGate(t *testing.T) {
	cs := newTestSession(t, &mockClient{}, "")
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "confirm=true") {
			t.Errorf("%s tells the model to set confirm=true: %s", tool.Name, b)
		}
		var schema struct {
			Properties map[string]struct{ Description string }
		}
		b, _ = json.Marshal(tool.InputSchema)
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}
		if c, ok := schema.Properties["confirm"]; ok && !strings.HasPrefix(c.Description, "Leave unset so the user is asked to confirm") {
			t.Errorf("%s confirm description = %q", tool.Name, c.Description)
		}
	}
}
