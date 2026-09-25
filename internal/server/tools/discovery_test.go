package tools_test

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // zone rules on machines without a zone database

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func TestSearch_HappyPath(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Items" {
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Id": "item-1", "Name": "Neon Cascade", "Type": "Movie", "ProductionYear": 1997},
						{"Id": "item-2", "Name": "Neon Cascade: Afterglow", "Type": "Movie", "ProductionYear": 2004},
					},
					"TotalRecordCount": 2,
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_search", map[string]any{
		"query": "Neon",
	})

	out := structured[jf.ItemListOutput](t, result)
	if out.TotalCount != 2 || out.Shown != 2 || len(out.Items) != 2 {
		t.Errorf("total_count = %d, shown = %d, %d items, want 2 of each", out.TotalCount, out.Shown, len(out.Items))
	}
	if len(out.Items) > 0 && out.Items[0].Name != "Neon Cascade" {
		t.Errorf("items[0].name = %q, want Neon Cascade", out.Items[0].Name)
	}
}

func TestSearch_QueriesItemsWithUserIDParam(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		},
	}

	callTool(t, mc, "", "jellyfin_search", map[string]any{
		"query": "Test Movie",
		"type":  "Movie",
	})

	if gotEndpoint != "/Items" {
		t.Errorf("endpoint = %q, want /Items", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if got := gotParams.Get("searchTerm"); got != "Test Movie" {
		t.Errorf("searchTerm = %q, want Test Movie", got)
	}
	if got := gotParams.Get("IncludeItemTypes"); got != "Movie" {
		t.Errorf("IncludeItemTypes = %q, want Movie", got)
	}
	// Jellyfin 12 orders search results by relevance and ignores SortBy.
	if gotParams.Has("SortBy") || gotParams.Has("StartIndex") {
		t.Errorf("search must not send SortBy or StartIndex, got %v", gotParams)
	}
}

func TestSearch_MoreResultsHintOffersNoPaging(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "item-1", "Name": "Test Movie", "Type": "Movie"},
					{"Id": "item-2", "Name": "Test Movie 2", "Type": "Movie"},
				},
				"TotalRecordCount": 6,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_search", map[string]any{
		"query": "Test",
		"limit": 2,
	})

	out := structured[jf.ItemListOutput](t, result)
	if out.NextStartIndex != nil {
		t.Errorf("next_start_index = %d, want none: a search cannot be paged", *out.NextStartIndex)
	}
	if text := resultText(t, result); strings.Contains(text, "start_index") || strings.Contains(text, "jellyfin_browse") {
		t.Errorf("hint must not offer paging a search, got: %s", text)
	}
	for _, want := range []string{"Refine the query", "narrow it with type", "raise limit"} {
		if !hasNote(out.Notes, want) {
			t.Errorf("expected a note containing %q, got: %v", want, out.Notes)
		}
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{
				"Items":            []map[string]any{},
				"TotalRecordCount": 0,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_search", map[string]any{
		"query": "nonexistent",
	})

	out := structured[jf.ItemListOutput](t, result)
	if out.TotalCount != 0 || out.Shown != 0 || out.Items == nil || len(out.Items) != 0 {
		t.Errorf("want total_count 0, shown 0, and an empty items list, got %+v", out)
	}
	if text := resultText(t, result); !strings.Contains(text, `"items":[]`) {
		t.Errorf("want an empty items list in the text, got: %s", text)
	}
}

