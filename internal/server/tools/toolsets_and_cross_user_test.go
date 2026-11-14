package tools_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

func TestToolsets_AllEightVerbNounGroupsAndLegacyAliases(t *testing.T) {
	wantCanonical := []string{
		"admin-server",
		"control-playback",
		"find-media",
		"live-tv",
		"manage-media",
		"monitor-media",
		"monitor-server",
		"my-media",
	}
	var gotCanonical []string
	for name := range tools.ToolsetMap {
		gotCanonical = append(gotCanonical, name)
	}
	slices.Sort(gotCanonical)
	if !slices.Equal(gotCanonical, wantCanonical) {
		t.Fatalf("ToolsetMap keys = %v, want %v", gotCanonical, wantCanonical)
	}

	// Verify find-* and monitor-* groups are strictly 100% read-only (ReadOnlyHint=true,
	// DestructiveHint=false, HasWriteAccess=false, and write-only props stripped).
	writeOnlyProps := []string{"confirm", "dry_run", "image_data", "subtitle_data", "password", "triggers", "repos"}
	for _, ts := range []string{
		"find-media",
		"monitor-media",
		"monitor-server",
		"find-media,monitor-media,monitor-server",
		"discovery",
		"media",
		"analytics",
	} {
		access, unknown := tools.ResolveToolAccess(ts, false)
		if len(unknown) != 0 {
			t.Fatalf("%s: unexpected unknown toolsets %v", ts, unknown)
		}
		if tools.HasWriteAccess(access) {
			t.Errorf("%s: HasWriteAccess = true, want false", ts)
		}
		cs := newTestSession(t, &mockClient{}, ts)
		list, err := cs.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatalf("%s ListTools: %v", ts, err)
		}
		if len(list.Tools) == 0 {
			t.Fatalf("%s registered 0 tools", ts)
		}
		for _, tl := range list.Tools {
			if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint {
				t.Errorf("%s -> %s annotations = %+v, want ReadOnlyHint=true DestructiveHint=false", ts, tl.Name, tl.Annotations)
			}
			b, _ := json.Marshal(tl.InputSchema)
			var schema struct {
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(b, &schema); err != nil {
				t.Fatalf("%s -> %s schema: %v", ts, tl.Name, err)
			}
			for _, banned := range writeOnlyProps {
				if _, ok := schema.Properties[banned]; ok {
					t.Errorf("%s -> %s exposes write-only property %q", ts, tl.Name, banned)
				}
			}
		}
	}

	// Verify exact tool counts for each canonical toolset and legacy alias.
	expectedCounts := map[string]int{
		"find-media":       14,
		"monitor-media":    2,
		"monitor-server":   7,
		"my-media":         2,
		"control-playback": 2,
		"manage-media":     5,
		"admin-server":     6,
		"live-tv":          1,
		// Legacy aliases (including mixed case):
		"DISCOVERY":     14,
		"Media":         14,
		"user":          2,
		"playback":      2,
		"content":       5,
		"analytics":     3, // monitor-media (2) + jellyfin_analytics (1)
		"admin":         6,
		"manage-server": 6,
		"livetv":        1,
	}
	for ts, want := range expectedCounts {
		cs := newTestSession(t, &mockClient{}, ts)
		list, err := cs.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatalf("%s ListTools: %v", ts, err)
		}
		if len(list.Tools) != want {
			var names []string
			for _, tl := range list.Tools {
				names = append(names, tl.Name)
			}
			t.Errorf("toolset %q registered %d tools (%v), want %d", ts, len(list.Tools), names, want)
		}
	}

	// Verify read-only write-group behavior: write-capable groups report HasWriteAccess=true,
	// and masking with readOnly=true sets HasWriteAccess=false.
	for _, rwTS := range []string{"my-media", "control-playback", "manage-media", "admin-server", "live-tv"} {
		acc, _ := tools.ResolveToolAccess(rwTS, false)
		if !tools.HasWriteAccess(acc) {
			t.Errorf("%s: HasWriteAccess = false, want true", rwTS)
		}
		roAcc, _ := tools.ResolveToolAccess(rwTS, true)
		if tools.HasWriteAccess(roAcc) {
			t.Errorf("%s (readOnly=true): HasWriteAccess = true, want false", rwTS)
		}
	}

	// Verify NarrowToolAccess monotonicity across every pair of canonical toolsets.
	for _, a := range wantCanonical {
		accA, _ := tools.ResolveToolAccess(a, false)
		for _, b := range wantCanonical {
			accB, _ := tools.ResolveToolAccess(b, false)
			narrowed := tools.NarrowToolAccess(accA, accB)
			for tool, got := range narrowed {
				if got&^accA[tool] != 0 || got&^accB[tool] != 0 {
					t.Errorf("NarrowToolAccess(%s, %s)[%s] = %b widened beyond (%b, %b)", a, b, tool, got, accA[tool], accB[tool])
				}
			}
		}
	}
}

