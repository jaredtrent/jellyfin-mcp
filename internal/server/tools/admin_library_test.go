package tools_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// libraryManagePosts records every PostNoContent request and fails the test
// on any Get or Del, so a test sees exactly the requests a tool sends.
func libraryManagePosts(t *testing.T, posts *[]postCall) *mockClient {
	t.Helper()
	return &mockClient{
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, reqBody any) error {
			*posts = append(*posts, postCall{endpoint: endpoint, params: maps.Clone(params), body: reqBody})
			return nil
		},
		getFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			t.Errorf("unexpected GET %s", endpoint)
			return nil
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error {
			t.Errorf("unexpected DELETE %s", endpoint)
			return nil
		},
	}
}

// bodyShape returns v as it is encoded on the wire, decoded into generic JSON
// values so request bodies compare by shape rather than by Go type.
func bodyShape(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestLibraryManage_AddFolder_PathInLibraryOptions(t *testing.T) {
	var posts []postCall
	mc := libraryManagePosts(t, &posts)

	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{
		"action":          "add_folder",
		"folder_name":     "Library A",
		"collection_type": "movies",
		"path":            "/media/library-a",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d POST requests, want 1", len(posts))
	}
	got := posts[0]
	if got.endpoint != "/Library/VirtualFolders" {
		t.Errorf("endpoint = %q, want /Library/VirtualFolders", got.endpoint)
	}
	wantParams := url.Values{
		"name":           {"Library A"},
		"collectionType": {"movies"},
		"refreshLibrary": {"true"},
	}
	if !reflect.DeepEqual(got.params, wantParams) {
		t.Errorf("params = %v, want %v", got.params, wantParams)
	}
	wantBody := bodyShape(t, map[string]any{
		"LibraryOptions": map[string]any{
			"PathInfos": []any{map[string]any{"Path": "/media/library-a"}},
		},
	})
	if gotBody := bodyShape(t, got.body); !reflect.DeepEqual(gotBody, wantBody) {
		t.Errorf("body = %v, want %v", gotBody, wantBody)
	}
	if !strings.Contains(text, "scan was requested") {
		t.Errorf("expected the result to report the scan, got: %s", text)
	}
}

func TestLibraryManage_AddFolder_WithoutPathSendsNoOptionsOrScan(t *testing.T) {
	var posts []postCall
	mc := libraryManagePosts(t, &posts)

	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{
		"action":      "add_folder",
		"folder_name": "Library A",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d POST requests, want 1", len(posts))
	}
	wantParams := url.Values{"name": {"Library A"}}
	if !reflect.DeepEqual(posts[0].params, wantParams) {
		t.Errorf("params = %v, want %v", posts[0].params, wantParams)
	}
	if gotBody := bodyShape(t, posts[0].body); !reflect.DeepEqual(gotBody, map[string]any{}) {
		t.Errorf("body = %v, want an empty object", gotBody)
	}
	if !strings.Contains(text, "add_path") {
		t.Errorf("expected the result to point at add_path, got: %s", text)
	}
}

func TestLibraryManage_AddFolder_TrimsName(t *testing.T) {
	var posts []postCall
	mc := libraryManagePosts(t, &posts)

	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{
		"action":      "add_folder",
		"folder_name": "  Library A\t",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	if len(posts) != 1 {
		t.Fatalf("got %d POST requests, want 1", len(posts))
	}
	if got := posts[0].params.Get("name"); got != "Library A" {
		t.Errorf("name = %q, want %q", got, "Library A")
	}
}

func TestLibraryManage_AddFolder_RejectsBlankName(t *testing.T) {
	var posts []postCall
	mc := libraryManagePosts(t, &posts)

	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{
		"action":      "add_folder",
		"folder_name": "   ",
		"path":        "/media/library-a",
	})

	if !result.IsError {
		t.Fatalf("expected an error for a blank name, got: %s", resultText(t, result))
	}
	if len(posts) != 0 {
		t.Errorf("got %d POST requests, want none", len(posts))
	}
}

// refresh_item asks Jellyfin to run its metadata and image providers; without
// a refresh mode the server would only rescan the file.
func TestLibraryManage_RefreshItemSendsRefreshModes(t *testing.T) {
	var got url.Values
	mc := &mockClient{postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, _ any) error {
		if endpoint != "/Items/item-1/Refresh" {
			t.Errorf("POST %s", endpoint)
		}
		got = params
		return nil
	}}
	callTool(t, mc, "", "jellyfin_library_manage", map[string]any{"action": "refresh_item", "item_id": "item-1"})
	for key, want := range map[string]string{"MetadataRefreshMode": "FullRefresh", "ImageRefreshMode": "FullRefresh", "ReplaceAllMetadata": "", "ReplaceAllImages": ""} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
	callTool(t, mc, "", "jellyfin_library_manage", map[string]any{"action": "refresh_item", "item_id": "item-1", "replace_all_metadata": true})
	if got.Get("ReplaceAllMetadata") != "true" || got.Get("MetadataRefreshMode") != "FullRefresh" {
		t.Errorf("replace: %v", got)
	}
}

func TestLibraryManage_UpdateOptionsAsksConfirmation(t *testing.T) {
	writes := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto([]map[string]any{{"Name": "Library A", "ItemId": "lib-1"}}, dest)
		},
		postNoContentFunc: func(context.Context, string, url.Values, any) error { writes++; return nil },
	}
	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{"action": "update_options", "folder_name": "Library A", "library_options": map[string]any{"EnableRealtimeMonitor": true}})
	if text := resultText(t, result); !strings.Contains(text, "CONFIRMATION REQUIRED") || !strings.Contains(text, "Replace all options") {
		t.Errorf("got %s", text)
	}
	if writes != 0 {
		t.Errorf("%d writes without confirmation", writes)
	}
}