func TestSearch_APIError(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			if strings.Contains(endpoint, "/Items") {
				return fmt.Errorf("API error 500: Internal Server Error")
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_search", map[string]any{
		"query": "test",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "error") && !strings.Contains(text, "Error") {
		t.Errorf("expected error message, got: %s", text)
	}
	if !result.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestLibraries_HappyPath(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Library/VirtualFolders" {
				return jsonInto([]map[string]any{
					{
						"Name":           "Movies",
						"ItemId":         "lib-1",
						"CollectionType": "movies",
						"Locations":      []string{"/media/movies"},
					},
					{
						"Name":           "TV Shows",
						"ItemId":         "lib-2",
						"CollectionType": "tvshows",
						"Locations":      []string{"/media/tv"},
					},
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_libraries", nil)

	out := structured[jf.LibraryListOutput](t, result)
	if out.Count != 2 || len(out.Libraries) != 2 {
		t.Fatalf("count = %d, %d libraries, want 2 of each", out.Count, len(out.Libraries))
	}
	if want := (jf.LibraryInfo{Name: "Movies", CollectionType: "movies", ItemID: "lib-1", Paths: []string{"/media/movies"}}); out.Libraries[0].Name != want.Name || out.Libraries[0].CollectionType != want.CollectionType || out.Libraries[0].ItemID != want.ItemID || !slices.Equal(out.Libraries[0].Paths, want.Paths) {
		t.Errorf("libraries[0] = %+v, want %+v", out.Libraries[0], want)
	}
}

func TestGetItem_HappyPath(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if strings.Contains(endpoint, "/Items/") {
				return jsonInto(map[string]any{
					"Id":              "item-1",
					"Name":            "Lucid Horizon",
					"Type":            "Movie",
					"ProductionYear":  2011,
					"Overview":        "A cartographer maps a city that rearranges itself every night.",
					"Genres":          []string{"Action", "Sci-Fi"},
					"CommunityRating": 7.3,
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_get_item", map[string]any{
		"item_id": "item-1",
	})

	out := structured[jf.DetailedItemOutput](t, result)
	if out.ID != "item-1" || out.Name != "Lucid Horizon" || out.Year != 2011 || out.CommunityRating != 7.3 || !slices.Equal(out.Genres, []string{"Action", "Sci-Fi"}) {
		t.Errorf("item = %+v, want Lucid Horizon (2011), rated 7.3, Action and Sci-Fi", out)
	}
}

func TestGetItem_NotFound(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			if strings.Contains(endpoint, "/Items/") {
				return fmt.Errorf("API error 404: Not Found")
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_get_item", map[string]any{
		"item_id": "nonexistent-id",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "error") && !strings.Contains(text, "Error") {
		t.Errorf("expected error message, got: %s", text)
	}
	if !result.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestBrowse_QueriesItemsWithUserIDParam(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		},
	}

	callTool(t, mc, "", "jellyfin_browse", map[string]any{
		"genre": "Genre A",
	})

	if gotEndpoint != "/Items" {
		t.Errorf("endpoint = %q, want /Items", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if got := gotParams.Get("Genres"); got != "Genre A" {
		t.Errorf("Genres = %q, want Genre A", got)
	}
}

// A browse whose matches outrun its page says where the next page starts,
// and one that shows every match, or the last page of them, does not.
func TestBrowse_PagesWithNextStartIndex(t *testing.T) {
	for _, tt := range []struct {
		total    int
		args     map[string]any
		wantNext *int
	}{
		{7, map[string]any{"limit": 2, "start_index": 3}, ptr(5)},
		{5, map[string]any{"limit": 2, "start_index": 3}, nil},
		{2, map[string]any{"limit": 2}, nil},
	} {
		mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{
				"Items":            []map[string]any{{"Id": "item-1", "Name": "Test Movie", "Type": "Movie"}, {"Id": "item-2", "Name": "Test Movie 2", "Type": "Movie"}},
				"TotalRecordCount": tt.total,
			}, dest)
		}}
		out := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_browse", tt.args))
		if out.TotalCount != tt.total || out.Shown != 2 {
			t.Errorf("%v of %d: total_count = %d, shown = %d, want %d and 2", tt.args, tt.total, out.TotalCount, out.Shown, tt.total)
		}
		if (out.NextStartIndex == nil) != (tt.wantNext == nil) || (tt.wantNext != nil && *out.NextStartIndex != *tt.wantNext) {
			t.Errorf("%v of %d: next_start_index = %v, want %v", tt.args, tt.total, out.NextStartIndex, tt.wantNext)
		}
	}
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// A browse with no type leaves out the folders of the library's paths, which
// a recursive listing under a library includes; a typed browse asks for its
// type alone.
func TestBrowse_LeavesOutPathFolders(t *testing.T) {
	for _, tt := range []struct {
		args        map[string]any
		wantExclude string
	}{
		{map[string]any{"parent_id": "lib-1"}, "Folder"},
		{map[string]any{"parent_id": "lib-1", "type": "Folder"}, ""},
		{map[string]any{"min_date_created": "2026-01-01"}, "Folder"},
	} {
		var gotParams url.Values
		mc := &mockClient{getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
			gotParams = params
			return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		}}
		callTool(t, mc, "", "jellyfin_browse", tt.args)
		if got := gotParams.Get("ExcludeItemTypes"); got != tt.wantExclude {
			t.Errorf("%v: ExcludeItemTypes = %q, want %q", tt.args, got, tt.wantExclude)
		}
	}
}

func TestGetItem_QueriesItemWithUserIDParam(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if endpoint != "/Items/item-1/Collections" {
				gotEndpoint, gotParams = endpoint, params
			}
			return jsonInto(map[string]any{"Id": "item-1", "Name": "Test Movie", "Type": "Movie"}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_get_item", map[string]any{
		"item_id": "item-1",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	if gotEndpoint != "/Items/item-1" {
		t.Errorf("endpoint = %q, want /Items/item-1", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
}

// getItemLibrary answers jellyfin_get_item for an item with an original
// language, two chapters, and one collection, and records the requests.
type getItemLibrary struct {
	requests      map[string]url.Values
	collectionErr error
	noCollections bool
}

func (l *getItemLibrary) client(version string) *mockClient {
	l.requests = map[string]url.Values{}
	return &mockClient{
		serverVersion: version,
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			l.requests[endpoint] = params
			switch endpoint {
			case "/Items/book-1":
				return jsonInto(map[string]any{
					"Id": "book-1", "Name": "Test Audiobook", "Type": "AudioBook",
					"OriginalLanguage": "fr",
					"Chapters": []map[string]any{
						{"Name": "Chapter 1", "StartPositionTicks": 0},
						{"Name": "Chapter 2", "StartPositionTicks": 37230000000},
					},
				}, dest)
			case "/Items/book-1/Collections":
				if l.collectionErr != nil {
					return l.collectionErr
				}
				if l.noCollections {
					return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
				}
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "col-1", "Name": "Test Collection"}}, "TotalRecordCount": 1}, dest)
			}
			return fmt.Errorf("unexpected endpoint %s", endpoint)
		},
	}
}