func TestManageMedia_LibraryManageCuratorTier(t *testing.T) {
	var posted []string
	var deleted []string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Library/VirtualFolders":
				return jsonInto([]map[string]any{{"Name": "Movies", "ItemId": "11111111111111111111111111111111", "Locations": []string{"/media/movies"}}}, dest)
			case "/Environment/Drives":
				return jsonInto([]map[string]any{{"Name": "/", "Path": "/"}}, dest)
			}
			return nil
		},
		postNoContentFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			posted = append(posted, endpoint)
			return nil
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error {
			deleted = append(deleted, endpoint)
			return nil
		},
	}

	// 1. manage-media alone: exposes list_folders, scan, refresh_item ONLY.
	csCurate := newTestSession(t, mc, "manage-media")
	list, err := csCurate.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var libTool *mcp.Tool
	for _, tl := range list.Tools {
		if tl.Name == "jellyfin_library_manage" {
			libTool = tl
			break
		}
	}
	if libTool == nil {
		t.Fatal("expected jellyfin_library_manage in manage-media")
	}
	b, _ := json.Marshal(libTool.InputSchema)
	var curateSchema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(b, &curateSchema)
	wantEnum := []string{"list_folders", "scan", "refresh_item"}
	if !slices.Equal(curateSchema.Properties["action"].Enum, wantEnum) {
		t.Errorf("manage-media action enum = %v, want %v", curateSchema.Properties["action"].Enum, wantEnum)
	}
	for _, curatorProp := range []string{"action", "item_id", "path", "replace_all_metadata", "replace_all_images", "confirm"} {
		if _, ok := curateSchema.Properties[curatorProp]; !ok {
			t.Errorf("manage-media missing curator property %q", curatorProp)
		}
	}
	for _, adminProp := range []string{"folder_name", "collection_type", "new_name", "library_options"} {
		if _, ok := curateSchema.Properties[adminProp]; ok {
			t.Errorf("manage-media leaked admin property %q", adminProp)
		}
	}

	// Allowed curator calls succeed.
	for _, args := range []map[string]any{
		{"action": "list_folders"},
		{"action": "scan", "item_id": "Movies"},
		{"action": "refresh_item", "item_id": "10000000000000000000000000000001"},
	} {
		res, callErr := csCurate.CallTool(t.Context(), &mcp.CallToolParams{Name: "jellyfin_library_manage", Arguments: args})
		if callErr != nil || res.IsError {
			t.Errorf("manage-media %v failed: err=%v res=%v", args, callErr, res)
		}
	}

	// Admin-only and filesystem-browsing actions are rejected under manage-media.
	for _, badAction := range []string{"delete_item", "remove_folder", "add_folder", "browse_drives", "browse_directory"} {
		res, callErr := csCurate.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "jellyfin_library_manage",
			Arguments: map[string]any{"action": badAction, "item_id": "10000000000000000000000000000001"},
		})
		if callErr == nil && (res == nil || !res.IsError) {
			t.Errorf("manage-media allowed %q!", badAction)
		}
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deletes, got %v", deleted)
	}

	// 2. monitor-server,manage-media union: adds browse_drives and browse_directory while still blocking delete_item/remove_folder.
	csUnion := newTestSession(t, mc, "monitor-server,manage-media")
	drivesRes, err := csUnion.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "jellyfin_library_manage",
		Arguments: map[string]any{"action": "browse_drives"},
	})
	if err != nil || drivesRes.IsError {
		t.Errorf("union browse_drives failed: err=%v text=%s", err, resultText(t, drivesRes))
	}
	scanRes, err := csUnion.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "jellyfin_library_manage",
		Arguments: map[string]any{"action": "scan"},
	})
	if err != nil || scanRes.IsError {
		t.Errorf("union scan failed: err=%v text=%s", err, resultText(t, scanRes))
	}
	delRes, err := csUnion.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "jellyfin_library_manage",
		Arguments: map[string]any{"action": "delete_item", "item_id": "10000000000000000000000000000001"},
	})
	if err == nil && (delRes == nil || !delRes.IsError) {
		t.Errorf("union allowed delete_item!")
	}

	// 3. manage-media + --read-only omits jellyfin_library_manage rather than widening to browse_drives.
	csManageRO := newReadOnlySession(t, mc, "manage-media")
	roList, err := csManageRO.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range roList.Tools {
		if tl.Name == "jellyfin_library_manage" || tl.Name == "jellyfin_collections" {
			t.Errorf("manage-media + --read-only unexpectedly exposed %s", tl.Name)
		}
	}
}

