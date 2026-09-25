package tools_test

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/url"
	"strings"
	"testing"
)

// Under --disable-destructive every destructive action is refused before it
// writes, even with confirm=true, while the tool's other actions keep working.
func TestDisableDestructive_RefusesEveryDestructiveAction(t *testing.T) {
	actions := []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_recordings", map[string]any{"action": "delete", "recording_id": "rec-1"}},
		{"jellyfin_recordings", map[string]any{"action": "cancel_timer", "timer_id": "timer-1"}},
		{"jellyfin_recordings", map[string]any{"action": "cancel_series_timer", "timer_id": "timer-1"}},
		{"jellyfin_collections", map[string]any{"action": "remove_items", "collection_id": "col-1", "item_ids": []string{"a"}}},
		{"jellyfin_library_manage", map[string]any{"action": "delete_item", "item_id": "item-1"}},
		{"jellyfin_library_manage", map[string]any{"action": "remove_folder", "folder_name": "Library A"}},
		{"jellyfin_library_manage", map[string]any{"action": "remove_path", "folder_name": "Library A", "path": "/media/a"}},
		{"jellyfin_users", map[string]any{"action": "delete", "user_id": "user-1"}},
		{"jellyfin_users", map[string]any{"action": "update_policy", "user_id": "user-1", "is_admin": true}},
		{"jellyfin_users", map[string]any{"action": "update_password", "user_id": "user-1", "password": "placeholder-password"}},
		{"jellyfin_users", map[string]any{"action": "update_config", "user_id": "user-1", "config": map[string]any{}}},
		{"jellyfin_library_manage", map[string]any{"action": "update_options", "folder_name": "Library A", "library_options": map[string]any{}}},
		{"jellyfin_tasks", map[string]any{"action": "set_triggers", "task_id": "task-1", "triggers": []any{}}},
		{"jellyfin_plugins", map[string]any{"action": "uninstall", "plugin_id": "plugin-1", "version": "1.0.0"}},
		{"jellyfin_plugins", map[string]any{"action": "update_config", "plugin_id": "plugin-1", "config": map[string]any{}}},
		{"jellyfin_plugins", map[string]any{"action": "set_repos", "repos": []any{}}},
		{"jellyfin_server", map[string]any{"action": "update_config_section", "key": "encoding", "config": map[string]any{}}},
		{"jellyfin_server", map[string]any{"action": "restore_backup", "file_name": "jellyfin-backup-1.zip"}},
		{"jellyfin_devices", map[string]any{"action": "delete", "device_id": "device-1"}},
		{"jellyfin_devices", map[string]any{"action": "revoke_api_key", "key": "key-1"}},
		{"jellyfin_subtitles_lyrics", map[string]any{"action": "delete_subtitle", "item_id": "item-1", "subtitle_index": 2}},
		{"jellyfin_subtitles_lyrics", map[string]any{"action": "delete_lyrics", "item_id": "item-1"}},
		{"jellyfin_system_control", map[string]any{"action": "restart"}},
		{"jellyfin_system_control", map[string]any{"action": "shutdown"}},
		{"jellyfin_videos", map[string]any{"action": "merge_versions", "item_ids": []string{"a", "b"}}},
		{"jellyfin_videos", map[string]any{"action": "split_versions", "item_id": "item-1"}},
		{"jellyfin_playlists", map[string]any{"action": "delete", "playlist_id": "playlist-1"}},
	}
	for _, a := range actions {
		t.Run(a.tool+"/"+a.args["action"].(string), func(t *testing.T) {
			writes := 0
			mc := &mockClient{
				disableDestructive: true,
				postFunc:           func(context.Context, string, url.Values, any, any) error { writes++; return nil },
				postNoContentFunc:  func(context.Context, string, url.Values, any) error { writes++; return nil },
				postRawFunc:        func(context.Context, string, url.Values, io.Reader, int64, string) error { writes++; return nil },
				delFunc:            func(context.Context, string, url.Values) error { writes++; return nil },
			}
			args := map[string]any{"confirm": true}
			for k, v := range a.args {
				args[k] = v
			}
			result := callTool(t, mc, "", a.tool, args)
			text := resultText(t, result)
			if !result.IsError || !strings.Contains(text, "--disable-destructive") {
				t.Errorf("got %s", text)
			}
			if writes != 0 {
				t.Errorf("%d write requests were sent", writes)
			}
		})
	}
}

