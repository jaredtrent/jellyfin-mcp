package tools_test

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func TestPlaybackStats_InvalidActionAndTypeValidation(t *testing.T) {
	calls := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, _ any) error {
			calls++
			return nil
		},
	}

	badAct := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{"action": "bogus"})
	if !badAct.IsError {
		t.Fatalf("expected error for invalid action, got %s", resultText(t, badAct))
	}

	missingItem := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{"action": "played_status"})
	if !missingItem.IsError || !strings.Contains(resultText(t, missingItem), "item_id is required") {
		t.Errorf("expected item_id required error, got: %s", resultText(t, missingItem))
	}

	for _, action := range []string{"played_status", "most_played", "unwatched_by_all"} {
		for _, badType := range []string{"BoxSet", "Folder", "MusicAlbum", "Movie,Series"} {
			args := map[string]any{"action": action, "type": badType}
			if action == "played_status" {
				args["item_id"] = "Sample Film"
			}
			res := callTool(t, mc, "", "jellyfin_playback_stats", args)
			if !res.IsError || !strings.Contains(resultText(t, res), "must be one of") {
				t.Errorf("%s type=%q: expected validation error, got isError=%v: %s", action, badType, res.IsError, resultText(t, res))
			}
		}
	}

	// Household-wide actions reject user_id with zero network calls rather than silently ignoring it.
	for _, action := range []string{"played_status", "most_played", "unwatched_by_all"} {
		args := map[string]any{"action": action, "user_id": "UserA"}
		if action == "played_status" {
			args["item_id"] = "Sample Film"
		}
		res := callTool(t, mc, "", "jellyfin_playback_stats", args)
		if !res.IsError || !strings.Contains(resultText(t, res), "user_id is not supported") {
			t.Errorf("%s user_id=UserA: expected rejection error, got isError=%v: %s", action, res.IsError, resultText(t, res))
		}
	}
	// Unsupported per-action parameters fail closed with zero network calls.
	for _, action := range []string{"playback_history", "most_played", "unwatched_by_all"} {
		res := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{"action": action, "item_id": "Sample Film"})
		if !res.IsError || !strings.Contains(resultText(t, res), "item_id is not supported") {
			t.Errorf("%s item_id: expected rejection error, got isError=%v: %s", action, res.IsError, resultText(t, res))
		}
	}
	for _, action := range []string{"played_status", "most_played", "unwatched_by_all"} {
		args := map[string]any{"action": action, "verified_only": false}
		if action == "played_status" {
			args["item_id"] = "Sample Film"
		}
		res := callTool(t, mc, "", "jellyfin_playback_stats", args)
		if !res.IsError || !strings.Contains(resultText(t, res), "verified_only is only supported") {
			t.Errorf("%s verified_only: expected rejection error, got isError=%v: %s", action, res.IsError, resultText(t, res))
		}
	}
	seriesHist := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{"action": "playback_history", "type": "Series"})
	if !seriesHist.IsError || !strings.Contains(resultText(t, seriesHist), "type=Episode") {
		t.Errorf("playback_history type=Series: expected Episode hint, got: %s", resultText(t, seriesHist))
	}
	if calls != 0 {
		t.Errorf("made %d network calls on invalid inputs, want 0", calls)
	}
}

