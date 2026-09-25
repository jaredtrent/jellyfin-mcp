package tools_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// analyticsGetCall records one Get request made through mockClient.
type analyticsGetCall struct {
	endpoint string
	userID   string
}

func TestAnalytics_ListActionsQueryItemsWithUserID(t *testing.T) {
	for _, action := range []string{
		"library_stats",
		"library_size",
		"size_report",
		"codec_report",
		"never_played",
		"recently_added",
		"duplicate_check",
	} {
		t.Run(action, func(t *testing.T) {
			var calls []analyticsGetCall
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
					calls = append(calls, analyticsGetCall{endpoint: endpoint, userID: params.Get("UserId")})
					return jsonInto(map[string]any{
						"Items":            []map[string]any{},
						"TotalRecordCount": 0,
					}, dest)
				},
			}

			result := callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": action})
			if result.IsError {
				t.Fatalf("unexpected error result: %s", resultText(t, result))
			}
			if len(calls) == 0 {
				t.Fatal("expected at least one Get request")
			}
			for _, c := range calls {
				if c.endpoint != "/Items" {
					t.Errorf("endpoint = %q, want %q", c.endpoint, "/Items")
				}
				if c.userID != "test-user-id" {
					t.Errorf("UserId query parameter = %q, want %q", c.userID, "test-user-id")
				}
			}
		})
	}
}

func TestAnalytics_DuplicateCheck_ItemsEndpoint(t *testing.T) {
	var calls []analyticsGetCall
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			calls = append(calls, analyticsGetCall{endpoint: endpoint, userID: params.Get("UserId")})
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "movie-1", "Name": "Test Movie", "ProductionYear": 2001, "Path": "/media/a/test-movie.mkv"},
					{"Id": "movie-2", "Name": "Test Movie", "ProductionYear": 2001, "Path": "/media/b/test-movie.mkv"},
					{"Id": "movie-3", "Name": "Other Movie", "ProductionYear": 2002},
				},
				"TotalRecordCount": 3,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "duplicate_check"})

	out := structured[jf.AnalyticsOutput](t, result)
	if out.Duplicates == nil || len(*out.Duplicates) != 1 {
		t.Errorf("expected one duplicate group, got: %+v", out.Duplicates)
	} else if g := (*out.Duplicates)[0]; g.Name != "Test Movie" || g.Year != 2001 || g.Count != 2 || len(g.Copies) != 2 {
		t.Errorf("duplicates[0] = %+v, want Test Movie (2001) with 2 copies", g)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 Get request, got %d: %+v", len(calls), calls)
	}
	if calls[0].endpoint != "/Items" || calls[0].userID != "test-user-id" {
		t.Errorf("request = %+v, want endpoint /Items with UserId test-user-id", calls[0])
	}
}

func TestAnalytics_PlayedStatus_PerUserItemLookup(t *testing.T) {
	var calls []analyticsGetCall
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			uid := params.Get("UserId")
			calls = append(calls, analyticsGetCall{endpoint: endpoint, userID: uid})
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "user-a", "Name": "User A"},
					{"Id": "user-b", "Name": "User B"},
				}, dest)
			case "/Items/series-1":
				item := map[string]any{"Id": "series-1", "Name": "Test Series", "Type": "Series"}
				switch uid {
				case "user-a":
					item["UserData"] = map[string]any{"Played": true, "PlayCount": 2, "UnplayedItemCount": 0}
				case "user-b":
					item["UserData"] = map[string]any{"Played": false, "UnplayedItemCount": 6}
				}
				return jsonInto(item, dest)
			case "/Items":
				return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 10}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_analytics", map[string]any{
		"action":  "played_status",
		"item_id": "series-1",
	})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, result))
	}

	want := []analyticsGetCall{
		{endpoint: "/Users"},
		{endpoint: "/Items/series-1", userID: "test-user-id"},
		{endpoint: "/Items", userID: "test-user-id"},
		{endpoint: "/Items/series-1", userID: "user-a"},
		{endpoint: "/Items/series-1", userID: "user-b"},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d Get requests, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, calls[i], want[i])
		}
	}

	out := structured[jf.AnalyticsOutput](t, result)
	if out.Users == nil || len(*out.Users) != 2 {
		t.Fatalf("users = %+v, want two", out.Users)
	}
	if b := (*out.Users)[1]; b.Name != "User B" || b.EpisodesPlayed == nil || *b.EpisodesPlayed != 4 || b.TotalEpisodes != 10 {
		t.Errorf("users[1] = %+v, want User B with 4 of 10 episodes played", b)
	}
}

