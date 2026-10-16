package tools_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestLibraryManage_ScanAllLibraries(t *testing.T) {
	var posts []postCall
	result := callTool(t, libraryManagePosts(t, &posts), "", "jellyfin_library_manage", map[string]any{"action": "scan"})
	if len(posts) != 1 || posts[0].endpoint != "/Library/Refresh" || len(posts[0].params) != 0 {
		t.Fatalf("posts = %+v, want one POST /Library/Refresh without parameters", posts)
	}
	if text := resultText(t, result); !strings.Contains(text, "Library scan started") {
		t.Errorf("got %s", text)
	}
}

// A scan by item_id sends what the dashboard's Scan Library sends, and the
// result says where Jellyfin lists a library's scan state, because it isn't a
// scheduled task.
func TestLibraryManage_ScanItemSendsDefaultModes(t *testing.T) {
	var posts []postCall
	result := callTool(t, libraryManagePosts(t, &posts), "", "jellyfin_library_manage", map[string]any{"action": "scan", "item_id": "lib-1"})
	if len(posts) != 1 {
		t.Fatalf("%d requests, want 1", len(posts))
	}
	if posts[0].endpoint != "/Items/lib-1/Refresh" {
		t.Errorf("POST %s", posts[0].endpoint)
	}
	want := url.Values{"MetadataRefreshMode": {"Default"}, "ImageRefreshMode": {"Default"}}
	if !reflect.DeepEqual(posts[0].params, want) {
		t.Errorf("params = %v, want %v", posts[0].params, want)
	}
	if text := resultText(t, result); !strings.Contains(text, "Scan started") || !strings.Contains(text, "RefreshStatus") {
		t.Errorf("got %s", text)
	}
}

// The replace flags belong to refresh_item, and replace_all_metadata would make
// every provider run on every item even in a scan, so every scope of scan
// refuses them before it sends anything.
func TestLibraryManage_ScanRefusesReplaceFlags(t *testing.T) {
	for _, args := range []map[string]any{
		{"action": "scan", "replace_all_metadata": true},
		{"action": "scan", "item_id": "lib-1", "replace_all_metadata": true},
		{"action": "scan", "path": "/media/tv/Show A", "replace_all_images": true},
	} {
		var posts []postCall
		result := callTool(t, libraryManagePosts(t, &posts), "", "jellyfin_library_manage", args)
		if text := resultText(t, result); !result.IsError || !strings.Contains(text, "refresh_item") {
			t.Errorf("%v: got %s", args, text)
		}
		if len(posts) != 0 {
			t.Errorf("%v: %d requests for a refused scan", args, len(posts))
		}
	}
}

func TestLibraryManage_ScanRefusesItemAndPathTogether(t *testing.T) {
	var posts []postCall
	result := callTool(t, libraryManagePosts(t, &posts), "", "jellyfin_library_manage", map[string]any{"action": "scan", "item_id": "lib-1", "path": "/media/tv"})
	if text := resultText(t, result); !result.IsError || !strings.Contains(text, "either item_id or path") {
		t.Errorf("got %s", text)
	}
	if len(posts) != 0 {
		t.Errorf("%d requests for a refused scan", len(posts))
	}
}

// tvLibrary is one library, TV, with the given folders.
func tvLibrary(folders ...string) []map[string]any {
	return []map[string]any{{"Name": "TV", "ItemId": "lib-1", "Locations": folders}}
}

// scanPathClient serves libs and a server configuration whose library monitor
// delay is delay; a nil delay makes the configuration unreadable. It records
// every POST.
func scanPathClient(t *testing.T, posts *[]postCall, libs []map[string]any, delay *int) *mockClient {
	t.Helper()
	return &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Library/VirtualFolders":
				return jsonInto(libs, dest)
			case "/System/Configuration":
				if delay == nil {
					return errors.New("configuration unavailable")
				}
				return jsonInto(map[string]any{"ServerName": "Server A", "LibraryMonitorDelay": *delay}, dest)
			}
			t.Errorf("unexpected GET %s", endpoint)
			return nil
		},
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, reqBody any) error {
			*posts = append(*posts, postCall{endpoint: endpoint, params: maps.Clone(params), body: reqBody})
			return nil
		},
	}
}

// A path goes to Jellyfin's library monitor, which reads the nearest item at
// or above it once the server's delay runs out, so the result states the delay
// the server has.
func TestLibraryManage_ScanPathReportsThePath(t *testing.T) {
	path := "/media/tv/Show A/Season 01"
	for delay, want := range map[int]string{15: "delay of 15 seconds", 1: "delay of 1 second,"} {
		var posts []postCall
		result := callTool(t, scanPathClient(t, &posts, tvLibrary("/media/tv"), &delay), "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": path})
		text := resultText(t, result)
		if result.IsError {
			t.Fatalf("unexpected error: %s", text)
		}
		if len(posts) != 1 {
			t.Fatalf("%d requests, want 1", len(posts))
		}
		if posts[0].endpoint != "/Library/Media/Updated" || len(posts[0].params) != 0 {
			t.Errorf("POST %s with %v, want /Library/Media/Updated without parameters", posts[0].endpoint, posts[0].params)
		}
		wantBody := bodyShape(t, map[string]any{"Updates": []any{map[string]any{"Path": path}}})
		if got := bodyShape(t, posts[0].body); !reflect.DeepEqual(got, wantBody) {
			t.Errorf("body = %v, want %v", got, wantBody)
		}
		for _, s := range []string{"TV library", want} {
			if !strings.Contains(text, s) {
				t.Errorf("result lacks %q: %s", s, text)
			}
		}
	}
}