func TestPlaybackStats_PlaybackHistory_FilteringAndTruncationNotes(t *testing.T) {
	const userAID = "88112233445566778899aabbccddeeff"
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": userAID, "Name": "UserA"},
				}, dest)
			case "/Items":
				if params.Get("UserId") != userAID {
					t.Errorf("UserId = %q, want %q", params.Get("UserId"), userAID)
				}
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{
							"Id": "m-verified", "Name": "Test Movie A", "Type": "Movie",
							"UserData": map[string]any{"Played": true, "PlayCount": 3, "LastPlayedDate": "2026-04-01T12:00:00Z"},
						},
						{
							"Id": "m-manual", "Name": "Sample Film", "Type": "Movie",
							"UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2026-03-20T12:00:00Z"},
						},
					},
					"TotalRecordCount": 10,
				}, dest)
			case "/System/ActivityLog/Entries":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Type": "VideoPlaybackStopped", "UserId": userAID, "ItemId": "m-verified"},
					},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
	}

	// Default verified_only=true keeps only verified playback and explains partial window check.
	verOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "monitor-media", "jellyfin_playback_stats", map[string]any{
		"action":  "playback_history",
		"user_id": "UserA",
		"limit":   2,
	}))
	if verOut.History == nil || len(*verOut.History) != 1 {
		t.Fatalf("verified history = %+v, want 1 entry", verOut.History)
	}
	if (*verOut.History)[0].ID != "m-verified" || (*verOut.History)[0].PlayCount != 3 || (*verOut.History)[0].ActualPlayback == nil || !*(*verOut.History)[0].ActualPlayback {
		t.Errorf("verified entry = %+v", (*verOut.History)[0])
	}
	if verOut.TotalCount == nil || *verOut.TotalCount != 10 || verOut.Shown == nil || *verOut.Shown != 1 {
		t.Errorf("counts: total=%v shown=%v, want 10 and 1", verOut.TotalCount, verOut.Shown)
	}
	if !hasNote(verOut.Notes, "Checked the 2 most recently played items out of 10 marked played") {
		t.Errorf("expected truncation check note, got: %v", verOut.Notes)
	}

	// verified_only=false includes both verified and manually marked items.
	allOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "monitor-media", "jellyfin_playback_stats", map[string]any{
		"action":        "playback_history",
		"user_id":       "UserA",
		"verified_only": false,
		"limit":         2,
	}))
	if allOut.History == nil || len(*allOut.History) != 2 {
		t.Fatalf("unverified history = %+v, want 2 entries", allOut.History)
	}
	if (*allOut.History)[1].ID != "m-manual" || (*allOut.History)[1].ActualPlayback == nil || *(*allOut.History)[1].ActualPlayback {
		t.Errorf("manual entry = %+v, want actual_playback=false", (*allOut.History)[1])
	}
	if !hasNote(allOut.Notes, "Showing 2 of 10") {
		t.Errorf("expected Showing 2 of 10 note, got: %v", allOut.Notes)
	}

	// Single-page verified_only=true (total == len(rawItems)) also explains excluded unverified items.
	singlePageMC := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{{"Id": userAID, "Name": "UserA"}}, dest)
			case "/Items":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Id": "m-verified", "Name": "Test Movie A", "Type": "Movie", "UserData": map[string]any{"Played": true}},
						{"Id": "m-manual", "Name": "Sample Film", "Type": "Movie", "UserData": map[string]any{"Played": true}},
					},
					"TotalRecordCount": 2,
				}, dest)
			case "/System/ActivityLog/Entries":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Type": "VideoPlaybackStopped", "UserId": userAID, "ItemId": "m-verified"}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
	}
	singleOut := structured[jf.PlaybackStatsOutput](t, callTool(t, singlePageMC, "monitor-media", "jellyfin_playback_stats", map[string]any{
		"action":  "playback_history",
		"user_id": "UserA",
	}))
	if singleOut.TotalCount == nil || *singleOut.TotalCount != 2 || singleOut.Shown == nil || *singleOut.Shown != 1 {
		t.Errorf("single-page counts: total=%v shown=%v, want 2 and 1", singleOut.TotalCount, singleOut.Shown)
	}
	if !hasNote(singleOut.Notes, "Excluded 1 marked-played item with no activity-log playback event") {
		t.Errorf("expected single-page exclusion note, got: %v", singleOut.Notes)
	}
}