// On Jellyfin 12, an item's details include its original language, its
// chapters with their start in ticks, and the collections that include it.
func TestGetItem_Jellyfin12Details(t *testing.T) {
	lib := &getItemLibrary{}
	out := structured[jf.DetailedItemOutput](t, callTool(t, lib.client("12.0.0"), "", "jellyfin_get_item", map[string]any{"item_id": "book-1"}))
	if out.OriginalLanguage != "fr" {
		t.Errorf("original_language = %q, want fr", out.OriginalLanguage)
	}
	wantChapters := []jf.ChapterInfo{{Name: "Chapter 1", Start: "0:00:00", StartTicks: 0}, {Name: "Chapter 2", Start: "1:02:03", StartTicks: 37230000000}}
	if !slices.Equal(out.Chapters, wantChapters) {
		t.Errorf("chapters = %+v, want %+v", out.Chapters, wantChapters)
	}
	if want := []jf.CollectionRef{{ID: "col-1", Name: "Test Collection"}}; out.IncludedInCollections == nil || !slices.Equal(*out.IncludedInCollections, want) {
		t.Errorf("included_in_collections = %+v, want %+v", out.IncludedInCollections, want)
	}
	if got := lib.requests["/Items/book-1/Collections"].Get("UserId"); got != "test-user-id" {
		t.Errorf("collections UserId = %q, want test-user-id", got)
	}
}

// Jellyfin 10.11 has no collections route, so none is requested; a failed
// request leaves the collections out, says so, and still returns the item.
func TestGetItem_CollectionsNeedJellyfin12(t *testing.T) {
	for _, tt := range []struct {
		name, version string
		err           error
		wantRequest   bool
		wantNote      bool
	}{
		{"10.11", "10.11.6", nil, false, false},
		{"a failed request", "12.1.0", fmt.Errorf("API error 500: failed"), true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lib := &getItemLibrary{collectionErr: tt.err}
			result := callTool(t, lib.client(tt.version), "", "jellyfin_get_item", map[string]any{"item_id": "book-1"})
			out := structured[jf.DetailedItemOutput](t, result)
			if _, requested := lib.requests["/Items/book-1/Collections"]; requested != tt.wantRequest {
				t.Errorf("collections requested = %v, want %v", requested, tt.wantRequest)
			}
			if out.IncludedInCollections != nil {
				t.Errorf("included_in_collections = %+v, want none", out.IncludedInCollections)
			}
			if got := hasNote(out.Notes, "The collections that include this item could not be listed") && hasNote(out.Notes, "API error 500"); got != tt.wantNote {
				t.Errorf("failure note present = %v, want %v: %v", got, tt.wantNote, out.Notes)
			}
			text := resultText(t, result)
			if strings.Contains(text, `"included_in_collections"`) {
				t.Errorf("the text lists collections that were not read: %s", text)
			}
			if got := strings.Contains(text, "The collections that include this item could not be listed") && strings.Contains(text, "API error 500"); got != tt.wantNote {
				t.Errorf("failure note shown = %v, want %v: %s", got, tt.wantNote, text)
			}
			if len(out.Chapters) != 2 || out.OriginalLanguage != "fr" {
				t.Errorf("the item's other details are missing: %+v", out)
			}
		})
	}
}

// On Jellyfin 12 an item in no collection says so with an empty list, and the
// item resource lists collections like the tool.
func TestGetItem_NoCollectionsIsAnEmptyList(t *testing.T) {
	lib := &getItemLibrary{noCollections: true}
	result := callTool(t, lib.client("12.1.0"), "", "jellyfin_get_item", map[string]any{"item_id": "book-1"})
	if out := structured[jf.DetailedItemOutput](t, result); out.IncludedInCollections == nil || len(*out.IncludedInCollections) != 0 {
		t.Errorf("included_in_collections = %v, want an empty list", out.IncludedInCollections)
	}
	if text := resultText(t, result); !strings.Contains(text, `"included_in_collections":[]`) {
		t.Errorf("want an empty list in the text: %s", text)
	}

	lib = &getItemLibrary{}
	read, err := newFullSession(t, lib.client("12.1.0")).ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://items/book-1"})
	if err != nil {
		t.Fatal(err)
	}
	if text := read.Contents[0].Text; !strings.Contains(text, `"name": "Test Collection"`) {
		t.Errorf("the item resource does not list the collections: %s", text)
	}
}

func TestRecommendationsRecentlyPlayed_QueriesItemsWithUserIDParam(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "item-1", "Name": "Test Movie", "Type": "Movie", "UserData": map[string]any{"LastPlayedDate": "2026-01-02T03:04:05Z"}},
				},
				"TotalRecordCount": 1,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_recommendations", map[string]any{
		"action": "recently_played",
	})

	out := structured[jf.RecommendationsOutput](t, result)
	if gotEndpoint != "/Items" {
		t.Errorf("endpoint = %q, want /Items", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if got := gotParams.Get("IsPlayed"); got != "true" {
		t.Errorf("IsPlayed = %q, want true", got)
	}
	if out.TotalCount == nil || *out.TotalCount != 1 || out.Items == nil || len(*out.Items) != 1 {
		t.Fatalf("want total_count 1 and one item, got %+v", out)
	}
	if got := (*out.Items)[0].LastPlayed; got != "2026-01-02" {
		t.Errorf("items[0].last_played = %q, want 2026-01-02", got)
	}
}

func TestItemExtras_UserScopedExtrasQueryItemWithUserIDParam(t *testing.T) {
	tests := []struct {
		action       string
		wantEndpoint string
		wantText     string
	}{
		{"special_features", "/Items/item-1/SpecialFeatures", "Special features (1)"},
		{"local_trailers", "/Items/item-1/LocalTrailers", "Local trailers (1)"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			var gotEndpoint string
			var gotParams url.Values
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
					gotEndpoint, gotParams = endpoint, params
					return jsonInto([]map[string]any{
						{"Id": "extra-1", "Name": "Test Extra", "Type": "Video"},
					}, dest)
				},
			}

			result := callTool(t, mc, "", "jellyfin_item_extras", map[string]any{
				"action":  tt.action,
				"item_id": "item-1",
			})

			text := resultText(t, result)
			if result.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			if gotEndpoint != tt.wantEndpoint {
				t.Errorf("endpoint = %q, want %s", gotEndpoint, tt.wantEndpoint)
			}
			if got := gotParams.Get("UserId"); got != "test-user-id" {
				t.Errorf("UserId = %q, want test-user-id", got)
			}
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("expected %q in result, got: %s", tt.wantText, text)
			}
		})
	}
}

