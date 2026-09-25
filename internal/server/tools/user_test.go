package tools_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// userDataCall is one write request the mock client received.
type userDataCall struct {
	method   string
	endpoint string
	params   url.Values
}

// recordUserDataCalls returns a mock client that appends every POST and
// DELETE it receives to calls.
func recordUserDataCalls(calls *[]userDataCall) *mockClient {
	return &mockClient{
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, _ any) error {
			*calls = append(*calls, userDataCall{"POST", endpoint, params})
			return nil
		},
		delFunc: func(_ context.Context, endpoint string, params url.Values) error {
			*calls = append(*calls, userDataCall{"DELETE", endpoint, params})
			return nil
		},
	}
}

func TestUserData_SimpleActions(t *testing.T) {
	tests := []struct {
		action   string
		method   string
		endpoint string
		query    string
		message  string
	}{
		{"favorite", "POST", "/UserFavoriteItems/item-123", "UserId=test-user-id", "Item added to favorites."},
		{"unfavorite", "DELETE", "/UserFavoriteItems/item-123", "UserId=test-user-id", "Item removed from favorites."},
		{"like", "POST", "/UserItems/item-123/Rating", "UserId=test-user-id&likes=true", "Item rated: liked."},
		{"dislike", "POST", "/UserItems/item-123/Rating", "UserId=test-user-id&likes=false", "Item rated: disliked."},
		{"clear_rating", "DELETE", "/UserItems/item-123/Rating", "UserId=test-user-id", "Rating cleared."},
		{"mark_played", "POST", "/UserPlayedItems/item-123", "UserId=test-user-id", "Item marked as played."},
		{"mark_unplayed", "DELETE", "/UserPlayedItems/item-123", "UserId=test-user-id", "Item marked as unplayed."},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			var calls []userDataCall
			result := callTool(t, recordUserDataCalls(&calls), "", "jellyfin_user_data", map[string]any{
				"action":  tt.action,
				"item_id": "item-123",
			})

			if text := resultText(t, result); result.IsError || text != tt.message {
				t.Fatalf("result = %q (error=%v), want %q", text, result.IsError, tt.message)
			}
			if len(calls) != 1 {
				t.Fatalf("got %d requests, want 1: %+v", len(calls), calls)
			}
			got := calls[0]
			if got.method != tt.method || got.endpoint != tt.endpoint {
				t.Errorf("request = %s %s, want %s %s", got.method, got.endpoint, tt.method, tt.endpoint)
			}
			if q := got.params.Encode(); q != tt.query {
				t.Errorf("query = %q, want %q", q, tt.query)
			}
		})
	}
}

// No request may carry the user or rating of an earlier call on the same
// server, wherever the action table and its template params are held.
func TestUserData_SimpleActionsDoNotShareParams(t *testing.T) {
	var calls []userDataCall
	mc := recordUserDataCalls(&calls)
	var currentUser string
	mc.getUserIDFunc = func(context.Context) (string, error) { return currentUser, nil }
	cs := newTestSession(t, mc, "")

	steps := []struct {
		user, action, itemID string
		method, endpoint     string
		query                string
	}{
		{"user-a", "like", "item-1", "POST", "/UserItems/item-1/Rating", "UserId=user-a&likes=true"},
		{"user-b", "favorite", "item-2", "POST", "/UserFavoriteItems/item-2", "UserId=user-b"},
		{"user-c", "dislike", "item-3", "POST", "/UserItems/item-3/Rating", "UserId=user-c&likes=false"},
		{"user-d", "mark_unplayed", "item-4", "DELETE", "/UserPlayedItems/item-4", "UserId=user-d"},
		{"user-e", "like", "item-5", "POST", "/UserItems/item-5/Rating", "UserId=user-e&likes=true"},
	}
	for _, s := range steps {
		currentUser = s.user
		result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "jellyfin_user_data",
			Arguments: map[string]any{"action": s.action, "item_id": s.itemID},
		})
		if err != nil {
			t.Fatalf("CallTool(%s): %v", s.action, err)
		}
		if result.IsError {
			t.Fatalf("%s: unexpected error: %s", s.action, resultText(t, result))
		}
	}

	// Every request is checked after the last call has run, so a later call
	// that mutated an earlier request's params fails here too.
	if len(calls) != len(steps) {
		t.Fatalf("got %d requests, want %d: %+v", len(calls), len(steps), calls)
	}
	for i, s := range steps {
		got := calls[i]
		if got.method != s.method || got.endpoint != s.endpoint {
			t.Errorf("request %d (%s) = %s %s, want %s %s", i, s.action, got.method, got.endpoint, s.method, s.endpoint)
		}
		if q := got.params.Encode(); q != s.query {
			t.Errorf("request %d (%s) query = %q, want %q", i, s.action, q, s.query)
		}
	}
}