func TestPlaybackStats_PlayedStatus_TitleResolutionAnd403Fallback(t *testing.T) {
	const (
		film1984ID = "10000000000000000000000000001984"
		film2021ID = "10000000000000000000000000002021"
	)

	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "u-active", "Name": "UserA"},
					{"Id": "u-disabled", "Name": "OldAccount", "Policy": map[string]any{"IsDisabled": true}},
				}, dest)
			case "/Items/" + film2021ID:
				return jsonInto(map[string]any{
					"Id": film2021ID, "Name": "Sample Film", "Type": "Movie", "ProductionYear": 2021,
					"UserData": map[string]any{"Played": params.Get("UserId") == "u-active", "PlayCount": 2, "LastPlayedDate": "2026-02-15T18:00:00Z"},
				}, dest)
			case "/Items":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Id": "folder-sample", "Name": "Sample Film (2021)", "Type": "Folder"},
						{"Id": film1984ID, "Name": "Sample Film", "Type": "Movie", "ProductionYear": 1984},
						{"Id": film2021ID, "Name": "Sample Film", "Type": "Movie", "ProductionYear": 2021},
					},
					"TotalRecordCount": 3,
				}, dest)
			default:
				return &jf.APIError{StatusCode: 400, Body: "invalid GUID"}
			}
		},
	}

	// Ambiguous title without year lists disambiguating years and IDs.
	ambig := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": "Sample Film",
	})
	if !ambig.IsError || !strings.Contains(resultText(t, ambig), "1984") || !strings.Contains(resultText(t, ambig), "2021") {
		t.Errorf("expected year disambiguation error for 'Sample Film', got: %s", resultText(t, ambig))
	}

	// Title with year resolves cleanly, ignores Folder item, and skips disabled user.
	out := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": "Sample Film (2021)",
	}))
	if out.ItemSummary == nil || out.ItemSummary.ID != film2021ID || out.ItemSummary.Name != "Sample Film" {
		t.Fatalf("item_summary = %+v, want Sample Film (%s)", out.ItemSummary, film2021ID)
	}
	if out.Users == nil || len(*out.Users) != 1 || (*out.Users)[0].Name != "UserA" || !(*out.Users)[0].Played || (*out.Users)[0].PlayCount != 2 {
		t.Errorf("users = %+v, want only enabled user UserA played=true play_count=2", out.Users)
	}

	// Non-admin 403 on /Users falls back to current user (resolving their Name via /Users/{id}) with an explanatory note.
	mc403 := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return &jf.APIError{StatusCode: 403, Body: "Forbidden"}
			case "/Users/test-user-id":
				return jsonInto(map[string]any{"Id": "test-user-id", "Name": "UserA"}, dest)
			case "/Items/" + film2021ID:
				return jsonInto(map[string]any{
					"Id": film2021ID, "Name": "Sample Film", "Type": "Movie",
					"UserData": map[string]any{"Played": true, "PlayCount": 1},
				}, dest)
			}
			return nil
		},
	}
	fbOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc403, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": film2021ID,
	}))
	if fbOut.Users == nil || len(*fbOut.Users) != 1 || !(*fbOut.Users)[0].Played || (*fbOut.Users)[0].Name != "UserA" {
		t.Errorf("403 fallback users = %+v, want UserA played=true", fbOut.Users)
	}
	if !hasNote(fbOut.Notes, "403 Forbidden") {
		t.Errorf("expected 403 fallback note, got: %v", fbOut.Notes)
	}
}