// createdLibrary is a fake /Items endpoint over items stored newest first by
// DateCreated. Like Jellyfin, it ignores parameters it does not know, honors
// StartIndex and Limit, and returns DateCreated only when Fields asks for it.
// Unless the request sorts by DateCreated descending, it lists items oldest
// first.
type createdLibrary struct {
	items    []map[string]any
	requests []url.Values
}

func (l *createdLibrary) client() *mockClient {
	return &mockClient{getFunc: l.get}
}

func (l *createdLibrary) get(_ context.Context, endpoint string, params url.Values, dest any) error {
	if endpoint != "/Items" {
		return fmt.Errorf("unexpected endpoint %s", endpoint)
	}
	sent, _ := url.ParseQuery(params.Encode())
	l.requests = append(l.requests, sent)

	ordered := l.items
	if params.Get("SortBy") != "DateCreated" || params.Get("SortOrder") != "Descending" {
		ordered = make([]map[string]any, 0, len(l.items))
		for i := len(l.items) - 1; i >= 0; i-- {
			ordered = append(ordered, l.items[i])
		}
	}
	start, _ := strconv.Atoi(params.Get("StartIndex"))
	limit := len(ordered)
	if params.Has("Limit") {
		limit, _ = strconv.Atoi(params.Get("Limit"))
	}
	withDate := slices.Contains(strings.Split(params.Get("Fields"), ","), "DateCreated")
	page := []map[string]any{}
	for i := start; i < len(ordered) && i < start+limit; i++ {
		item := maps.Clone(ordered[i])
		if !withDate {
			delete(item, "DateCreated")
		}
		page = append(page, item)
	}
	return jsonInto(map[string]any{"Items": page, "TotalRecordCount": len(ordered)}, dest)
}

// browseLibrary holds six movies, newest first, with DateCreated written in
// the forms Jellyfin emits.
func browseLibrary() *createdLibrary {
	return &createdLibrary{items: []map[string]any{
		{"Id": "item-1", "Name": "Test Movie 1", "Type": "Movie", "DateCreated": "2026-03-10T12:00:00.1234567Z"},
		{"Id": "item-2", "Name": "Test Movie 2", "Type": "Movie", "DateCreated": "2026-03-05T00:00:00.0000000Z"},
		{"Id": "item-3", "Name": "Test Movie 3", "Type": "Movie", "DateCreated": "2026-02-20T08:00:00.5Z"},
		{"Id": "item-4", "Name": "Test Movie 4", "Type": "Movie", "DateCreated": "2026-02-01T10:00:00Z"},
		{"Id": "item-5", "Name": "Test Movie 5", "Type": "Movie", "DateCreated": "2026-01-15T23:59:59.9999999Z"},
		{"Id": "item-6", "Name": "Test Movie 6", "Type": "Movie", "DateCreated": "2025-12-31T23:59:59.9999999"},
	}}
}

// browseIDs returns the item IDs of a jellyfin_browse result in order.
func browseIDs(t *testing.T, result *mcp.CallToolResult) []string {
	t.Helper()
	out := structured[jf.ItemListOutput](t, result)
	ids := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

// inZone sets the server's time zone for the rest of the test.
func inZone(t *testing.T, loc *time.Location) {
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
}

func TestBrowse_DateCreatedWindow(t *testing.T) {
	tests := []struct {
		name    string
		zone    *time.Location // the server's time zone; nil means UTC
		args    map[string]any
		wantIDs []string
	}{
		{
			name:    "min only, a date starts at midnight",
			args:    map[string]any{"min_date_created": "2026-03-05"},
			wantIDs: []string{"item-1", "item-2"},
		},
		{
			name:    "max only, a date includes the whole day",
			args:    map[string]any{"max_date_created": "2026-01-15"},
			wantIDs: []string{"item-5", "item-6"},
		},
		{
			name:    "both, with an exact timestamp",
			args:    map[string]any{"min_date_created": "2026-02-01T12:00:00Z", "max_date_created": "2026-03-05"},
			wantIDs: []string{"item-2", "item-3"},
		},
		{
			name:    "timestamp with an offset",
			args:    map[string]any{"min_date_created": "2026-02-20T09:00:00+02:00", "max_date_created": "2026-02-20T09:00:01+01:00"},
			wantIDs: []string{"item-3"},
		},
		{
			name:    "a date starts at midnight in the server's time zone",
			zone:    time.FixedZone("UTC-7", -7*60*60),
			args:    map[string]any{"min_date_created": "2026-03-05"},
			wantIDs: []string{"item-1"},
		},
		{
			name:    "a date ends at midnight in the server's time zone",
			zone:    time.FixedZone("UTC+9", 9*60*60),
			args:    map[string]any{"max_date_created": "2026-01-15"},
			wantIDs: []string{"item-6"},
		},
		{
			name:    "sort_by DateCreated is allowed",
			args:    map[string]any{"min_date_created": "2026-02-01", "sort_by": "DateCreated", "sort_order": "descending"},
			wantIDs: []string{"item-1", "item-2", "item-3", "item-4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.zone == nil {
				tt.zone = time.UTC
			}
			inZone(t, tt.zone)
			lib := browseLibrary()
			result := callTool(t, lib.client(), "", "jellyfin_browse", tt.args)
			if got := browseIDs(t, result); !slices.Equal(got, tt.wantIDs) {
				t.Errorf("items = %v, want %v", got, tt.wantIDs)
			}
			out := structured[jf.ItemListOutput](t, result)
			// Every case fits in one page, so the total is what is shown.
			if want := len(tt.wantIDs); out.TotalCount != want || out.Shown != want || out.NextStartIndex != nil {
				t.Errorf("total_count = %d, shown = %d, next_start_index = %v, want %d, %d, and none", out.TotalCount, out.Shown, out.NextStartIndex, want, want)
			}
			for _, item := range out.Items {
				if item.DateAdded == "" {
					t.Errorf("item %s has no date_added", item.ID)
				}
			}
			if out.TotalIsLowerBound {
				t.Errorf("total_is_lower_bound is set for a scan that reached the end of the list")
			}
			for _, q := range lib.requests {
				if q.Has("MinDateCreated") || q.Has("MaxDateCreated") {
					t.Errorf("sent a creation-date parameter /Items does not have: %v", q)
				}
				if q.Get("UserId") != "test-user-id" {
					t.Errorf("UserId = %q, want test-user-id", q.Get("UserId"))
				}
			}
		})
	}
}