func TestCrossUserInspectionAndTelemetry(t *testing.T) {
	const (
		userAID    = "88112233445566778899aabbccddeeff"
		userBID    = "77112233445566778899aabbccddeeff"
		playlistID = "40000000000000000000000000000099"
		seriesID   = "20000000000000000000000000000099"
	)

	var (
		latestEndpoint string
		seenUserIDs    []string
	)
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if uid := params.Get("UserId"); uid != "" {
				seenUserIDs = append(seenUserIDs, endpoint+":"+uid)
			}
			switch {
			case endpoint == "/Users":
				return jsonInto([]map[string]any{
					{"Id": userAID, "Name": "UserA"},
					{"Id": userBID, "Name": "UserB"},
				}, dest)
			case strings.HasSuffix(endpoint, "/Items/Latest"):
				latestEndpoint = endpoint
				return jsonInto([]map[string]any{{"Id": "m-1", "Name": "Test Movie A", "Type": "Movie"}}, dest)
			case endpoint == "/Shows/NextUp":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "ep-1", "Name": "Pilot", "Type": "Episode"}}}, dest)
			case endpoint == "/Sessions":
				return jsonInto([]map[string]any{
					{
						"Id":                 "sess-user-a",
						"UserId":             userAID,
						"UserName":           "UserA",
						"Client":             "Media App",
						"ApplicationVersion": "1.3.0",
						"DeviceName":         "Living Room TV",
						"RemoteEndPoint":     "192.0.2.42",
					},
					{
						"Id":                 "sess-user-b",
						"UserId":             userBID,
						"UserName":           "UserB",
						"Client":             "Jellyfin Web",
						"ApplicationVersion": "10.11.6",
						"DeviceName":         "Web Browser",
						"RemoteEndPoint":     "192.0.2.99",
					},
				}, dest)
			case endpoint == "/UserItems/Resume":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "m-res", "Name": "Test Movie B", "Type": "Movie"}}}, dest)
			case endpoint == "/UserItems/m-1/UserData":
				return jsonInto(map[string]any{"Played": params.Get("UserId") == userAID, "IsFavorite": true, "PlayCount": 4}, dest)
			case endpoint == "/Playlists/"+playlistID+"/Items":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "tr-1", "Name": "Test Track A", "Type": "Audio"}}, "TotalRecordCount": 1}, dest)
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "Playlist":
				if params.Get("UserId") == userAID {
					return jsonInto(map[string]any{"Items": []map[string]any{{"Id": playlistID, "Name": "UserA Mix", "Type": "Playlist"}}, "TotalRecordCount": 1}, dest)
				}
				return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "Season":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "sea-1", "Name": "Season 1", "IndexNumber": 1, "ChildCount": 8}}, "TotalRecordCount": 1}, dest)
			case endpoint == "/Items" && params.Get("ParentId") == "sea-1":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "ep-1", "Name": "Episode 1", "IndexNumber": 1, "ParentIndexNumber": 1, "UserData": map[string]any{"Played": params.Get("UserId") == userAID}}}, "TotalRecordCount": 1}, dest)
			case endpoint == "/Items":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "m-unplayed", "Name": "Test Movie C", "Type": "Movie"}}, "TotalRecordCount": 1}, dest)
			case endpoint == "/System/ActivityLog/Entries":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{
							"Id":            10,
							"Name":          "UserA has authenticated",
							"Type":          "AuthenticationSucceeded",
							"Date":          "2026-04-10T18:00:00Z",
							"Severity":      "Information",
							"ShortOverview": "Remote IP: 192.0.2.42",
							"UserId":        userAID,
						},
						{
							"Id":            11,
							"Name":          "Duplicate text event",
							"Type":          "SessionStarted",
							"Overview":      "Same text",
							"ShortOverview": "Same text",
						},
					},
					"TotalRecordCount": 2,
				}, dest)
			}
			return nil
		},
	}

	// Run cross-user inspection under find-media,monitor-media,monitor-server (100% read-only!).
	cs := newTestSession(t, mc, "find-media,monitor-media,monitor-server")

	call := func(tool string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatalf("CallTool(%s, %v): %v", tool, args, err)
		}
		return res
	}

	// 1. jellyfin_recommendations (latest + next_up) for UserA
	recLatest := structured[jf.RecommendationsOutput](t, call("jellyfin_recommendations", map[string]any{"action": "latest", "user_id": "UserA"}))
	if latestEndpoint != "/Items/Latest" || !slices.Contains(seenUserIDs, "/Items/Latest:"+userAID) || recLatest.Items == nil || len(*recLatest.Items) != 1 {
		t.Errorf("latest for UserA: endpoint=%q seen=%v items=%+v", latestEndpoint, seenUserIDs, recLatest.Items)
	}
	_ = structured[jf.RecommendationsOutput](t, call("jellyfin_recommendations", map[string]any{"action": "next_up", "user_id": "UserA"}))
	if !slices.Contains(seenUserIDs, "/Shows/NextUp:"+userAID) {
		t.Errorf("expected /Shows/NextUp with UserA's ID, got %v", seenUserIDs)
	}

	// 2. jellyfin_tv_shows (seasons + episodes) for UserA
	tvSeasons := structured[jf.TVShowsOutput](t, call("jellyfin_tv_shows", map[string]any{"action": "seasons", "series_id": seriesID, "user_id": "UserA"}))
	if tvSeasons.Seasons == nil || len(*tvSeasons.Seasons) != 1 || (*tvSeasons.Seasons)[0].EpisodeCount != 8 {
		t.Errorf("tv_shows seasons for UserA = %+v", tvSeasons.Seasons)
	}
	tvEps := structured[jf.TVShowsOutput](t, call("jellyfin_tv_shows", map[string]any{"action": "episodes", "series_id": seriesID, "season_number": 1, "user_id": userAID}))
	if tvEps.Episodes == nil || len(*tvEps.Episodes) != 1 || !(*tvEps.Episodes)[0].Played {
		t.Errorf("tv_shows episodes for UserA = %+v, want played=true", tvEps.Episodes)
	}

	// 3. jellyfin_sessions (list filtered by user_id + remote_ip & app_version + resume for UserA)
	sessList := structured[jf.SessionsOutput](t, call("jellyfin_sessions", map[string]any{"action": "list", "user_id": "UserA"}))
	if sessList.Sessions == nil || len(*sessList.Sessions) != 1 {
		t.Fatalf("filtered sessions = %+v, want 1 session for UserA", sessList.Sessions)
	}
	s := (*sessList.Sessions)[0]
	if s.User != "UserA" || s.AppVersion != "1.3.0" || s.RemoteIP != "192.0.2.42" {
		t.Errorf("session telemetry = %+v, want User=UserA AppVersion=1.3.0 RemoteIP=192.0.2.42", s)
	}
	sessResume := structured[jf.SessionsOutput](t, call("jellyfin_sessions", map[string]any{"action": "resume", "user_id": "UserA"}))
	if sessResume.Resume == nil || len(*sessResume.Resume) != 1 || !slices.Contains(seenUserIDs, "/UserItems/Resume:"+userAID) {
		t.Errorf("resume for UserA = %+v, seen=%v", sessResume.Resume, seenUserIDs)
	}

	// 4. jellyfin_user_data get_user_data in find-media RO projection with user_id
	udText := resultText(t, call("jellyfin_user_data", map[string]any{"action": "get_user_data", "item_id": "m-1", "user_id": "UserA"}))
	if !strings.Contains(udText, `"played":true`) || !slices.Contains(seenUserIDs, "/UserItems/m-1/UserData:"+userAID) {
		t.Errorf("get_user_data for UserA = %s (seen=%v)", udText, seenUserIDs)
	}

	// 5. jellyfin_playlists list & get by playlist name in find-media RO projection with user_id
	plListText := resultText(t, call("jellyfin_playlists", map[string]any{"action": "list", "user_id": "UserA"}))
	if !strings.Contains(plListText, "UserA Mix") {
		t.Errorf("playlists list for UserA = %s", plListText)
	}
	plGetText := resultText(t, call("jellyfin_playlists", map[string]any{"action": "get", "playlist_id": "UserA Mix", "user_id": "UserA"}))
	if !strings.Contains(plGetText, "Test Track A") || !slices.Contains(seenUserIDs, "/Playlists/"+playlistID+"/Items:"+userAID) {
		t.Errorf("playlists get for UserA = %s (seen=%v)", plGetText, seenUserIDs)
	}

	// 6. jellyfin_analytics never_played with user_id
	neverOut := structured[jf.AnalyticsOutput](t, call("jellyfin_analytics", map[string]any{"action": "never_played", "user_id": "UserA"}))
	if neverOut.Items == nil || len(*neverOut.Items) != 1 {
		t.Errorf("analytics never_played for UserA = %+v", neverOut.Items)
	}

	// 7. jellyfin_system_info activity_log extracts short_overview when distinct from overview
	actText := resultText(t, call("jellyfin_system_info", map[string]any{"action": "activity_log"}))
	entries := systemInfoEntries(t, actText)
	if len(entries) != 2 {
		t.Fatalf("activity_log entries = %d, want 2", len(entries))
	}
	if entries[0]["short_overview"] != "Remote IP: 192.0.2.42" {
		t.Errorf("entries[0] short_overview = %v, want Remote IP: 192.0.2.42", entries[0]["short_overview"])
	}
	if _, dup := entries[1]["short_overview"]; dup {
		t.Errorf("entries[1] should omit duplicate short_overview, got %v", entries[1])
	}

	// 8. Unknown user_id returns clean ResolveErrResult across all cross-user tools.
	for _, badCall := range []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_recommendations", map[string]any{"action": "next_up", "user_id": "GhostUser"}},
		{"jellyfin_tv_shows", map[string]any{"action": "next_up", "user_id": "GhostUser"}},
		{"jellyfin_sessions", map[string]any{"action": "list", "user_id": "GhostUser"}},
		{"jellyfin_sessions", map[string]any{"action": "resume", "user_id": "GhostUser"}},
		{"jellyfin_user_data", map[string]any{"action": "get_user_data", "item_id": "m-1", "user_id": "GhostUser"}},
		{"jellyfin_playlists", map[string]any{"action": "list", "user_id": "GhostUser"}},
		{"jellyfin_analytics", map[string]any{"action": "never_played", "user_id": "GhostUser"}},
	} {
		argsCopy := maps.Clone(badCall.args)
		res := call(badCall.tool, argsCopy)
		if !res.IsError || !strings.HasPrefix(resultText(t, res), "No user has ID 'GhostUser'") {
			t.Errorf("%s(%v): got isError=%v %q, want prefix No user has ID 'GhostUser'", badCall.tool, argsCopy, res.IsError, resultText(t, res))
		}
	}
}