func TestPlaybackStats_MostPlayed_MoviesAndSeriesHouseholdAggregation(t *testing.T) {
	const moviesLibID = "11111111111111111111111111111111"
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Library/VirtualFolders":
				return jsonInto([]map[string]any{
					{"Name": "Movies", "ItemId": moviesLibID, "CollectionType": "movies"},
				}, dest)
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "u-user-a", "Name": "UserA"},
					{"Id": "u-user-b", "Name": "UserB"},
					{"Id": "u-disabled", "Name": "DisabledUser", "Policy": map[string]any{"IsDisabled": true}},
				}, dest)
			case "/Items":
				uid := params.Get("UserId")
				typ := params.Get("IncludeItemTypes")
				if uid == "u-disabled" {
					t.Errorf("unexpected /Items query for disabled user")
				}
				switch {
				case typ == "Movie" && uid == "u-user-a":
					if params.Get("ParentId") != moviesLibID {
						t.Errorf("ParentId = %q, want %q", params.Get("ParentId"), moviesLibID)
					}
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "m-movie-a", "Name": "Test Movie A", "Type": "Movie", "ProductionYear": 2016, "UserData": map[string]any{"Played": true, "PlayCount": 2, "LastPlayedDate": "2026-03-01T10:00:00Z"}},
							{"Id": "m-movie-b", "Name": "Test Movie B", "Type": "Movie", "ProductionYear": 2017, "UserData": map[string]any{"Played": true, "PlayCount": 5, "LastPlayedDate": "2026-04-01T10:00:00Z"}},
						},
						"TotalRecordCount": 2,
					}, dest)
				case typ == "Movie" && uid == "u-user-b":
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "m-movie-a", "Name": "Test Movie A", "Type": "Movie", "ProductionYear": 2016, "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2026-04-10T10:00:00Z"}},
						},
						"TotalRecordCount": 1,
					}, dest)
				case typ == "Episode" && uid == "u-user-a":
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "ep-1", "SeriesId": "s-alpha", "SeriesName": "Series Alpha", "Type": "Episode", "UserData": map[string]any{"Played": true, "PlayCount": 2, "LastPlayedDate": "2026-04-02T10:00:00Z"}},
							{"Id": "ep-2", "SeriesId": "s-alpha", "SeriesName": "Series Alpha", "Type": "Episode", "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2026-04-03T10:00:00Z"}},
							{"Id": "ep-3", "SeriesId": "s-beta", "SeriesName": "Series Beta", "Type": "Episode", "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2026-04-05T10:00:00Z"}},
						},
						"TotalRecordCount": 3,
					}, dest)
				case typ == "Episode" && uid == "u-user-b":
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "ep-3", "SeriesId": "s-beta", "SeriesName": "Series Beta", "Type": "Episode", "UserData": map[string]any{"Played": true, "PlayCount": 1, "LastPlayedDate": "2026-04-06T10:00:00Z"}},
						},
						"TotalRecordCount": 1,
					}, dest)
				}
			}
			return nil
		},
	}

	// Movies: Test Movie A watched by 2 users (total 3 plays) ranks ahead of Test Movie B watched by 1 user (5 plays).
	movieOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":    "most_played",
		"parent_id": "Movies",
		"limit":     1,
	}))
	if movieOut.TotalCount == nil || *movieOut.TotalCount != 2 || movieOut.Shown == nil || *movieOut.Shown != 1 {
		t.Fatalf("movie most_played counts: total=%v shown=%v, want 2 and 1", movieOut.TotalCount, movieOut.Shown)
	}
	topMovie := (*movieOut.MostPlayed)[0]
	if topMovie.ID != "m-movie-a" || topMovie.UsersPlayed != 2 || topMovie.TotalPlays != 3 || topMovie.LastPlayed != "2026-04-10" {
		t.Errorf("topMovie = %+v, want Test Movie A users_played=2 total_plays=3 last_played=2026-04-10", topMovie)
	}
	if !hasNote(movieOut.Notes, "Showing 1 of 2") {
		t.Errorf("expected truncation note, got: %v", movieOut.Notes)
	}

	// Series: Series Beta watched by 2 users (2 plays) ranks ahead of Series Alpha watched by 1 user across 2 episodes (3 plays).
	seriesOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action": "most_played",
		"type":   "Series",
	}))
	if seriesOut.MostPlayed == nil || len(*seriesOut.MostPlayed) != 2 {
		t.Fatalf("series most_played = %+v, want 2 series", seriesOut.MostPlayed)
	}
	first, second := (*seriesOut.MostPlayed)[0], (*seriesOut.MostPlayed)[1]
	if first.ID != "s-beta" || first.UsersPlayed != 2 || first.TotalPlays != 2 {
		t.Errorf("first series = %+v, want Series Beta users_played=2 total_plays=2", first)
	}
	if second.ID != "s-alpha" || second.UsersPlayed != 1 || second.TotalPlays != 3 {
		t.Errorf("second series = %+v, want Series Alpha users_played=1 total_plays=3", second)
	}
	if !hasNote(seriesOut.Notes, "For Series, users_played counts enabled users") {
		t.Errorf("expected Series aggregation note, got: %v", seriesOut.Notes)
	}
}