// date_added is the day in the server's time zone, the zone the window is
// read in, so an item inside a one-day window is labeled with that day.
func TestBrowse_DateAddedIsTheDayOfTheWindow(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	result := callTool(t, browseLibrary().client(), "", "jellyfin_browse", map[string]any{
		"min_date_created": "2026-03-04",
		"max_date_created": "2026-03-04",
	})
	if got, want := browseIDs(t, result), []string{"item-2"}; !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	out := structured[jf.ItemListOutput](t, result)
	if got := out.Items[0].DateAdded; got != "2026-03-04" {
		t.Errorf("date_added = %q, want 2026-03-04", got)
	}
}

// Where daylight saving time starts at midnight, the day starts at 01:00. In
// Santiago on 2023-09-03 the clocks moved from 00:00 -04 to 01:00 -03, so the
// day before ends at 04:00 UTC.
func TestBrowse_DateWindowWhereClocksSkipMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	inZone(t, loc)
	library := func() *createdLibrary {
		return &createdLibrary{items: []map[string]any{
			{"Id": "day-3", "Name": "Test Movie 1", "Type": "Movie", "DateCreated": "2023-09-03T04:30:00Z"}, // 01:30 -03
			{"Id": "day-2", "Name": "Test Movie 2", "Type": "Movie", "DateCreated": "2023-09-03T03:30:00Z"}, // 23:30 -04 the day before
		}}
	}
	for _, tt := range []struct {
		args    map[string]any
		wantIDs []string
	}{
		{map[string]any{"min_date_created": "2023-09-03"}, []string{"day-3"}},
		{map[string]any{"max_date_created": "2023-09-02"}, []string{"day-2"}},
	} {
		result := callTool(t, library().client(), "", "jellyfin_browse", tt.args)
		if got := browseIDs(t, result); !slices.Equal(got, tt.wantIDs) {
			t.Errorf("%v: items = %v, want %v", tt.args, got, tt.wantIDs)
		}
	}
}

// languageLibrary answers /Items with no items unless found is set, and
// /Items/Filters2 with the languages the library has. It records requests by
// endpoint.
type languageLibrary struct {
	found    bool
	requests map[string][]url.Values
}

func (l *languageLibrary) client(version string) *mockClient {
	l.requests = map[string][]url.Values{}
	return &mockClient{
		serverVersion: version,
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			sent, _ := url.ParseQuery(params.Encode())
			l.requests[endpoint] = append(l.requests[endpoint], sent)
			switch endpoint {
			case "/Items":
				items := []map[string]any{}
				if l.found {
					items = append(items, map[string]any{"Id": "item-1", "Name": "Test Movie", "Type": "Movie", "DateCreated": "2026-03-10T12:00:00Z"})
				}
				return jsonInto(map[string]any{"Items": items, "TotalRecordCount": len(items)}, dest)
			case "/Items/Filters2":
				return jsonInto(map[string]any{
					"AudioLanguages":    []map[string]any{{"Name": "English (eng)", "Value": "eng"}, {"Name": "Greek, Modern (1453-) (gre)", "Value": "gre"}, {"Name": "xyz", "Value": "xyz"}},
					"SubtitleLanguages": []map[string]any{{"Name": "Spanish (spa)", "Value": "spa"}},
				}, dest)
			}
			return fmt.Errorf("unexpected endpoint %s", endpoint)
		},
	}
}