func TestCrossUserUnsupportedActionsRejectUserIDWithoutMutating(t *testing.T) {
	httpCalls := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, _ any) error {
			httpCalls++
			return nil
		},
		postFunc: func(_ context.Context, _ string, _ url.Values, _ any, _ any) error {
			httpCalls++
			return nil
		},
		postNoContentFunc: func(_ context.Context, _ string, _ url.Values, _ any) error {
			httpCalls++
			return nil
		},
		delFunc: func(_ context.Context, _ string, _ url.Values) error {
			httpCalls++
			return nil
		},
	}

	cs := newTestSession(t, mc, "")
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_user_data", map[string]any{"action": "mark_played", "item_id": "m-1", "user_id": "UserA"}},
		{"jellyfin_user_data", map[string]any{"action": "favorite", "item_id": "m-1", "user_id": "UserA"}},
		{"jellyfin_user_data", map[string]any{"action": "rate", "item_id": "m-1", "rating": 8.5, "user_id": "UserA"}},
		{"jellyfin_user_data", map[string]any{"action": "set_user_data", "item_id": "m-1", "played": true, "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "create", "name": "Synth", "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "delete", "playlist_id": "pl-1", "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "add_items", "playlist_id": "pl-1", "item_ids": []string{"m-1"}, "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "remove_items", "playlist_id": "pl-1", "item_ids": []string{"m-1"}, "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "move_item", "playlist_id": "pl-1", "item_id": "m-1", "new_index": 0, "user_id": "UserA"}},
		{"jellyfin_playlists", map[string]any{"action": "deduplicate", "playlist_id": "pl-1", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "library_stats", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "codec_report", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "recently_added", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "duplicate_check", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "library_size", "user_id": "UserA"}},
		{"jellyfin_analytics", map[string]any{"action": "size_report", "user_id": "UserA"}},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err != nil {
			t.Fatalf("%s(%v): %v", tc.tool, tc.args, err)
		}
		if !res.IsError || !strings.Contains(resultText(t, res), "user_id is only supported") {
			t.Errorf("%s(%v): got isError=%v %q, want user_id rejection", tc.tool, tc.args, res.IsError, resultText(t, res))
		}
	}
	if httpCalls != 0 {
		t.Errorf("made %d HTTP calls on unsupported user_id actions, want 0", httpCalls)
	}
}
