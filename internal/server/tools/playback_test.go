package tools_test

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func TestSessions_List(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Sessions" {
				// The idle session comes first from the server; the
				// result lists the playing one first.
				return jsonInto([]map[string]any{
					{
						"Id":         "session-2",
						"DeviceName": "iPhone",
						"Client":     "Jellyfin Mobile",
						"UserName":   "testuser",
					},
					{
						"Id":         "session-1",
						"DeviceName": "Living Room TV",
						"Client":     "Jellyfin Web",
						"UserName":   "testuser",
						"NowPlayingItem": map[string]any{
							"Name": "Neon Cascade",
							"Type": "Movie",
						},
					},
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_sessions", map[string]any{
		"action": "list",
	})

	out := structured[jf.SessionsOutput](t, result)
	if out.Sessions == nil || len(*out.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want two", out.Sessions)
	}
	if out.Resume != nil {
		t.Errorf("resume = %+v, want absent for action list", out.Resume)
	}
	playing, idle := (*out.Sessions)[0], (*out.Sessions)[1]
	if playing.SessionID != "session-1" || playing.Status != "playing" || playing.NowPlaying == nil || playing.NowPlaying.Name != "Neon Cascade" {
		t.Errorf("sessions[0] = %+v, want session-1 playing Neon Cascade", playing)
	}
	if idle.SessionID != "session-2" || idle.Status != "connected" || idle.NowPlaying != nil {
		t.Errorf("sessions[1] = %+v, want session-2 connected with nothing playing", idle)
	}
}

func TestSessions_Resume(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{
						"Id":   "item-1",
						"Name": "Movie in Progress",
						"Type": "Movie",
						"UserData": map[string]any{
							"PlayedPercentage": 45.0,
						},
					},
				},
				"TotalRecordCount": 1,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_sessions", map[string]any{
		"action": "resume",
	})

	out := structured[jf.SessionsOutput](t, result)
	if out.Resume == nil || len(*out.Resume) != 1 || (*out.Resume)[0].Progress != "45%" {
		t.Errorf("resume = %+v, want one item at 45%%", out.Resume)
	}
	if out.Sessions != nil {
		t.Errorf("sessions = %+v, want absent for action resume", out.Sessions)
	}
	if gotEndpoint != "/UserItems/Resume" {
		t.Errorf("endpoint = %q, want /UserItems/Resume", gotEndpoint)
	}
	for key, want := range map[string]string{
		"UserId": "test-user-id",
	} {
		if got := gotParams.Get(key); got != want {
			t.Errorf("param %s = %q, want %q", key, got, want)
		}
	}
}

func TestPlaybackControl_DisplayContent(t *testing.T) {
	var itemEndpoint string
	var itemParams url.Values
	var viewEndpoint string
	var viewParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if endpoint == "/Sessions" {
				return answerSessions(dest)
			}
			itemEndpoint, itemParams = endpoint, params
			return jsonInto(map[string]any{"Id": "item-123", "Name": "Test Movie", "Type": "Movie"}, dest)
		},
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, _ any) error {
			viewEndpoint, viewParams = endpoint, params
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_playback_control", map[string]any{
		"session_id": "session-1",
		"command":    "DisplayContent",
		"item_id":    "item-123",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if itemEndpoint != "/Items/item-123" {
		t.Errorf("item lookup endpoint = %q, want /Items/item-123", itemEndpoint)
	}
	if q := itemParams.Encode(); q != "UserId=test-user-id" {
		t.Errorf("item lookup query = %q, want UserId=test-user-id", q)
	}
	if viewEndpoint != "/Sessions/session-1/Viewing" {
		t.Errorf("viewing endpoint = %q, want /Sessions/session-1/Viewing", viewEndpoint)
	}
	if got := viewParams.Get("itemName"); got != "Test Movie" {
		t.Errorf("itemName = %q, want Test Movie", got)
	}
}