func TestLibraryManage_ScanPathStatesTheDefaultDelayWhenTheConfigurationIsUnreadable(t *testing.T) {
	var posts []postCall
	result := callTool(t, scanPathClient(t, &posts, tvLibrary("/media/tv"), nil), "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": "/media/tv/Show A"})
	text := resultText(t, result)
	if result.IsError || len(posts) != 1 {
		t.Fatalf("%d requests, result %s", len(posts), text)
	}
	if !strings.Contains(text, "60 seconds by default") {
		t.Errorf("got %s", text)
	}
}

func TestLibraryManage_ScanPathSendsNothingWhenTheLibrariesAreUnreadable(t *testing.T) {
	posts := 0
	mc := &mockClient{
		getFunc:           func(context.Context, string, url.Values, any) error { return errors.New("server unavailable") },
		postNoContentFunc: func(context.Context, string, url.Values, any) error { posts++; return nil },
	}
	result := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": "/media/tv/Show A"})
	if !result.IsError || posts != 0 {
		t.Errorf("%d requests, result %s", posts, resultText(t, result))
	}
}

// Jellyfin accepts any path, looks it up letter for letter, and ignores one
// that resolves to no item, so a path no library folder holds is refused
// before anything is sent, and the error names the folders that work.
func TestLibraryManage_ScanPathOutsideTheLibrariesIsRefused(t *testing.T) {
	delay := 15
	for _, c := range []struct {
		path    string
		folders []string
		want    string
	}{
		{"/downloads/tv/Show A", []string{"/media/tv"}, "'/media/tv' (TV)"},
		{"/media", []string{"/media/tv"}, "'/media/tv' (TV)"},
		{"/media/tv-archive/Show A", []string{"/media/tv"}, "'/media/tv' (TV)"},
		{"/Media/TV/Show A", []string{"/media/tv"}, "same letter case"},
		{`\media\tv\Show A`, []string{"/media/tv"}, "'/media/tv' (TV)"},
		{`d:\media\tv\Show A`, []string{`D:\Media\TV`}, `'D:\Media\TV' (TV)`},
		{"/media/tv/Show A", []string{""}, "isn't inside a library folder"},
		{"/media/tv/Show A", nil, "no library on the server has a folder"},
	} {
		var posts []postCall
		result := callTool(t, scanPathClient(t, &posts, tvLibrary(c.folders...), &delay), "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": c.path})
		if text := resultText(t, result); !result.IsError || !strings.Contains(text, c.want) {
			t.Errorf("%s in %q: got %s", c.path, c.folders, text)
		}
		if len(posts) != 0 {
			t.Errorf("%s in %q: %d requests for a refused path", c.path, c.folders, len(posts))
		}
	}
}

// A Windows server stores paths with backslashes and reads either separator,
// so a path under a Windows folder is sent with backslashes. A folder can end
// in a separator, and a folder at the root holds every path.
func TestLibraryManage_ScanPathMatchesTheWayJellyfinDoes(t *testing.T) {
	delay := 15
	for _, c := range []struct{ folder, path, sent string }{
		{`D:\Media\TV`, `D:\Media\TV\Show A`, `D:\Media\TV\Show A`},
		{`D:\Media\TV`, "D:/Media/TV/Show A", `D:\Media\TV\Show A`},
		{`D:\Media\TV\`, `D:\Media\TV`, `D:\Media\TV`},
		{`\\nas\media\TV`, `\\nas\media\TV\Show A`, `\\nas\media\TV\Show A`},
		{"/media/tv/", "/media/tv", "/media/tv"},
		{"/", "/media/tv/Show A", "/media/tv/Show A"},
	} {
		var posts []postCall
		result := callTool(t, scanPathClient(t, &posts, tvLibrary(c.folder), &delay), "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": c.path})
		if result.IsError || len(posts) != 1 {
			t.Errorf("%q in %q: %d requests, result %s", c.path, c.folder, len(posts), resultText(t, result))
			continue
		}
		want := bodyShape(t, map[string]any{"Updates": []any{map[string]any{"Path": c.sent}}})
		if got := bodyShape(t, posts[0].body); !reflect.DeepEqual(got, want) {
			t.Errorf("%q in %q: body = %v, want %v", c.path, c.folder, got, want)
		}
	}
}

// One library's folder can sit inside another's, and the deepest folder that
// holds the path names the library, whichever library is listed first.
func TestLibraryManage_ScanPathNamesTheLibraryWithTheDeepestFolder(t *testing.T) {
	delay := 15
	outer := map[string]any{"Name": "Home Videos", "ItemId": "lib-2", "Locations": []string{"/media"}}
	inner := tvLibrary("/media/tv")[0]
	for _, libs := range [][]map[string]any{{outer, inner}, {inner, outer}} {
		var posts []postCall
		result := callTool(t, scanPathClient(t, &posts, libs, &delay), "", "jellyfin_library_manage", map[string]any{"action": "scan", "path": "/media/tv/Show A"})
		if text := resultText(t, result); result.IsError || !strings.Contains(text, "in the TV library") {
			t.Errorf("got %s", text)
		}
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