func TestDisableDestructive_KeepsOtherActions(t *testing.T) {
	posts := 0
	mc := &mockClient{
		disableDestructive: true,
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto([]map[string]any{{"Id": "user-1", "Name": "User A"}}, dest)
		},
		postFunc: func(_ context.Context, _ string, _ url.Values, _ any, dest any) error {
			posts++
			return jsonInto(map[string]any{"Id": "user-2"}, dest)
		},
	}
	if result := callTool(t, mc, "", "jellyfin_users", map[string]any{"action": "list"}); result.IsError {
		t.Errorf("list: %s", resultText(t, result))
	}
	if result := callTool(t, mc, "", "jellyfin_users", map[string]any{"action": "create", "username": "User B"}); result.IsError || posts != 1 {
		t.Errorf("create: %s (posts %d)", resultText(t, result), posts)
	}
}

func TestDisableDestructive_RefusesPlaylistRemovals(t *testing.T) {
	p := newFakePlaylist(t, "a,a,b")
	mc := p.client()
	mc.disableDestructive = true
	result := callTool(t, mc, "", "jellyfin_playlists", map[string]any{"action": "deduplicate", "playlist_id": "playlist-1", "confirm": true, "dry_run": false})
	if text := resultText(t, result); !result.IsError || !strings.Contains(text, "--disable-destructive") {
		t.Errorf("got %s", text)
	}
	if m := p.methods(); len(m) != 0 {
		t.Errorf("write requests were sent: %v", m)
	}
}

// A call that carries the user's acceptance of the confirmation form
// reports it, so the model need not guess whether the form was shown.
func TestConfirmationNote_PrefixesConfirmedResults(t *testing.T) {
	deleted := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Users" {
				return jsonInto([]map[string]any{{"Id": "user-1", "Policy": map[string]any{"IsAdministrator": true}}}, dest)
			}
			return jsonInto(map[string]any{"Id": "playlist-1", "Name": "Road Trip"}, dest)
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error { deleted++; return nil },
	}
	cs := newTestSession(t, mc, "")
	args := map[string]any{"action": "delete", "playlist_id": "playlist-1"}

	warned, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "jellyfin_playlists", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, warned); !strings.Contains(text, "Delete playlist 'Road Trip'?") || deleted != 0 {
		t.Fatalf("unconfirmed: %s (deleted %d)", text, deleted)
	}
	confirmed, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "jellyfin_playlists", Arguments: args,
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, confirmed); text != "Confirmed by the user. Playlist 'Road Trip' deleted." || deleted != 1 {
		t.Errorf("confirmed: %q (deleted %d)", text, deleted)
	}
	declined, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "jellyfin_playlists", Arguments: args,
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "decline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, declined); text != "The user didn't confirm, so nothing changed." || deleted != 1 {
		t.Errorf("declined: %q (deleted %d)", text, deleted)
	}
}

// On a protocol before 2026-07-28 the SDK answers the form inside the call,
// so the confirmation is recorded on the call's context, and the note still
// reaches the client.
func TestConfirmationNote_LegacyProtocol(t *testing.T) {
	deleted := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Users" {
				return jsonInto([]map[string]any{{"Id": "user-1", "Policy": map[string]any{"IsAdministrator": true}}}, dest)
			}
			return jsonInto(map[string]any{"Id": "playlist-1", "Name": "Road Trip"}, dest)
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error { deleted++; return nil },
	}
	forms := 0
	cs := newClientSession(t, mc, "", &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			forms++
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "jellyfin_playlists", Arguments: map[string]any{"action": "delete", "playlist_id": "playlist-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, result); text != "Confirmed by the user. Playlist 'Road Trip' deleted." || deleted != 1 || forms != 1 {
		t.Errorf("got %q (deleted %d, forms %d)", text, deleted, forms)
	}
}