func TestPlaybackControl_SetVolume(t *testing.T) {
	var calls int
	var gotEndpoint string
	var gotParams url.Values
	var gotBody any
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error { return answerSessions(dest) },
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, body any) error {
			calls++
			gotEndpoint, gotParams, gotBody = endpoint, params, body
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_playback_control", map[string]any{
		"session_id": "session-1",
		"command":    "SetVolume",
		"volume":     35,
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if calls != 1 {
		t.Fatalf("requests sent = %d, want 1", calls)
	}
	if gotEndpoint != "/Sessions/session-1/Command" {
		t.Errorf("endpoint = %q, want /Sessions/session-1/Command", gotEndpoint)
	}
	if len(gotParams) != 0 {
		t.Errorf("query = %q, want none", gotParams.Encode())
	}
	body, err := json.Marshal(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"Arguments":{"Volume":"35"},"Name":"SetVolume"}`; string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPlaybackControl_SetVolumeValidation(t *testing.T) {
	tests := []struct {
		name    string
		volume  any // nil omits the argument
		wantErr bool
	}{
		{name: "missing", volume: nil, wantErr: true},
		{name: "below range", volume: -1, wantErr: true},
		{name: "above range", volume: 101, wantErr: true},
		{name: "minimum", volume: 0},
		{name: "maximum", volume: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error { return answerSessions(dest) },
				postNoContentFunc: func(context.Context, string, url.Values, any) error {
					calls++
					return nil
				},
			}
			args := map[string]any{"session_id": "session-1", "command": "SetVolume"}
			if tt.volume != nil {
				args["volume"] = tt.volume
			}

			result := callTool(t, mc, "", "jellyfin_playback_control", args)

			text := resultText(t, result)
			if result.IsError != tt.wantErr {
				t.Fatalf("IsError = %v, want %v: %s", result.IsError, tt.wantErr, text)
			}
			wantCalls := 1
			if tt.wantErr {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("requests sent = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestPlaybackTools_NoSyncPlayTool(t *testing.T) {
	cs := newTestSession(t, &mockClient{}, "")
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal("ListTools:", err)
	}
	for _, tool := range res.Tools {
		if strings.Contains(tool.Name, "syncplay") {
			t.Errorf("tool %q is registered; SyncPlay is covered by jellyfin://guides/syncplay and the syncplay-help prompt", tool.Name)
		}
	}
}

// toolNamePattern matches the tool names a guide or prompt tells the model to call.
var toolNamePattern = regexp.MustCompile(`jellyfin_[a-z_]+`)

// assertNamesRegisteredTools fails when text refers to a tool that tools/list does not offer.
func assertNamesRegisteredTools(t *testing.T, cs *mcp.ClientSession, what, text string) {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal("ListTools:", err)
	}
	registered := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		registered[tool.Name] = true
	}
	for _, name := range toolNamePattern.FindAllString(text, -1) {
		if !registered[name] {
			t.Errorf("%s refers to %s, which is not a registered tool", what, name)
		}
	}
}

func TestSyncPlayGuide_ListedAndReadable(t *testing.T) {
	const uri = "jellyfin://guides/syncplay"
	cs := newFullSession(t, &mockClient{})

	list, err := cs.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal("ListResources:", err)
	}
	var listed *mcp.Resource
	for _, r := range list.Resources {
		if r.URI == uri {
			listed = r
		}
	}
	if listed == nil {
		t.Fatalf("%s is not listed", uri)
	}
	if listed.MIMEType != "text/markdown" || listed.Title == "" || listed.Description == "" {
		t.Errorf("listing = %+v, want text/markdown with a title and description", listed)
	}

	read, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatal("ReadResource:", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(read.Contents))
	}
	text := read.Contents[0].Text
	for _, want := range []string{"# SyncPlay", "SyncPlayAccess", "WebSocket", "## Common Problems", "API key"} {
		if !strings.Contains(text, want) {
			t.Errorf("guide is missing %q", want)
		}
	}
	assertNamesRegisteredTools(t, cs, "the SyncPlay guide", text)
}

func TestSyncPlayHelpPrompt_ListedAndRenders(t *testing.T) {
	cs := newFullSession(t, &mockClient{})

	list, err := cs.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatal("ListPrompts:", err)
	}
	var listed *mcp.Prompt
	for _, p := range list.Prompts {
		if p.Name == "syncplay-help" {
			listed = p
		}
	}
	if listed == nil {
		t.Fatal("syncplay-help is not listed")
	}
	if listed.Title == "" || listed.Description == "" {
		t.Errorf("listing = %+v, want a title and description", listed)
	}

	tests := []struct {
		name string
		args map[string]string
		want []string
	}{
		{
			name: "setup",
			want: []string{"jellyfin://guides/syncplay", "jellyfin_sessions", "Profile > SyncPlay access", "creating a group"},
		},
		{
			name: "troubleshoot",
			args: map[string]string{"problem": "the group stays paused"},
			want: []string{"jellyfin://guides/syncplay", "\"the group stays paused\"", "log_file", "jellyfin://guides/remote-access"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "syncplay-help", Arguments: tt.args})
			if err != nil {
				t.Fatal("GetPrompt:", err)
			}
			if len(res.Messages) != 1 {
				t.Fatalf("got %d messages, want 1", len(res.Messages))
			}
			tc, ok := res.Messages[0].Content.(*mcp.TextContent)
			if !ok {
				t.Fatalf("content is %T, want *mcp.TextContent", res.Messages[0].Content)
			}
			for _, want := range tt.want {
				if !strings.Contains(tc.Text, want) {
					t.Errorf("prompt is missing %q:\n%s", want, tc.Text)
				}
			}
			assertNamesRegisteredTools(t, cs, "the syncplay-help prompt", tc.Text)
		})
	}
}

// answerSessions fills dest with one controllable session, session-1, the way
// GET /Sessions lists it.
func answerSessions(dest any) error {
	return jsonInto([]map[string]any{{
		"Id": "session-1", "DeviceName": "Living Room", "Client": "Test Client",
		"Capabilities": map[string]any{"SupportsMediaControl": true},
	}}, dest)
}
