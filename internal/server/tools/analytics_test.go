package tools_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

func TestAnalytics_CodecReport_ResolutionClasses(t *testing.T) {
	video := func(w, h int) map[string]any {
		return map[string]any{"Type": "Video", "Codec": "hevc", "Width": w, "Height": h}
	}
	items := []map[string]any{
		// 8K (4320p): 16:9, 4:3, and portrait
		{"Id": "m1", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(7680, 4320)}}}},
		{"Id": "m2", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(5760, 4320)}}}},
		{"Id": "m3", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(4320, 7680)}}}},
		// 4K (2160p): cropped scope, 4:3 UHD, 21:9 WUHD, and portrait
		{"Id": "m4", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(3832, 1596)}}}},
		{"Id": "m5", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(2880, 2160)}}}},
		{"Id": "m6", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(5120, 2160)}}}},
		{"Id": "m7", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(2160, 3840)}}}},
		// 1440p: QHD, 21:9 UWQHD, cropped 2560-wide, and portrait 1440p
		{"Id": "m8", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(2560, 1440)}}}},
		{"Id": "m9", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(3440, 1440)}}}},
		{"Id": "m10", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(2560, 1080)}}}},
		{"Id": "m11", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1440, 2560)}}}},
		// 1080p: cropped scope, dual-axis crop, 4:3 Blu-ray, DCI 2K, portrait, and height-only
		{"Id": "m12", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1918, 802)}}}},
		{"Id": "m13", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1818, 994)}}}},
		{"Id": "m14", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1440, 1080)}}}},
		{"Id": "m15", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(2048, 1080)}}}},
		{"Id": "m16", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1080, 1920)}}}},
		{"Id": "m17", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(0, 1080)}}}},
		// 720p: cropped scope, 2:1 Univisium, 4:3 720p, and portrait
		{"Id": "m18", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1280, 536)}}}},
		{"Id": "m19", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1440, 720)}}}},
		{"Id": "m20", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(960, 720)}}}},
		{"Id": "m21", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(720, 1280)}}}},
		// 576p: PAL DVD, cropped PAL, square-pixel PAL 1024x576, and portrait
		{"Id": "m22", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(720, 576)}}}},
		{"Id": "m23", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(688, 568)}}}},
		{"Id": "m24", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(1024, 576)}}}},
		{"Id": "m25", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(576, 1024)}}}},
		// 480p: qHD 960x540, cropped widescreen DVD, 4:3 480p, and portrait
		{"Id": "m26", "MediaSources": []map[string]any{{"Container": "mp4", "MediaStreams": []map[string]any{video(960, 540)}}}},
		{"Id": "m27", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(720, 400)}}}},
		{"Id": "m28", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(640, 480)}}}},
		{"Id": "m29", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(480, 854)}}}},
		// SD (<480p): 360p, 352p, and portrait 360p
		{"Id": "m30", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(640, 360)}}}},
		{"Id": "m31", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(624, 352)}}}},
		{"Id": "m32", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(360, 640)}}}},
		// Unmeasured stream (0x0) is not bucketed; dual-layer file counts every video stream present.
		{"Id": "m33", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{video(0, 0)}}}},
		{"Id": "m34", "MediaSources": []map[string]any{{"Container": "mkv", "MediaStreams": []map[string]any{
			{"Type": "Video", "Codec": "hevc", "Width": 3840, "Height": 2160, "VideoRange": "HDR", "VideoRangeType": "Unknown"},
			{"Type": "Video", "Codec": "hevc", "Width": 1920, "Height": 1080, "VideoRange": "Unknown", "VideoRangeType": "Unknown"},
		}}}},
	}

	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto(map[string]any{"Items": items, "TotalRecordCount": len(items)}, dest)
	}}

	out := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report"}))
	if out.CodecReport == nil {
		t.Fatal("codec_report is nil")
	}
	want := map[string]int{
		"8K (4320p)": 3,
		"4K (2160p)": 5,
		"1440p":      4,
		"1080p":      7,
		"720p":       4,
		"576p":       4,
		"480p":       4,
		"SD (<480p)": 3,
	}
	if !maps.Equal(out.CodecReport.Resolutions, want) {
		t.Errorf("resolutions = %v, want %v", out.CodecReport.Resolutions, want)
	}
	wantRanges := map[string]int{"HDR": 1, "Unknown": 1}
	if !maps.Equal(out.CodecReport.VideoRanges, wantRanges) {
		t.Errorf("video_ranges = %v, want %v", out.CodecReport.VideoRanges, wantRanges)
	}
}

func TestAnalytics_CodecReport_RejectsContainerTypesWithoutNetwork(t *testing.T) {
	calls := 0
	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, _ any) error {
		calls++
		return nil
	}}

	for _, bad := range []string{"Series", "series", "Season", "MusicAlbum", "MusicArtist", "BoxSet", "Playlist", "Folder", "CollectionFolder", "Movie,Series", " , "} {
		res := callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report", "type": bad})
		if !res.IsError {
			t.Errorf("type=%q: expected an error result", bad)
		}
		if txt := resultText(t, res); !strings.Contains(txt, "Episode") && !strings.Contains(txt, "file-bearing") {
			t.Errorf("type=%q: error %q should point to file-bearing types", bad, txt)
		}
	}
	if calls != 0 {
		t.Errorf("made %d network calls for invalid codec_report types, want 0", calls)
	}

	var sentType string
	mcValid := &mockClient{getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
		sentType = params.Get("IncludeItemTypes")
		return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
	}}
	res := callTool(t, mcValid, "", "jellyfin_analytics", map[string]any{"action": "codec_report", "type": " Movie , Episode , movie "})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, res))
	}
	if sentType != "Movie,Episode" {
		t.Errorf("IncludeItemTypes = %q, want %q", sentType, "Movie,Episode")
	}
}

func TestAnalytics_CodecReport_EmptyMovieDefaultNote(t *testing.T) {
	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
	}}

	scoped := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report", "parent_id": "tv-lib"}))
	if !hasNote(scoped.Notes, "No Movie media files were found in this library") || !hasNote(scoped.Notes, "type=Episode") {
		t.Errorf("scoped empty notes = %v, want TV/music type hint", scoped.Notes)
	}

	unscoped := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report"}))
	if !hasNote(unscoped.Notes, "codec_report defaults to type=Movie") {
		t.Errorf("unscoped empty notes = %v, want default-Movie hint", unscoped.Notes)
	}

	explicit := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report", "type": "Episode", "parent_id": "tv-lib"}))
	if len(explicit.Notes) != 0 {
		t.Errorf("explicit type=Episode empty notes = %v, want none", explicit.Notes)
	}
}