func TestPlaybackStats_UnwatchedByAll_MoviesAndSeries(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "u-user-a", "Name": "UserA"},
					{"Id": "u-user-b", "Name": "UserB"},
					{"Id": "u-disabled", "Name": "DisabledUser", "Policy": map[string]any{"IsDisabled": true}},
				}, dest)
			case "/Items":
				uid := params.Get("UserId")
				typ := params.Get("IncludeItemTypes")
				isPlayed := params.Get("IsPlayed") == "true"

				switch {
				case isPlayed && typ == "Movie" && uid == "u-user-a":
					return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "m-watched-a"}}, "TotalRecordCount": 1}, dest)
				case isPlayed && typ == "Movie" && uid == "u-user-b":
					return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "m-watched-b"}}, "TotalRecordCount": 1}, dest)
				case !isPlayed && typ == "Movie":
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "m-watched-a", "Name": "Watched By UserA", "Type": "Movie"},
							{"Id": "m-unwatched-1", "Name": "Unwatched Gem", "Type": "Movie", "DateCreated": "2026-04-01T00:00:00Z"},
							{"Id": "m-watched-b", "Name": "Watched By UserB", "Type": "Movie"},
							{"Id": "m-unwatched-2", "Name": "Only Disabled Watched", "Type": "Movie", "DateCreated": "2026-03-01T00:00:00Z"},
						},
						"TotalRecordCount": 4,
					}, dest)
				case isPlayed && typ == "Episode" && uid == "u-user-b":
					// UserB watched 1 episode of s-partial, so s-partial is NOT unwatched_by_all.
					return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "ep-1", "SeriesId": "s-partial"}}, "TotalRecordCount": 1}, dest)
				case isPlayed && typ == "Series" && uid == "u-user-a":
					// UserA manually marked s-marked played.
					return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "s-marked"}}, "TotalRecordCount": 1}, dest)
				case isPlayed:
					return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
				case !isPlayed && typ == "Series":
					return jsonInto(map[string]any{
						"Items": []map[string]any{
							{"Id": "s-partial", "Name": "Partial Show", "Type": "Series"},
							{"Id": "s-marked", "Name": "Marked Show", "Type": "Series"},
							{"Id": "s-untouched", "Name": "Untouched Show", "Type": "Series"},
						},
						"TotalRecordCount": 3,
					}, dest)
				}
			}
			return nil
		},
	}

	movieOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action": "unwatched_by_all",
		"limit":  1,
	}))
	if movieOut.TotalCount == nil || *movieOut.TotalCount != 2 || movieOut.Shown == nil || *movieOut.Shown != 1 {
		t.Fatalf("movie unwatched_by_all counts: total=%v shown=%v, want 2 and 1", movieOut.TotalCount, movieOut.Shown)
	}
	if (*movieOut.Unwatched)[0].ID != "m-unwatched-1" || (*movieOut.Unwatched)[0].DateAdded != "2026-04-01" {
		t.Errorf("unwatched[0] = %+v, want m-unwatched-1 date_added=2026-04-01", (*movieOut.Unwatched)[0])
	}

	// Full movie list contains both unwatched items and neither watched item.
	movieAll := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action": "unwatched_by_all",
	}))
	var gotMovieIDs []string
	for _, it := range *movieAll.Unwatched {
		gotMovieIDs = append(gotMovieIDs, it.ID)
	}
	if want := []string{"m-unwatched-1", "m-unwatched-2"}; !slices.Equal(gotMovieIDs, want) {
		t.Errorf("unwatched movie IDs = %v, want %v", gotMovieIDs, want)
	}

	// Series excludes both s-partial (episode played by UserB) and s-marked (series marked played by UserA).
	seriesOut := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action": "unwatched_by_all",
		"type":   "Series",
	}))
	if seriesOut.Unwatched == nil || len(*seriesOut.Unwatched) != 1 || (*seriesOut.Unwatched)[0].ID != "s-untouched" {
		t.Errorf("series unwatched = %+v, want only s-untouched", seriesOut.Unwatched)
	}
}