// recentLibrary holds total items, newest first. The first recent items were
// created an hour apart starting an hour ago; the rest were created more than
// 40 days ago.
func recentLibrary(recent, total int) *createdLibrary {
	now := time.Now().UTC()
	lib := &createdLibrary{}
	for i := range total {
		created := now.Add(-time.Duration(i+1) * time.Hour)
		if i >= recent {
			created = now.AddDate(0, 0, -40).Add(-time.Duration(i) * time.Hour)
		}
		lib.items = append(lib.items, map[string]any{
			"Id":          fmt.Sprintf("item-%d", i),
			"Name":        fmt.Sprintf("Test Movie %d", i),
			"Type":        "Movie",
			"DateCreated": created.Format("2006-01-02T15:04:05.0000000Z"),
		})
	}
	return lib
}

func TestAnalytics_RecentlyAdded_CountsTheWindowAndStops(t *testing.T) {
	lib := recentLibrary(210, 1000)
	result := callTool(t, lib.client(), "", "jellyfin_analytics", map[string]any{
		"action": "recently_added",
		"days":   30,
		"limit":  5,
	})
	out := structured[jf.AnalyticsOutput](t, result)
	if out.TotalCount == nil || *out.TotalCount != 210 || out.Shown == nil || *out.Shown != 5 || out.Items == nil || len(*out.Items) != 5 {
		t.Fatalf("want total_count 210, shown 5, and 5 items, got %+v", out)
	}
	if got := analyticsItemIDs(out); got[0] != "item-0" || got[4] != "item-4" {
		t.Errorf("items = %v, want item-0 through item-4", got)
	}
	if !hasNote(out.Notes, "More results available") {
		t.Errorf("expected a note that more results are available, got: %v", out.Notes)
	}

	var starts []string
	for _, q := range lib.requests {
		starts = append(starts, q.Get("StartIndex"))
		if q.Has("MinDateCreated") {
			t.Errorf("sent MinDateCreated, which /Items does not have: %v", q)
		}
		if q.Get("SortBy") != "DateCreated" || q.Get("SortOrder") != "Descending" {
			t.Errorf("SortBy=%q SortOrder=%q, want DateCreated Descending", q.Get("SortBy"), q.Get("SortOrder"))
		}
	}
	// Item 210 is the first outside the window; it is on the second page.
	if want := []string{"0", "200"}; !slices.Equal(starts, want) {
		t.Errorf("requested pages at StartIndex %v, want %v", starts, want)
	}
}

func TestAnalytics_RecentlyAdded_UndatedItemDoesNotStop(t *testing.T) {
	lib := recentLibrary(3, 6)
	delete(lib.items[1], "DateCreated")
	result := callTool(t, lib.client(), "", "jellyfin_analytics", map[string]any{"action": "recently_added"})
	out := structured[jf.AnalyticsOutput](t, result)
	if out.TotalCount == nil || *out.TotalCount != 2 || out.Shown == nil || *out.Shown != 2 {
		t.Errorf("want total_count 2 and shown 2, got %+v", out)
	}
	if got, want := analyticsItemIDs(out), []string{"item-0", "item-2"}; !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v: the undated item is left out and the window still ends at item-3", got, want)
	}
	if out.UndatedCount != 1 {
		t.Errorf("undated_count = %d, want 1", out.UndatedCount)
	}
}

func TestAnalytics_RecentlyAdded_HintAtTheLimitCap(t *testing.T) {
	lib := recentLibrary(600, 700)
	result := callTool(t, lib.client(), "", "jellyfin_analytics", map[string]any{"action": "recently_added"})
	out := structured[jf.AnalyticsOutput](t, result)
	if out.TotalCount == nil || *out.TotalCount != 600 || out.Shown == nil || *out.Shown != 500 || out.Items == nil || len(*out.Items) != 500 {
		t.Errorf("want total_count 600 and shown 500, got %+v", out)
	}
	if !hasNote(out.Notes, "jellyfin_browse using min_date_created and start_index") {
		t.Errorf("expected a way past the limit cap, got: %v", out.Notes)
	}
	if hasNote(out.Notes, "Increase limit") {
		t.Errorf("suggested raising a limit that is already at its cap: %v", out.Notes)
	}
}