// Language filters are sent to Jellyfin 12 as trimmed comma-separated codes.
func TestBrowse_LanguageFilters(t *testing.T) {
	lib := &languageLibrary{found: true}
	result := callTool(t, lib.client("12.0.0"), "", "jellyfin_browse", map[string]any{
		"audio_languages":    " eng, jpn ,",
		"subtitle_languages": "spa",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	q := lib.requests["/Items"][0]
	if q.Get("AudioLanguages") != "eng,jpn" || q.Get("SubtitleLanguages") != "spa" {
		t.Errorf("sent AudioLanguages %q and SubtitleLanguages %q, want eng,jpn and spa", q.Get("AudioLanguages"), q.Get("SubtitleLanguages"))
	}
	if _, asked := lib.requests["/Items/Filters2"]; asked {
		t.Error("asked for the library's languages although items matched")
	}
}

// Jellyfin 10.11 ignores the language parameters, so the browse is refused
// rather than answered unfiltered.
func TestBrowse_LanguageFiltersNeedJellyfin12(t *testing.T) {
	for _, arg := range []string{"audio_languages", "subtitle_languages"} {
		lib := &languageLibrary{found: true}
		result := callTool(t, lib.client("10.11.6"), "", "jellyfin_browse", map[string]any{arg: "eng"})
		if !result.IsError || !strings.Contains(resultText(t, result), "need Jellyfin 12 or later. This server runs Jellyfin 10.11.6.") {
			t.Errorf("%s on 10.11: %s", arg, resultText(t, result))
		}
		if len(lib.requests["/Items"]) != 0 {
			t.Errorf("%s on 10.11 browsed anyway", arg)
		}
	}
}

// When a language filter matches nothing because a requested code does not
// occur in the part of the library browsed, the result names that code and
// lists the codes that do occur. Jellyfin lists stream languages only for
// video types, so a browse names one, or asks about movies, series, and
// episodes.
func TestBrowse_LanguageFilterMatchingNothingListsLanguages(t *testing.T) {
	const audioHint = "No item here has audio tracks in fre. The audio languages here are eng (English), gre (Greek, Modern (1453-)), xyz."
	for _, tt := range []struct {
		name      string
		args      map[string]any
		wantTypes string // the Filters2 query's types; empty means no query
		want      []string
		notWant   []string
	}{
		{"no type", map[string]any{"audio_languages": "fre", "parent_id": "lib-1"}, "Movie,Series,Episode", []string{audioHint}, nil},
		{"a video type", map[string]any{"audio_languages": "eng,fre", "type": "Episode"}, "Episode", []string{audioHint}, nil},
		{"a season", map[string]any{"subtitle_languages": "fre", "type": "Season"}, "Season", []string{"No item here has subtitle tracks in fre. The subtitle languages here are spa (Spanish)."}, []string{"audio tracks"}},
		{"by date added", map[string]any{"audio_languages": "fre", "min_date_created": "2026-01-01"}, "Movie,Series,Episode", []string{audioHint}, nil},
		{"every code occurs", map[string]any{"audio_languages": "eng", "genre": "Genre A"}, "Movie,Series,Episode", nil, []string{"No item here"}},
		{"not a video type", map[string]any{"audio_languages": "fre", "type": "Audio"}, "", nil, []string{"No item here"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lib := &languageLibrary{}
			out := structured[jf.ItemListOutput](t, callTool(t, lib.client("12.1.0"), "", "jellyfin_browse", tt.args))
			if out.TotalCount != 0 || out.Shown != 0 || out.Items == nil || len(out.Items) != 0 {
				t.Errorf("want total_count 0, shown 0, and an empty items list, got %+v", out)
			}
			for _, want := range tt.want {
				if !hasNote(out.Notes, want) {
					t.Errorf("want a note containing %q in: %v", want, out.Notes)
				}
			}
			for _, notWant := range tt.notWant {
				if hasNote(out.Notes, notWant) {
					t.Errorf("want no note containing %q in: %v", notWant, out.Notes)
				}
			}
			asked := lib.requests["/Items/Filters2"]
			if tt.wantTypes == "" {
				if len(asked) != 0 {
					t.Errorf("asked Filters2 %v for a type it has no languages for", asked)
				}
				return
			}
			if len(asked) == 0 {
				t.Fatal("did not ask for the library's languages")
			}
			if q := asked[0]; q.Get("IncludeItemTypes") != tt.wantTypes || q.Get("UserId") != "test-user-id" || q.Get("ParentId") != jf.GetString(tt.args, "parent_id") {
				t.Errorf("Filters2 query %v, want types %s", q, tt.wantTypes)
			}
		})
	}
	lib := &languageLibrary{}
	callTool(t, lib.client("12.1.0"), "", "jellyfin_browse", map[string]any{"genre": "Genre A"})
	if _, asked := lib.requests["/Items/Filters2"]; asked {
		t.Error("asked for the library's languages without a language filter")
	}
}

// A language list with no codes, and subtitle languages with
// has_subtitles=false, which Jellyfin would ignore, are refused rather than
// answered unfiltered.
func TestBrowse_LanguageFiltersRefuseWhatJellyfinWouldIgnore(t *testing.T) {
	for _, tt := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"audio_languages": " , "}, `audio_languages has no language codes: " , "`},
		{map[string]any{"subtitle_languages": ","}, `subtitle_languages has no language codes: ","`},
		{map[string]any{"subtitle_languages": "eng", "has_subtitles": false}, "subtitle_languages cannot be combined with has_subtitles=false"},
	} {
		lib := &languageLibrary{found: true}
		result := callTool(t, lib.client("12.1.0"), "", "jellyfin_browse", tt.args)
		if !result.IsError || !strings.Contains(resultText(t, result), tt.want) {
			t.Errorf("%v: %s", tt.args, resultText(t, result))
		}
		if len(lib.requests["/Items"]) != 0 {
			t.Errorf("%v browsed anyway", tt.args)
		}
	}
}

func TestBrowse_DateCreatedPagesTheFilteredList(t *testing.T) {
	lib := browseLibrary()
	result := callTool(t, lib.client(), "", "jellyfin_browse", map[string]any{
		"min_date_created": "2026-01-01",
		"max_date_created": "2026-03-06",
		"start_index":      1,
		"limit":            2,
	})
	if got, want := browseIDs(t, result), []string{"item-3", "item-4"}; !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	out := structured[jf.ItemListOutput](t, result)
	if out.TotalCount != 4 || out.Shown != 2 {
		t.Errorf("total_count = %d, shown = %d, want 4 and 2", out.TotalCount, out.Shown)
	}
	if out.NextStartIndex == nil || *out.NextStartIndex != 3 {
		t.Errorf("next_start_index = %v, want 3", out.NextStartIndex)
	}
	for _, q := range lib.requests {
		if q.Get("StartIndex") != "0" {
			t.Errorf("start_index must page the filtered list, not the server's: sent StartIndex=%s", q.Get("StartIndex"))
		}
	}
}