func TestUserData_GetUserData(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{
				"PlayCount":             3,
				"IsFavorite":            true,
				"Played":                true,
				"Rating":                8.5,
				"PlaybackPositionTicks": 0,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_user_data", map[string]any{
		"action":  "get_user_data",
		"item_id": "item-123",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotEndpoint != "/UserItems/item-123/UserData" {
		t.Errorf("endpoint = %q, want /UserItems/item-123/UserData", gotEndpoint)
	}
	if q := gotParams.Encode(); q != "UserId=test-user-id" {
		t.Errorf("query = %q, want UserId=test-user-id", q)
	}
	if !strings.Contains(text, `"play_count": 3`) {
		t.Errorf("expected play_count in result, got: %s", text)
	}
}

func TestUserData_Rate(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	var capturedBody any
	mc := &mockClient{
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, body any) error {
			gotEndpoint, gotParams, capturedBody = endpoint, params, body
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_user_data", map[string]any{
		"action":  "rate",
		"item_id": "item-123",
		"rating":  8.5,
	})

	text := resultText(t, result)
	if !strings.Contains(text, "8.5") {
		t.Errorf("expected rating value in result, got: %s", text)
	}
	if gotEndpoint != "/UserItems/item-123/UserData" {
		t.Errorf("endpoint = %q, want /UserItems/item-123/UserData", gotEndpoint)
	}
	if q := gotParams.Encode(); q != "UserId=test-user-id" {
		t.Errorf("query = %q, want UserId=test-user-id", q)
	}
	if capturedBody == nil {
		t.Error("expected body to be sent")
	}
}

func TestUserData_SetUserData(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	var gotBody map[string]any
	mc := &mockClient{
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, body any) error {
			gotEndpoint, gotParams = endpoint, params
			gotBody, _ = body.(map[string]any)
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_user_data", map[string]any{
		"action":  "set_user_data",
		"item_id": "item-123",
		"played":  true,
		"confirm": true,
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotEndpoint != "/UserItems/item-123/UserData" {
		t.Errorf("endpoint = %q, want /UserItems/item-123/UserData", gotEndpoint)
	}
	if q := gotParams.Encode(); q != "UserId=test-user-id" {
		t.Errorf("query = %q, want UserId=test-user-id", q)
	}
	if played, _ := gotBody["Played"].(bool); !played {
		t.Errorf("body = %v, want Played=true", gotBody)
	}
}

func TestPlaylists_List(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "playlist-1", "Name": "Playlist A", "ChildCount": 4},
				},
				"TotalRecordCount": 1,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_playlists", map[string]any{
		"action": "list",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotEndpoint != "/Items" {
		t.Errorf("endpoint = %q, want /Items", gotEndpoint)
	}
	for key, want := range map[string]string{
		"UserId":           "test-user-id",
		"IncludeItemTypes": "Playlist",
		"Recursive":        "true",
	} {
		if got := gotParams.Get(key); got != want {
			t.Errorf("param %s = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(text, "Playlist A") {
		t.Errorf("expected playlist name in result, got: %s", text)
	}
}

// An item_ids element holding a comma-separated list counts each ID, so the
// confirmation names as many items as the request removes.
func TestCollections_RemoveItemsCountsEveryID(t *testing.T) {
	var sent string
	mc := &mockClient{delFunc: func(_ context.Context, _ string, params url.Values) error { sent = params.Get("ids"); return nil }}
	result := callTool(t, mc, "", "jellyfin_collections", map[string]any{"action": "remove_items", "collection_id": "col-1", "item_ids": []string{"a,b", "c"}})
	if text := resultText(t, result); !strings.Contains(text, "Remove 'a', 'b', 'c' from collection") {
		t.Errorf("got %s", text)
	}
	callTool(t, mc, "", "jellyfin_collections", map[string]any{"action": "remove_items", "collection_id": "col-1", "item_ids": []string{"a,b", "c"}, "confirm": true})
	if sent != "a,b,c" {
		t.Errorf("ids = %q, want a,b,c", sent)
	}
}