func TestAnalytics_RecentlyAdded_ReportsTheScanBound(t *testing.T) {
	lib := recentLibrary(jf.DefaultMaxItems+100, jf.DefaultMaxItems+100)
	result := callTool(t, lib.client(), "", "jellyfin_analytics", map[string]any{
		"action": "recently_added",
		"days":   365,
	})
	out := structured[jf.AnalyticsOutput](t, result)
	if out.TotalCount == nil || *out.TotalCount != jf.DefaultMaxItems || !out.TotalIsLowerBound {
		t.Errorf("total_count = %v, total_is_lower_bound = %v, want %d and true", out.TotalCount, out.TotalIsLowerBound, jf.DefaultMaxItems)
	}
	if out.Shown == nil || *out.Shown != 500 {
		t.Errorf("shown = %v, want 500", out.Shown)
	}
}

func TestAnalytics_PlayedStatus_QueryValues(t *testing.T) {
	var episodeParent string
	var itemParams []url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{{"Id": "user-a", "Name": "User A"}}, dest)
			case "/Items":
				episodeParent = params.Get("ParentId")
				return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 4}, dest)
			default:
				itemParams = append(itemParams, params)
				return jsonInto(map[string]any{"Id": "series 1", "Name": "Test Series", "Type": "Series"}, dest)
			}
		},
	}

	result := callTool(t, mc, "", "jellyfin_analytics", map[string]any{
		"action":  "played_status",
		"item_id": "series 1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, result))
	}
	if episodeParent != "series 1" {
		t.Errorf("ParentId = %q, want the item ID as given; query values are encoded by the client", episodeParent)
	}
	for _, q := range itemParams {
		if q.Has("enableUserData") {
			t.Errorf("sent enableUserData, which GET /Items/{itemId} does not have: %v", q)
		}
	}
}

// never_played suggests a larger limit only while one is possible.
func TestAnalytics_NeverPlayedMoreResults(t *testing.T) {
	mc := &mockClient{getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
		limit, _ := strconv.Atoi(params.Get("Limit"))
		items := make([]map[string]any, limit)
		for i := range items {
			items[i] = map[string]any{"Id": fmt.Sprintf("item-%d", i), "Name": "Test Movie", "Type": "Movie"}
		}
		return jsonInto(map[string]any{"Items": items, "TotalRecordCount": 2000}, dest)
	}}
	for _, tt := range []struct {
		limit int
		want  string
	}{
		{10, "Increase limit (currently 10, at most 500)"},
		{jf.MaxLimitCap, "The limit is at its maximum of 500, so narrow with type or parent_id"},
	} {
		out := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "never_played", "limit": tt.limit}))
		if !hasNote(out.Notes, tt.want) {
			t.Errorf("limit %d: want a note containing %q, got: %v", tt.limit, tt.want, out.Notes)
		}
	}
}

// A type whose count could not be read is named, so a smaller total is not
// mistaken for a complete one.
func TestAnalytics_LibraryStatsNamesUnreadTypes(t *testing.T) {
	mc := &mockClient{getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
		if params.Get("IncludeItemTypes") == "Series" {
			return errors.New("API error 500: boom")
		}
		return jsonInto(map[string]any{"TotalRecordCount": 3}, dest)
	}}
	out := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "library_stats"}))
	if want := []string{"Series (API error 500: boom)"}; !slices.Equal(out.UnreadTypes, want) {
		t.Errorf("unread_types = %v, want %v", out.UnreadTypes, want)
	}
	if !hasNote(out.Notes, "Counts could not be read for: Series (API error 500: boom)") {
		t.Errorf("expected a note naming the unread type, got: %v", out.Notes)
	}
	if out.Stats == nil || slices.ContainsFunc(*out.Stats, func(c jf.TypeCount) bool { return c.Type == "Series" }) {
		t.Errorf("stats = %+v, want every type but Series", out.Stats)
	}
}

// analyticsItemIDs returns the IDs of an analytics result's items in order.
func analyticsItemIDs(out jf.AnalyticsOutput) []string {
	if out.Items == nil {
		return nil
	}
	ids := make([]string, 0, len(*out.Items))
	for _, it := range *out.Items {
		ids = append(ids, it.ID)
	}
	return ids
}
