package tools_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

func TestBuildToolFilter_AllToolsets(t *testing.T) {
	enabled := tools.BuildToolFilter("", false, false)
	// With no filters, all tools should be enabled
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected jellyfin_search to be enabled with no filters")
	}
	if !enabled("jellyfin_metadata", tools.AnnotWriteOp) {
		t.Error("expected jellyfin_metadata to be enabled with no filters")
	}
}

func TestBuildToolFilter_SpecificToolset(t *testing.T) {
	enabled := tools.BuildToolFilter("discovery", false, false)
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
	enabled := tools.BuildToolFilter("discovery,media", false, false)
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
	enabled := tools.BuildToolFilter("", true, false)
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

func TestBuildToolFilter_DisableDestructive(t *testing.T) {
	enabled := tools.BuildToolFilter("", false, true)
	if !enabled("jellyfin_search", tools.AnnotReadOnly) {
		t.Error("expected read-only tool to be enabled")
	}
	if !enabled("jellyfin_metadata", tools.AnnotWriteOp) {
		t.Error("expected write tool to be enabled when only destructive is disabled")
	}
	if enabled("jellyfin_system_control", tools.AnnotDestructive) {
		t.Error("expected destructive tool to be disabled")
	}
}

func TestDisableDestructiveRemovesDestructiveTools(t *testing.T) {
	mc := &mockClient{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	tools.RegisterTools(srv, mc, tools.BuildToolFilter("", false, true))

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool, len(list.Tools))
	for _, tool := range list.Tools {
		registered[tool.Name] = true
	}

	destructiveTools := []string{
		"jellyfin_system_control",
		"jellyfin_users",
		"jellyfin_library_manage",
		"jellyfin_devices",
		"jellyfin_recordings",
		"jellyfin_videos",
	}
	for _, toolName := range destructiveTools {
		if registered[toolName] {
			t.Errorf("%s remains registered with --disable-destructive", toolName)
		}
		if _, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      toolName,
			Arguments: map[string]any{"confirm": true},
		}); err == nil {
			t.Errorf("%s remains callable with --disable-destructive", toolName)
		}
	}
}

func TestBuildToolFilter_NilAnnotations(t *testing.T) {
	enabled := tools.BuildToolFilter("", false, false)
	// Tools with nil annotations should be enabled when no filter is active
	if !enabled("some_tool", nil) {
		t.Error("expected tool with nil annotations to be enabled with no filters")
	}

	enabledReadOnly := tools.BuildToolFilter("", true, false)
	// Tools with nil annotations are not read-only, so should be disabled
	if enabledReadOnly("some_tool", nil) {
		t.Error("expected tool with nil annotations to be disabled in read-only mode")
	}
}

func TestRegisterTools_AllRegistered(t *testing.T) {
	mc := &mockClient{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	filter := tools.BuildToolFilter("", false, false)
	tools.RegisterTools(srv, mc, filter)

	// Connect and list tools
	ct, st := mcp.NewInMemoryTransports()
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

// callTool is a test helper that registers tools, connects client/server,
// and calls a specific tool with the given arguments.
func callTool(t *testing.T, mc *mockClient, toolsets string, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	filter := tools.BuildToolFilter(toolsets, false, false)
	tools.RegisterTools(srv, mc, filter)

	ct, st := mcp.NewInMemoryTransports()
	_, err := srv.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal("server connect:", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

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