func TestBrowse_DateCreatedRejectsOtherOrders(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"sort_by", map[string]any{"min_date_created": "2026-01-01", "sort_by": "SortName"}, "sort_by must be omitted or DateCreated"},
		{"sort_order", map[string]any{"max_date_created": "2026-01-01", "sort_order": "Ascending"}, "sort_order must be omitted or Descending"},
		{"malformed date", map[string]any{"min_date_created": "last week"}, "min_date_created must be a date"},
		{"reversed window", map[string]any{"min_date_created": "2026-02-01", "max_date_created": "2026-01-01"}, "is later than max_date_created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lib := browseLibrary()
			result := callTool(t, lib.client(), "", "jellyfin_browse", tt.args)
			text := resultText(t, result)
			if !result.IsError || !strings.Contains(text, tt.want) {
				t.Errorf("expected an error containing %q, got: %s", tt.want, text)
			}
			if len(lib.requests) != 0 {
				t.Errorf("made %d requests for an invalid request", len(lib.requests))
			}
		})
	}
}

func TestBrowse_DateCreatedLeavesOutUndatedItems(t *testing.T) {
	lib := browseLibrary()
	lib.items[1] = map[string]any{"Id": "item-undated", "Name": "Test Movie 7", "Type": "Movie"}
	result := callTool(t, lib.client(), "", "jellyfin_browse", map[string]any{"min_date_created": "2026-02-01"})
	if got, want := browseIDs(t, result), []string{"item-1", "item-3", "item-4"}; !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	if out := structured[jf.ItemListOutput](t, result); out.UndatedCount != 1 {
		t.Errorf("undated_count = %d, want 1", out.UndatedCount)
	}
}

func TestBrowse_DateCreatedReportsTheScanBound(t *testing.T) {
	newest := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	lib := &createdLibrary{}
	for i := range jf.DefaultMaxItems + 100 {
		lib.items = append(lib.items, map[string]any{
			"Id":          fmt.Sprintf("item-%d", i),
			"Name":        fmt.Sprintf("Test Movie %d", i),
			"Type":        "Movie",
			"DateCreated": newest.Add(-time.Duration(i) * time.Minute).Format("2006-01-02T15:04:05.0000000Z"),
		})
	}
	result := callTool(t, lib.client(), "", "jellyfin_browse", map[string]any{"max_date_created": "2026-03-01"})
	out := structured[jf.ItemListOutput](t, result)
	if out.TotalCount != jf.DefaultMaxItems || !out.TotalIsLowerBound || out.Shown != 50 {
		t.Errorf("total_count = %d, total_is_lower_bound = %v, shown = %d, want %d, true, and 50", out.TotalCount, out.TotalIsLowerBound, out.Shown, jf.DefaultMaxItems)
	}
	if out.NextStartIndex == nil || *out.NextStartIndex != 50 {
		t.Errorf("next_start_index = %v, want 50", out.NextStartIndex)
	}
	if want := fmt.Sprintf("Only the %d most recently added items were examined", jf.DefaultMaxItems); !hasNote(out.Notes, want) {
		t.Errorf("expected a note containing %q, got: %v", want, out.Notes)
	}
	scanned := 0
	for _, q := range lib.requests {
		n, _ := strconv.Atoi(q.Get("Limit"))
		scanned += n
	}
	if scanned != jf.DefaultMaxItems {
		t.Errorf("requested %d items in total, want the scan bound %d", scanned, jf.DefaultMaxItems)
	}
}

// An item added while the scan pages through the list pushes the previous
// page's last item onto the next page, so the server can return it twice.
func TestBrowse_DateCreatedCountsEachItemOnce(t *testing.T) {
	lib := browseLibrary()
	lib.items = append(lib.items[:2], append([]map[string]any{lib.items[1]}, lib.items[2:]...)...)

	result := callTool(t, lib.client(), "", "jellyfin_browse", map[string]any{
		"min_date_created": "2026-02-01",
	})
	got := browseIDs(t, result)
	want := []string{"item-1", "item-2", "item-3", "item-4"}
	if !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
}

// Jellyfin 12 reports the size of its ranked candidate window, at most three
// times the limit, as the search total, so a full window is a lower bound.
func TestSearch_FullCandidateWindowIsLowerBound(t *testing.T) {
	for _, tt := range []struct {
		version string
		total   int
		want    bool
	}{
		{"12.1.0", 6, true},
		{"12.1.0", 5, false},
		{"10.11.6", 6, false},
	} {
		mc := &mockClient{
			serverVersion: tt.version,
			getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "item-1", "Name": "Test Movie", "Type": "Movie"}, {"Id": "item-2", "Name": "Test Movie 2", "Type": "Movie"}},
					"TotalRecordCount": tt.total,
				}, dest)
			},
		}
		out := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_search", map[string]any{"query": "Test", "limit": 2}))
		if out.TotalIsLowerBound != tt.want || out.TotalCount != tt.total {
			t.Errorf("%s total %d: structured %+v", tt.version, tt.total, out)
		}
	}
}