func TestPlaybackStats_PlayedStatus_SeriesEpisodesBoundsAndSameYearTypeFilter(t *testing.T) {
	const (
		movieID  = "10000000000000000000000000002015"
		seriesID = "20000000000000000000000000002015"
	)

	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "u-null", "Name": "NullUnplayed"},
					{"Id": "u-over", "Name": "OverCount"},
					{"Id": "u-done", "Name": "MarkedPlayedNoCount"},
				}, dest)
			case "/Items/" + seriesID:
				var ud map[string]any
				switch params.Get("UserId") {
				case "u-null":
					// Omitted UnplayedItemCount with Played=false must NOT report 10 of 10 played.
					ud = map[string]any{"Played": false}
				case "u-over":
					// Cached UnplayedItemCount > non-missing totalEpisodes must clamp at 0, not negative.
					ud = map[string]any{"Played": false, "UnplayedItemCount": 12}
				case "u-done":
					ud = map[string]any{"Played": true}
				default:
					ud = map[string]any{"Played": false, "UnplayedItemCount": 10}
				}
				return jsonInto(map[string]any{
					"Id": seriesID, "Name": "Shared Title", "Type": "Series", "ProductionYear": 2015,
					"UserData": ud,
				}, dest)
			case "/Items/" + movieID:
				return jsonInto(map[string]any{
					"Id": movieID, "Name": "Shared Title", "Type": "Movie", "ProductionYear": 2015,
				}, dest)
			case "/Items":
				if params.Get("ParentId") == seriesID {
					return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 10}, dest)
				}
				items := []map[string]any{
					{"Id": movieID, "Name": "Shared Title", "Type": "Movie", "ProductionYear": 2015},
					{"Id": seriesID, "Name": "Shared Title", "Type": "Series", "ProductionYear": 2015},
				}
				if want := params.Get("IncludeItemTypes"); want != "" {
					var filtered []map[string]any
					for _, it := range items {
						if strings.EqualFold(jf.GetString(it, "Type"), want) {
							filtered = append(filtered, it)
						}
					}
					items = filtered
				}
				return jsonInto(map[string]any{"Items": items, "TotalRecordCount": len(items)}, dest)
			default:
				return &jf.APIError{StatusCode: 400, Body: "invalid GUID"}
			}
		},
	}

	// Without type, same-year Movie + Series reports both types clearly in disambiguation error.
	ambig := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": "Shared Title (2015)",
	})
	if !ambig.IsError || !strings.Contains(resultText(t, ambig), "Movie, 2015") || !strings.Contains(resultText(t, ambig), "Series, 2015") {
		t.Errorf("expected Movie/Series disambiguation, got: %s", resultText(t, ambig))
	}

	// Passing type=Series resolves cleanly and handles nil/over-counted UnplayedItemCount safely.
	out := structured[jf.PlaybackStatsOutput](t, callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": "Shared Title (2015)",
		"type":    "Series",
	}))
	if out.ItemSummary == nil || out.ItemSummary.ID != seriesID || out.ItemSummary.TotalEpisodes != 10 {
		t.Fatalf("item_summary = %+v", out.ItemSummary)
	}
	users := *out.Users
	if len(users) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(users))
	}
	if users[0].Played || users[0].EpisodesPlayed != nil {
		t.Errorf("NullUnplayed = %+v, want played=false and episodes_played=nil", users[0])
	}
	if users[1].Played || users[1].EpisodesPlayed == nil || *users[1].EpisodesPlayed != 0 {
		t.Errorf("OverCount = %+v, want episodes_played=0 (clamped >= 0)", users[1])
	}
	if !users[2].Played || users[2].EpisodesPlayed == nil || *users[2].EpisodesPlayed != 10 {
		t.Errorf("MarkedPlayedNoCount = %+v, want played=true and episodes_played=10", users[2])
	}

	// Explicit Movie GUID with type=Series returns a clear type-mismatch error.
	mismatch := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "played_status",
		"item_id": movieID,
		"type":    "Series",
	})
	if !mismatch.IsError || !strings.Contains(resultText(t, mismatch), "does not match type=Series") {
		t.Errorf("expected type mismatch error, got: %s", resultText(t, mismatch))
	}
}