// Jellyfin 12 matches stream filters against each version and part of a
// title, so a browse using them says that a title can appear more than once.
func TestBrowse_StreamFiltersNotePerVersionMatchesOnJellyfin12(t *testing.T) {
	items := func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto(map[string]any{
			"Items":            []map[string]any{{"Id": "item-1", "Name": "Test Movie", "Type": "Movie"}},
			"TotalRecordCount": 1,
		}, dest)
	}
	for _, tt := range []struct {
		version string
		args    map[string]any
		want    bool
	}{
		{"12.1.0", map[string]any{"has_subtitles": false}, true},
		{"12.1.0", map[string]any{"audio_languages": "jpn"}, true},
		{"12.1.0", map[string]any{"genre": "Genre A"}, false},
		{"10.11.6", map[string]any{"has_subtitles": true}, false},
	} {
		out := structured[jf.ItemListOutput](t, callTool(t, &mockClient{serverVersion: tt.version, getFunc: items}, "", "jellyfin_browse", tt.args))
		if got := hasNote(out.Notes, "can appear more than once"); got != tt.want {
			t.Errorf("%s %v: note present = %v, want %v: %v", tt.version, tt.args, got, tt.want, out.Notes)
		}
	}
}

// The download link never carries the API key. It points at the item's page in
// the Jellyfin web app, where the signed-in user downloads with their own account.
func TestDownloadLink_CarriesNoKey(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if endpoint != "/Items/item-1" || params.Get("UserId") != "test-user-id" {
				t.Errorf("GET %s %v", endpoint, params)
			}
			return jsonInto(map[string]any{
				"Id": "item-1", "Name": "Test Movie", "Path": "/media/movies/test-movie.mkv",
				"MediaSources": []map[string]any{{"Size": 1500}},
			}, dest)
		},
	}
	result := callTool(t, mc, "", "jellyfin_download_link", map[string]any{"item_id": "item-1"})
	text := resultText(t, result)
	if result.IsError {
		t.Fatal(text)
	}
	for _, banned := range []string{"ApiKey", "api_key", "/Download"} {
		if strings.Contains(text, banned) {
			t.Errorf("result contains %q: %s", banned, text)
		}
	}
	for _, want := range []string{`"web_page": "http://localhost:8096/web/#/details?id=item-1"`, `"file_path": "/media/movies/test-movie.mkv"`, `"file_size_bytes": 1500`, "Download button"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in result, got: %s", want, text)
		}
	}
}

// People, artists, and genres are not library items, so a search for one of
// those types goes to the endpoint that lists them by name.
func TestSearch_NameTypesUseTheirOwnEndpoints(t *testing.T) {
	for typ, endpoint := range map[string]string{"Person": "/Persons", "MusicArtist": "/Artists", "Genre": "/Genres", "Movie": "/Items"} {
		var gotEndpoint string
		var gotParams url.Values
		mc := &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint, gotParams = endpoint, params
			return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "x-1", "Name": "Person A", "Type": typ}}, "TotalRecordCount": 1}, dest)
		}}
		result := callTool(t, mc, "", "jellyfin_search", map[string]any{"query": "Person", "type": typ})
		if result.IsError || gotEndpoint != endpoint || gotParams.Get("searchTerm") != "Person" {
			t.Errorf("%s: endpoint %q params %v: %s", typ, gotEndpoint, gotParams, resultText(t, result))
		}
		if typ == "Movie" && gotParams.Get("IncludeItemTypes") != "Movie" {
			t.Errorf("Movie: IncludeItemTypes = %q", gotParams.Get("IncludeItemTypes"))
		}
		if typ != "Movie" && gotParams.Has("IncludeItemTypes") {
			t.Errorf("%s: IncludeItemTypes sent to %s", typ, endpoint)
		}
	}
}

// With user_id, browse queries as that user and says whose play state the
// results show, including through the date-added path.
func TestBrowse_AnotherUsersView(t *testing.T) {
	for _, args := range []map[string]any{
		{"user_id": "user-b", "is_played": true, "sort_by": "DatePlayed"},
		{"user_id": "user-b", "min_date_created": "2026-01-01"},
	} {
		var itemsUser string
		mc := &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users/user-b":
				return jsonInto(map[string]any{"Id": "user-b-id", "Name": "Sam"}, dest)
			case "/Items":
				itemsUser = params.Get("UserId")
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "m1", "Name": "Test Movie", "Type": "Movie", "DateCreated": "2026-02-01T00:00:00Z"}}, "TotalRecordCount": 1}, dest)
			}
			return fmt.Errorf("unexpected endpoint %s", endpoint)
		}}
		out := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_browse", args))
		if itemsUser != "user-b-id" {
			t.Errorf("%v: /Items UserId = %q, want user-b-id", args, itemsUser)
		}
		if len(out.Notes) == 0 || !strings.Contains(out.Notes[0], "user 'Sam'") {
			t.Errorf("%v: notes = %q, want the first to name user 'Sam'", args, out.Notes)
		}
	}
}

// An unknown user_id is refused with the way to find IDs, before any item
// query.
func TestBrowse_UnknownUserID(t *testing.T) {
	queried := false
	mc := &mockClient{getFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
		if endpoint == "/Items" {
			queried = true
		}
		return &jf.APIError{StatusCode: 404}
	}}
	res := callTool(t, mc, "", "jellyfin_browse", map[string]any{"user_id": "nobody"})
	if text := resultText(t, res); !res.IsError || !strings.Contains(text, "No user has ID 'nobody'") || !strings.Contains(text, "jellyfin_users") {
		t.Errorf("result = %q (error %v)", text, res.IsError)
	}
	if queried {
		t.Error("items were queried for an unknown user")
	}
}
