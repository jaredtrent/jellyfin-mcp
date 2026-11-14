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

func newReadOnlySession(t *testing.T, mc *mockClient, toolsets string) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-ro", Version: "0.1"}, nil)
	tools.RegisterTools(srv, mc, tools.BuildToolFilter(toolsets, true))

	ct, st := newTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal("server connect:", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	cs, err := client.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestBrowse_ResolutionTiers(t *testing.T) {
	const videoDefault = "Movie,Episode,MusicVideo,Video"
	tests := []struct {
		tier     string
		want     map[string]string
		notWant  []string
		wantType string
	}{
		{
			tier:     "sd",
			want:     map[string]string{"IsHD": "false", "MinWidth": "1"},
			notWant:  []string{"Is4K", "MaxWidth", "MaxHeight", "MinHeight"},
			wantType: videoDefault,
		},
		{
			tier:     "720p",
			want:     map[string]string{"IsHD": "true", "MinWidth": "1", "MaxWidth": "1799", "MaxHeight": "999", "MinHeight": "1"},
			notWant:  []string{"Is4K"},
			wantType: videoDefault,
		},
		{
			tier:     "1080p",
			want:     map[string]string{"IsHD": "true", "MinWidth": "1400", "MaxWidth": "2499", "MaxHeight": "1399", "MinHeight": "1"},
			notWant:  []string{"Is4K"},
			wantType: videoDefault,
		},
		{
			tier:     "1440p",
			want:     map[string]string{"IsHD": "true", "MinWidth": "2500", "MaxWidth": "3799", "MaxHeight": "2099", "MinHeight": "1"},
			notWant:  []string{"Is4K"},
			wantType: videoDefault,
		},
		{
			tier:     "4k",
			want:     map[string]string{"Is4K": "true", "MinWidth": "1", "MaxWidth": "7599", "MaxHeight": "4199", "MinHeight": "1"},
			notWant:  []string{"IsHD"},
			wantType: videoDefault,
		},
		{
			tier:     "8k",
			want:     map[string]string{"Is4K": "true", "MinWidth": "7600"},
			notWant:  []string{"IsHD", "MaxWidth", "MaxHeight"},
			wantType: videoDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			var gotParams url.Values
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
					if endpoint == "/Items" {
						gotParams = maps.Clone(params)
					}
					return jsonInto(map[string]any{
						"Items":            []map[string]any{{"Id": "m-1", "Name": "Cropped Movie", "Type": "Movie", "Width": 1920, "Height": 800}},
						"TotalRecordCount": 1,
					}, dest)
				},
			}
			res := callTool(t, mc, "", "jellyfin_browse", map[string]any{"resolution": tt.tier})
			out := structured[jf.ItemListOutput](t, res)
			if out.TotalCount != 1 {
				t.Fatalf("total_count = %d, want 1", out.TotalCount)
			}
			if got := gotParams.Get("IncludeItemTypes"); got != tt.wantType {
				t.Errorf("IncludeItemTypes = %q, want %q", got, tt.wantType)
			}
			for k, v := range tt.want {
				if got := gotParams.Get(k); got != v {
					t.Errorf("param %s = %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.notWant {
				if gotParams.Has(k) {
					t.Errorf("unexpected param %s = %q", k, gotParams.Get(k))
				}
			}
		})
	}

	t.Run("conflicts and non-video types", func(t *testing.T) {
		mc := &mockClient{}
		for _, bad := range []map[string]any{
			{"resolution": "1080p", "is_hd": true},
			{"resolution": "4k", "is_4k": true},
			{"resolution": "1080p", "min_width": 1920},
			{"resolution": "1080p", "type": "Audio"},
			{"resolution": "4k", "type": "Series"},
		} {
			res := callTool(t, mc, "", "jellyfin_browse", bad)
			if !res.IsError {
				t.Errorf("expected error for %v, got %s", bad, resultText(t, res))
			}
		}
	})

	t.Run("group_by_series with resolution tier", func(t *testing.T) {
		var scans []url.Values
		mc := &mockClient{
			getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
				scans = append(scans, maps.Clone(params))
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "ep-1", "SeriesId": "s-1", "SeriesName": "Series Beta"}},
					"TotalRecordCount": 1,
				}, dest)
			},
		}
		res := callTool(t, mc, "", "jellyfin_browse", map[string]any{
			"type":            "Episode",
			"resolution":      "4k",
			"group_by_series": true,
		})
		out := structured[jf.ItemListOutput](t, res)
		if len(scans) != 2 || scans[0].Get("Is4K") != "true" || scans[1].Has("Is4K") || scans[1].Has("MaxWidth") {
			t.Errorf("scans = %v, want 2 scans with Is4K=true on first and stripped on second", scans)
		}
		if out.TotalCount != 1 || out.Items[0].Name != "Series Beta" {
			t.Errorf("unexpected series output: %+v", out)
		}
	})

	t.Run("every encode aspect ratio partitions cleanly into one tier", func(t *testing.T) {
		catalog := []struct {
			id, name string
			w, h     int
			wantTier string
		}{
			{"sd-ntsc", "NTSC DVD", 720, 480, "sd"},
			{"sd-pal", "PAL Widescreen", 1024, 576, "sd"},
			{"720-4x3", "4:3 720p", 960, 720, "720p"},
			{"720-crop", "Cropped 720p", 1280, 534, "720p"},
			{"720-16x9", "16:9 720p", 1280, 720, "720p"},
			{"720-960", "4:3 960p", 1280, 960, "720p"},
			{"1080-4x3", "4:3 Blu-ray 1080p", 1440, 1080, "1080p"},
			{"1080-crop", "Cropped 1080p", 1920, 800, "1080p"},
			{"1080-16x9", "16:9 1080p", 1920, 1080, "1080p"},
			{"1440-crop", "Cropped 1440p", 2560, 1070, "1440p"},
			{"1440-16x9", "16:9 1440p", 2560, 1440, "1440p"},
			{"4k-4x3", "4:3 UHD 4K", 2880, 2160, "4k"},
			{"4k-portrait", "Portrait 4K", 2160, 3840, "4k"},
			{"4k-crop", "Cropped 4K", 3840, 1600, "4k"},
			{"4k-16x9", "16:9 4K", 3840, 2160, "4k"},
			{"4k-dci", "DCI 4K", 4096, 2160, "4k"},
			{"8k-crop", "Cropped 8K", 7680, 3200, "8k"},
			{"8k-16x9", "16:9 8K", 7680, 4320, "8k"},
		}
		atoi := func(s string) int {
			n := 0
			for i := range len(s) {
				n = n*10 + int(s[i]-'0')
			}
			return n
		}
		mc := &mockClient{
			getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
				var matched []map[string]any
				for _, c := range catalog {
					is4K := c.w >= 3800 || c.h >= 2100
					isHD := !is4K && (c.w >= 1200 || c.h >= 700)
					if params.Get("Is4K") == "true" && params.Get("IsHD") == "true" {
						if !isHD && !is4K {
							continue
						}
					} else if params.Get("Is4K") == "true" {
						if !is4K {
							continue
						}
					} else if params.Has("IsHD") {
						wantHD := params.Get("IsHD") == "true"
						if wantHD != isHD || (!wantHD && is4K) {
							continue
						}
					}
					if params.Has("MinWidth") && c.w < atoi(params.Get("MinWidth")) {
						continue
					}
					if params.Has("MaxWidth") && c.w > atoi(params.Get("MaxWidth")) {
						continue
					}
					if params.Has("MinHeight") && c.h < atoi(params.Get("MinHeight")) {
						continue
					}
					if params.Has("MaxHeight") && c.h > atoi(params.Get("MaxHeight")) {
						continue
					}
					matched = append(matched, map[string]any{"Id": c.id, "Name": c.name, "Type": "Movie", "Width": c.w, "Height": c.h})
				}
				return jsonInto(map[string]any{"Items": matched, "TotalRecordCount": len(matched)}, dest)
			},
		}
		gotTiers := map[string][]string{}
		for _, tier := range []string{"sd", "720p", "1080p", "1440p", "4k", "8k"} {
			out := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_browse", map[string]any{"resolution": tier}))
			for _, it := range out.Items {
				gotTiers[it.ID] = append(gotTiers[it.ID], tier)
			}
		}
		for _, c := range catalog {
			if !slices.Equal(gotTiers[c.id], []string{c.wantTier}) {
				t.Errorf("%s (%dx%d) matched tiers %v, want [%s]", c.id, c.w, c.h, gotTiers[c.id], c.wantTier)
			}
		}
	})
}

func TestSearch_CatalogTypesAndValidation(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Persons":
				if params.Get("ExcludePersonTypes") != "Artist,AlbumArtist" {
					t.Errorf("/Persons ExcludePersonTypes = %q", params.Get("ExcludePersonTypes"))
				}
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "p-1", "Name": "Director One", "Type": "Person"}},
					"TotalRecordCount": 1,
				}, dest)
			case "/Studios":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "st-1", "Name": "Studio One", "Type": "Studio"}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
	}

	personOut := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_search", map[string]any{"type": "Person"}))
	if personOut.TotalCount != 1 || personOut.Items[0].Name != "Director One" {
		t.Errorf("Person catalog: %+v", personOut)
	}

	studioOut := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_search", map[string]any{"type": "Studio"}))
	if studioOut.TotalCount != 1 || studioOut.Items[0].Name != "Studio One" {
		t.Errorf("Studio catalog: %+v", studioOut)
	}

	for _, invalid := range []map[string]any{
		{},
		{"query": "   "},
		{"type": "Movie"},
	} {
		res := callTool(t, mc, "", "jellyfin_search", invalid)
		if !res.IsError || !strings.Contains(resultText(t, res), "jellyfin_browse") {
			t.Errorf("expected query validation error pointing to jellyfin_browse for %v, got: %s", invalid, resultText(t, res))
		}
	}
}

func TestNameResolution_EndToEndAcrossTools(t *testing.T) {
	const (
		moviesID = "f137a2dd21bbc1b99aa5c0f6bf02a805"
		showsID  = "a67a9f2d8b1140e7b1109901829304aa"
		userAID  = "88112233445566778899aabbccddeeff"
	)

	var (
		lastItemsParams url.Values
		refreshedPath   string
		updatedPolicy   map[string]any
	)

	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Library/VirtualFolders":
				return jsonInto([]map[string]any{
					{"Name": "Movies", "ItemId": moviesID, "CollectionType": "movies"},
					{"Name": "TV Shows", "ItemId": showsID, "CollectionType": "tvshows"},
				}, dest)
			case "/Users":
				return jsonInto([]map[string]any{
					{"Id": "00112233445566778899aabbccddeeff", "Name": "admin", "Policy": map[string]any{"IsAdministrator": true}},
					{"Id": userAID, "Name": "UserA", "Policy": map[string]any{"IsAdministrator": false, "EnableAllFolders": false, "EnabledFolders": []string{moviesID}}},
				}, dest)
			case "/Users/UserA":
				t.Errorf("unexpected GET /Users/UserA (must resolve username via /Users first)")
				return &jf.APIError{StatusCode: 400}
			case "/Users/" + userAID:
				return jsonInto(map[string]any{
					"Id": userAID, "Name": "UserA",
					"Policy": map[string]any{"IsAdministrator": false, "EnableAllFolders": false, "EnabledFolders": []string{moviesID}},
				}, dest)
			case "/Items":
				if p := params.Get("ParentId"); p != "" && !jf.IsJellyfinID(p) {
					t.Errorf("unexpected non-GUID ParentId=%q sent to /Items", p)
					return &jf.APIError{StatusCode: 400}
				}
				if u := params.Get("UserId"); u != "" && u != "test-user-id" && !jf.IsJellyfinID(u) {
					t.Errorf("unexpected non-GUID UserId=%q sent to /Items", u)
					return &jf.APIError{StatusCode: 400}
				}
				lastItemsParams = maps.Clone(params)
				if params.Get("IncludeItemTypes") == "Episode" && params.Get("EnableImages") == "false" {
					return jsonInto(map[string]any{
						"Items":            []map[string]any{{"Id": "ep-1", "SeriesId": "s-1", "SeriesName": "Series Alpha"}},
						"TotalRecordCount": 1,
					}, dest)
				}
				return jsonInto(map[string]any{
					"Items": []map[string]any{{
						"Id": "m-1", "Name": "Test Movie A", "Type": "Movie",
						"UserData": map[string]any{"Played": true, "PlayCount": 1},
					}},
					"TotalRecordCount": 1,
				}, dest)
			case "/Items/Latest":
				if p := params.Get("ParentId"); p != moviesID {
					t.Errorf("Latest ParentId = %q, want %q", p, moviesID)
				}
				return jsonInto([]map[string]any{{"Id": "m-1", "Name": "Test Movie A", "Type": "Movie"}}, dest)
			case "/System/ActivityLog/Entries":
				return jsonInto(map[string]any{
					"Items": []map[string]any{{
						"Id": 1, "Name": "UserA played Test Movie A", "Type": "VideoPlaybackStopped",
						"UserId": userAID, "ItemId": "m-1", "Severity": "Information",
					}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
		postNoContentFunc: func(_ context.Context, endpoint string, _ url.Values, body any) error {
			if strings.HasSuffix(endpoint, "/Refresh") {
				if endpoint == "/Items/Movies/Refresh" {
					t.Errorf("unexpected POST /Items/Movies/Refresh (must resolve library name before POST)")
					return &jf.APIError{StatusCode: 400}
				}
				refreshedPath = endpoint
				return nil
			}
			if endpoint == "/Users/"+userAID+"/Policy" {
				b, _ := json.Marshal(body)
				_ = json.Unmarshal(b, &updatedPolicy)
				return nil
			}
			return nil
		},
	}

	// 1. jellyfin_browse with library name + username
	browseOut := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_browse", map[string]any{
		"parent_id": "Movies",
		"user_id":   "usera",
	}))
	if lastItemsParams.Get("ParentId") != moviesID || lastItemsParams.Get("UserId") != userAID {
		t.Errorf("browse params = %v, want ParentId=%s UserId=%s", lastItemsParams, moviesID, userAID)
	}
	if !hasNote(browseOut.Notes, "user 'UserA'") {
		t.Errorf("expected UserA note in %v", browseOut.Notes)
	}

	// 2. jellyfin_browse group_by_series with library name (resolved up-front, 0 HTTP 400s)
	seriesOut := structured[jf.ItemListOutput](t, callTool(t, mc, "", "jellyfin_browse", map[string]any{
		"parent_id":       "tv shows",
		"type":            "Episode",
		"group_by_series": true,
	}))
	if seriesOut.TotalCount != 1 || seriesOut.Items[0].Name != "Series Alpha" {
		t.Errorf("group_by_series by library name: %+v", seriesOut)
	}

	// 3. Unknown library name lists available libraries (including group_by_series)
	for _, badArgs := range []map[string]any{
		{"parent_id": "Anime"},
		{"parent_id": "Anime", "type": "Episode", "group_by_series": true},
	} {
		badLib := callTool(t, mc, "", "jellyfin_browse", badArgs)
		if !badLib.IsError || !strings.Contains(resultText(t, badLib), "Movies, TV Shows") {
			t.Errorf("expected available libraries in error for %v, got: %s", badArgs, resultText(t, badLib))
		}
	}

	// 4. jellyfin_recommendations latest with library name
	recOut := structured[jf.RecommendationsOutput](t, callTool(t, mc, "", "jellyfin_recommendations", map[string]any{
		"action":    "latest",
		"parent_id": "movies",
	}))
	if recOut.Items == nil || len(*recOut.Items) != 1 {
		t.Errorf("latest by library name: %+v", recOut)
	}

	// 5. jellyfin_playback_stats playback_history with username
	histRes := callTool(t, mc, "", "jellyfin_playback_stats", map[string]any{
		"action":  "playback_history",
		"user_id": "UserA",
	})
	if histRes.IsError || !strings.Contains(resultText(t, histRes), `"actual_playback":true`) {
		t.Errorf("playback_history by username: %s", resultText(t, histRes))
	}

	// 6. jellyfin_users get + update_policy with username and library names
	userGet := callTool(t, mc, "", "jellyfin_users", map[string]any{"action": "get", "user_id": "UserA"})
	if userGet.IsError || !strings.Contains(resultText(t, userGet), `"name":"UserA"`) || !strings.Contains(resultText(t, userGet), moviesID) || !strings.Contains(resultText(t, userGet), `"enabled_folder_names":["Movies"]`) {
		t.Errorf("users get by username: %s", resultText(t, userGet))
	}
	policyPrompt := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":             "update_policy",
		"user_id":            "UserA",
		"enable_all_folders": false,
		"enabled_folder_ids": []any{"Movies", "TV Shows"},
	})
	if policyPrompt.IsError || !strings.Contains(resultText(t, policyPrompt), "Movies") || !strings.Contains(resultText(t, policyPrompt), "TV Shows") {
		t.Errorf("expected library names in update_policy confirmation prompt, got: %s", resultText(t, policyPrompt))
	}
	policyRes := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":             "update_policy",
		"user_id":            "UserA",
		"enable_all_folders": false,
		"enabled_folder_ids": []any{"Movies", "TV Shows"},
		"confirm":            true,
	})
	if policyRes.IsError {
		t.Errorf("update_policy error: %s", resultText(t, policyRes))
	}
	gotFolders, _ := json.Marshal(updatedPolicy["EnabledFolders"])
	if string(gotFolders) != `["`+moviesID+`","`+showsID+`"]` {
		t.Errorf("EnabledFolders = %s, want Movies and TV Shows UUIDs", gotFolders)
	}

	// 7. jellyfin_library_manage scan by library name
	scanRes := callTool(t, mc, "", "jellyfin_library_manage", map[string]any{
		"action":  "scan",
		"item_id": "Movies",
	})
	if scanRes.IsError || refreshedPath != "/Items/"+moviesID+"/Refresh" {
		t.Errorf("scan by library name: path=%q text=%s", refreshedPath, resultText(t, scanRes))
	}
}

func TestReadOnlyMode_MixedToolsAndWriteRefusal(t *testing.T) {
	writes := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/ScheduledTasks":
				return jsonInto([]map[string]any{{"Id": "t-1", "Name": "Scan Media Library", "State": "Idle"}}, dest)
			case "/LiveTv/Recordings":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "r-1", "Name": "News"}}, "TotalRecordCount": 1}, dest)
			case "/Users":
				return jsonInto([]map[string]any{{"Id": "u-1", "Name": "admin", "Policy": map[string]any{"IsAdministrator": true}}}, dest)
			case "/Plugins":
				return jsonInto([]map[string]any{{"Id": "pl-1", "Name": "Sample Plugin", "Version": "1.0", "Status": "Active"}}, dest)
			case "/Devices":
				return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "d-1", "Name": "Living Room TV"}}}, dest)
			case "/UserItems/m-1/UserData":
				return jsonInto(map[string]any{"Played": true, "PlayCount": 2}, dest)
			case "/Items/m-1/Images":
				return jsonInto([]map[string]any{{"ImageType": "Primary", "Width": 1000, "Height": 1500}}, dest)
			case "/Library/VirtualFolders":
				return jsonInto([]map[string]any{{"Name": "Movies", "ItemId": "lib-1", "CollectionType": "movies"}}, dest)
			case "/Items/m-1/RemoteSearch/Subtitles/eng":
				return jsonInto([]map[string]any{{"Id": "sub-1", "Name": "Sub", "Format": "srt", "ThreeLetterISOLanguageName": "eng"}}, dest)
			default:
				return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
			}
		},
		postFunc:          func(context.Context, string, url.Values, any, any) error { writes++; return nil },
		postNoContentFunc: func(context.Context, string, url.Values, any) error { writes++; return nil },
		delFunc:           func(context.Context, string, url.Values) error { writes++; return nil },
	}

	cs := newReadOnlySession(t, mc, "")
	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal("ListTools:", err)
	}

	// 25 total tools minus 2 pure-write tools (jellyfin_playback, jellyfin_collections) = 23 tools.
	if len(list.Tools) != 23 {
		names := make([]string, len(list.Tools))
		for i, tl := range list.Tools {
			names[i] = tl.Name
		}
		t.Fatalf("got %d tools in --read-only, want 23: %v", len(list.Tools), names)
	}

	writeOnlyProps := []string{"confirm", "dry_run", "image_data", "subtitle_data", "password", "triggers", "repos"}
	for _, tl := range list.Tools {
		if tl.Name == "jellyfin_playback" || tl.Name == "jellyfin_collections" {
			t.Errorf("pure-write tool %s must not be registered in --read-only", tl.Name)
		}
		if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint {
			t.Errorf("%s annotations in --read-only = %+v, want ReadOnlyHint=true DestructiveHint=false", tl.Name, tl.Annotations)
		}
		b, _ := json.Marshal(tl.InputSchema)
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatalf("%s schema unmarshal: %v", tl.Name, err)
		}
		for _, banned := range writeOnlyProps {
			if _, ok := schema.Properties[banned]; ok {
				t.Errorf("%s in --read-only still exposes write-only property %q", tl.Name, banned)
			}
		}
		if act, ok := schema.Properties["action"]; ok {
			for _, badAct := range []string{"delete", "delete_recording", "create", "restart", "shutdown", "upload", "merge_versions", "split_versions"} {
				if slices.Contains(act.Enum, badAct) {
					t.Errorf("%s in --read-only still lists write action %q in enum %v", tl.Name, badAct, act.Enum)
				}
			}
		}
	}

	// Read actions on mixed tools succeed in --read-only mode.
	readCalls := []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_playlists", map[string]any{"action": "list"}},
		{"jellyfin_library_manage", map[string]any{"action": "list_folders"}},
		{"jellyfin_user_data", map[string]any{"action": "get_user_data", "item_id": "m-1"}},
		{"jellyfin_users", map[string]any{"action": "list"}},
		{"jellyfin_plugins", map[string]any{"action": "list"}},
		{"jellyfin_server", map[string]any{"action": "list_tasks"}},
		{"jellyfin_devices", map[string]any{"action": "list"}},
		{"jellyfin_live_tv", map[string]any{"action": "recordings"}},
		{"jellyfin_images", map[string]any{"action": "list", "item_id": "m-1"}},
		{"jellyfin_subtitles_lyrics", map[string]any{"action": "search_subtitles", "item_id": "m-1", "language": "eng"}},
	}
	for _, rc := range readCalls {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: rc.tool, Arguments: rc.args})
		if err != nil {
			t.Errorf("%s %v RPC error: %v", rc.tool, rc.args, err)
			continue
		}
		if res.IsError {
			t.Errorf("%s %v unexpected tool error: %s", rc.tool, rc.args, resultText(t, res))
		}
	}

	// Write actions on mixed tools are rejected (either by schema enum validation or handler guard) with 0 writes.
	writeCalls := []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_server", map[string]any{"action": "restart"}},
		{"jellyfin_playlists", map[string]any{"action": "delete", "playlist_id": "p-1"}},
		{"jellyfin_users", map[string]any{"action": "delete", "user_id": "u-1"}},
		{"jellyfin_live_tv", map[string]any{"action": "delete_recording", "recording_id": "r-1"}},
	}
	for _, wc := range writeCalls {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: wc.tool, Arguments: wc.args})
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("%s %v succeeded in --read-only mode!", wc.tool, wc.args)
		}
	}
	if writes != 0 {
		t.Errorf("expected 0 write calls in --read-only mode, got %d", writes)
	}
}

func TestSchemas_ZeroNullUnionsAndAllPromptsAndGuidesValid(t *testing.T) {
	for _, ro := range []bool{false, true} {
		var cs *mcp.ClientSession
		if ro {
			cs = newReadOnlySession(t, &mockClient{}, "")
		} else {
			cs = newFullSession(t, &mockClient{})
		}
		list, err := cs.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tl := range list.Tools {
			inBytes, _ := json.Marshal(tl.InputSchema)
			if strings.Contains(string(inBytes), `"null"`) {
				t.Errorf("ro=%v %s InputSchema contains null union: %s", ro, tl.Name, inBytes)
			}
			if tl.OutputSchema != nil {
				outBytes, _ := json.Marshal(tl.OutputSchema)
				var raw map[string]any
				if err := json.Unmarshal(outBytes, &raw); err != nil {
					t.Fatalf("%s OutputSchema unmarshal: %v", tl.Name, err)
				}
				assertNoOptionalNullUnions(t, tl.Name, "$", raw)
			}
		}

		if !ro {
			pList, err := cs.ListPrompts(t.Context(), nil)
			if err != nil {
				t.Fatal("ListPrompts:", err)
			}
			for _, p := range pList.Prompts {
				args := map[string]string{}
				for _, a := range p.Arguments {
					switch a.Name {
					case "library":
						args[a.Name] = "Movies"
					case "user", "username":
						args[a.Name] = "UserA"
					case "issue":
						args[a.Name] = "missing_overview"
					default:
						args[a.Name] = "test"
					}
				}
				res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: p.Name, Arguments: args})
				if err != nil {
					t.Fatalf("GetPrompt(%s): %v", p.Name, err)
				}
				for _, msg := range res.Messages {
					if tc, ok := msg.Content.(*mcp.TextContent); ok {
						assertNamesRegisteredTools(t, cs, "prompt "+p.Name, tc.Text)
					}
				}
			}

			rList, err := cs.ListResources(t.Context(), nil)
			if err != nil {
				t.Fatal("ListResources:", err)
			}
			for _, r := range rList.Resources {
				if !strings.HasPrefix(r.URI, "jellyfin://guides/") {
					continue
				}
				read, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: r.URI})
				if err != nil {
					t.Fatalf("ReadResource(%s): %v", r.URI, err)
				}
				for _, c := range read.Contents {
					assertNamesRegisteredTools(t, cs, "guide "+r.URI, c.Text)
				}
			}
		}
	}
}

func assertNoOptionalNullUnions(t *testing.T, toolName, path string, node map[string]any) {
	t.Helper()
	if node == nil {
		return
	}
	reqSet := map[string]bool{}
	if reqSlice, ok := node["required"].([]any); ok {
		for _, r := range reqSlice {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for name, rawProp := range props {
			prop, _ := rawProp.(map[string]any)
			if prop == nil {
				continue
			}
			if !reqSet[name] {
				if types, ok := prop["type"].([]any); ok {
					for _, tp := range types {
						if tp == "null" {
							t.Errorf("%s optional field %s.%s has null union: %v", toolName, path, name, types)
						}
					}
				}
			}
			assertNoOptionalNullUnions(t, toolName, path+"."+name, prop)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		assertNoOptionalNullUnions(t, toolName, path+"[]", items)
	}
}

func TestConfirmWithDefaultDryRun_Rejected(t *testing.T) {
	mc := &mockClient{}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"jellyfin_playlists", map[string]any{"action": "deduplicate", "playlist_id": "pl-1", "confirm": true}},
		{"jellyfin_playlists", map[string]any{"action": "remove_items", "playlist_id": "pl-1", "item_ids": []any{"a"}, "dry_run": true, "confirm": true}},
		{"jellyfin_metadata", map[string]any{"action": "batch_update", "item_ids": []any{"m-1"}, "tags": []any{"sci-fi"}, "confirm": true}},
		{"jellyfin_metadata", map[string]any{"action": "update", "item_id": "m-1", "name": "New", "dry_run": true, "confirm": true}},
	} {
		res := callTool(t, mc, "", tc.tool, tc.args)
		txt := resultText(t, res)
		if !res.IsError || !strings.Contains(txt, "dry_run=false and confirm=true") {
			t.Errorf("%s(%v): expected dry_run=false and confirm=true guidance, got isError=%v: %s", tc.tool, tc.args, res.IsError, txt)
		}
	}
}

func TestMergeVersions_PreGateValidation(t *testing.T) {
	// 1. Non-GUID item_ids rejected with HTTP 400 -> pre-gate error with hint instead of confirmation prompt.
	mcBadGUID := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			if endpoint == "/Items" {
				return &jf.APIError{StatusCode: 400, Body: "invalid Ids"}
			}
			return nil
		},
	}
	res := callTool(t, mcBadGUID, "", "jellyfin_metadata", map[string]any{
		"action":   "merge_versions",
		"item_ids": []any{"bad-id-1", "bad-id-2"},
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "jellyfin_search") {
		t.Errorf("expected pre-gate error with search hint on HTTP 400, got isError=%v: %s", res.IsError, resultText(t, res))
	}

	// 2. Missing item ID when /Items returns fewer items -> pre-gate not-found error with hint.
	mcMissing := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Items" {
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Id": "10000000000000000000000000000005", "Name": "Test Movie B"},
				}}, dest)
			}
			return nil
		},
	}
	res2 := callTool(t, mcMissing, "", "jellyfin_metadata", map[string]any{
		"action":   "merge_versions",
		"item_ids": []any{"10000000000000000000000000000005", "10000000000000000000000000000099"},
	})
	if !res2.IsError || !strings.Contains(resultText(t, res2), "was not found") {
		t.Errorf("expected pre-gate item-not-found error, got isError=%v: %s", res2.IsError, resultText(t, res2))
	}
}

func TestTVShows_SeriesNameResolution(t *testing.T) {
	const seriesAlphaID = "20000000000000000000000000000001"
	var nextUpSeriesID string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch {
			case endpoint == "/Shows/NextUp":
				nextUpSeriesID = params.Get("SeriesId")
				if !jf.IsJellyfinID(nextUpSeriesID) {
					return &jf.APIError{StatusCode: 400, Body: "SeriesId must be GUID"}
				}
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "22000000000000000000000000000104", "Name": "Episode Four", "Type": "Episode"}},
					"TotalRecordCount": 1,
				}, dest)
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "Series":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": seriesAlphaID, "Name": "Series Alpha", "Type": "Series", "ProductionYear": 2022}},
					"TotalRecordCount": 1,
				}, dest)
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "Season":
				if !jf.IsJellyfinID(params.Get("ParentId")) {
					return &jf.APIError{StatusCode: 400, Body: "ParentId must be GUID"}
				}
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "21000000000000000000000000000001", "Name": "Season 1", "IndexNumber": 1, "ChildCount": 9}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
	}

	// next_up resolves series name up front without sending non-GUID SeriesId to /Shows/NextUp.
	res := callTool(t, mc, "", "jellyfin_tv_shows", map[string]any{
		"action":    "next_up",
		"series_id": "Series Alpha",
	})
	if res.IsError {
		t.Fatalf("next_up by name failed: %s", resultText(t, res))
	}
	if nextUpSeriesID != seriesAlphaID {
		t.Errorf("next_up SeriesId = %q, want %q", nextUpSeriesID, seriesAlphaID)
	}

	// seasons resolves series name when Jellyfin rejects non-GUID ParentId with 400.
	resSeasons := callTool(t, mc, "", "jellyfin_tv_shows", map[string]any{
		"action":    "seasons",
		"series_id": "Series Alpha",
	})
	if resSeasons.IsError {
		t.Fatalf("seasons by name failed: %s", resultText(t, resSeasons))
	}
	out := structured[jf.TVShowsOutput](t, resSeasons)
	if out.Seasons == nil || len(*out.Seasons) != 1 || (*out.Seasons)[0].EpisodeCount != 9 {
		t.Errorf("unexpected seasons output: %+v", out.Seasons)
	}
}

func TestPlaylists_PlaylistNameResolution(t *testing.T) {
	const synthID = "40000000000000000000000000000001"
	var requestedEndpoints []string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			requestedEndpoints = append(requestedEndpoints, endpoint)
			switch {
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "Playlist":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": synthID, "Name": "Late Night Synth", "Type": "Playlist"}},
					"TotalRecordCount": 1,
				}, dest)
			case endpoint == "/Playlists/"+synthID+"/Items":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "32000000000000000000000000000001", "Name": "Test Track One", "Type": "Audio"}},
					"TotalRecordCount": 1,
				}, dest)
			default:
				return &jf.APIError{StatusCode: 404, Body: "not found: " + endpoint}
			}
		},
	}

	res := callTool(t, mc, "", "jellyfin_playlists", map[string]any{
		"action":      "get",
		"playlist_id": "Late Night Synth",
	})
	if res.IsError {
		t.Fatalf("playlist get by name failed: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "Test Track One") {
		t.Errorf("expected track in playlist output, got: %s", resultText(t, res))
	}
	for _, ep := range requestedEndpoints {
		if strings.Contains(ep, "Late Night Synth") || strings.Contains(ep, "Late%20Night%20Synth") {
			t.Errorf("sent unresolved playlist name in URL path: %s", ep)
		}
	}

	// Nonexistent playlist name lists available playlists.
	resMissing := callTool(t, mc, "", "jellyfin_playlists", map[string]any{
		"action":      "get",
		"playlist_id": "Missing Mix",
	})
	if !resMissing.IsError || !strings.Contains(resultText(t, resMissing), "Late Night Synth") {
		t.Errorf("expected error naming available playlist, got isError=%v: %s", resMissing.IsError, resultText(t, resMissing))
	}
}

func TestBrowse_PersonGUIDUsesPersonIds(t *testing.T) {
	const directorID = "80000000000000000000000000000005"
	var lastParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if endpoint == "/Items" {
				lastParams = params
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": "10000000000000000000000000000009", "Name": "Test Movie C", "Type": "Movie"}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
	}

	resGUID := callTool(t, mc, "", "jellyfin_browse", map[string]any{
		"person": directorID,
		"type":   "Movie",
	})
	if resGUID.IsError {
		t.Fatalf("browse by person GUID failed: %s", resultText(t, resGUID))
	}
	if lastParams.Get("PersonIds") != directorID || lastParams.Get("Person") != "" {
		t.Errorf("person GUID params: PersonIds=%q Person=%q, want PersonIds=%q", lastParams.Get("PersonIds"), lastParams.Get("Person"), directorID)
	}

	resName := callTool(t, mc, "", "jellyfin_browse", map[string]any{
		"person": "Director Two",
		"type":   "Movie",
	})
	if resName.IsError {
		t.Fatalf("browse by person name failed: %s", resultText(t, resName))
	}
	if lastParams.Get("Person") != "Director Two" || lastParams.Get("PersonIds") != "" {
		t.Errorf("person name params: Person=%q PersonIds=%q, want Person=Director Two", lastParams.Get("Person"), lastParams.Get("PersonIds"))
	}
}

func TestPlayback_DeviceNameResolutionAndCollectionsNameResolution(t *testing.T) {
	const (
		tvSessID = "sess-tv-1"
		boxSetID = "50000000000000000000000000000001"
	)
	var postedPaths []string
	var deletedPaths []string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch {
			case endpoint == "/Sessions":
				return jsonInto([]map[string]any{
					{"Id": tvSessID, "DeviceName": "Living Room TV", "Client": "Media App", "Capabilities": map[string]any{"SupportsMediaControl": true}},
					{"Id": "sess-web-idle", "DeviceName": "Kitchen Tablet", "Client": "Jellyfin Web", "Capabilities": map[string]any{"SupportsMediaControl": false}},
				}, dest)
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "BoxSet":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": boxSetID, "Name": "Sci-Fi Essentials", "Type": "BoxSet"}},
					"TotalRecordCount": 1,
				}, dest)
			}
			return nil
		},
		postNoContentFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			postedPaths = append(postedPaths, endpoint)
			return nil
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error {
			deletedPaths = append(deletedPaths, endpoint)
			return nil
		},
	}

	// 1. Playback resolves unambiguous controllable DeviceName (case-insensitive) to session ID.
	playRes := callTool(t, mc, "", "jellyfin_playback", map[string]any{
		"action":     "PlayNow",
		"session_id": "living room tv",
		"item_ids":   []any{"10000000000000000000000000000009"},
	})
	if playRes.IsError {
		t.Fatalf("PlayNow by device name failed: %s", resultText(t, playRes))
	}
	if !slices.Contains(postedPaths, "/Sessions/"+tvSessID+"/Playing") {
		t.Errorf("postedPaths = %v, want /Sessions/%s/Playing", postedPaths, tvSessID)
	}

	// 2. Non-controllable device name refuses remote control cleanly.
	nonCtrl := callTool(t, mc, "", "jellyfin_playback", map[string]any{
		"action":     "Pause",
		"session_id": "Kitchen Tablet",
	})
	if !nonCtrl.IsError || !strings.Contains(resultText(t, nonCtrl), "does not accept remote control") {
		t.Errorf("expected non-controllable error, got: %s", resultText(t, nonCtrl))
	}

	// 3. Unknown session lists controllable device names.
	missingSess := callTool(t, mc, "", "jellyfin_playback", map[string]any{
		"action":     "Pause",
		"session_id": "Bedroom TV",
	})
	if !missingSess.IsError || !strings.Contains(resultText(t, missingSess), "Living Room TV") {
		t.Errorf("expected controllable device hint in error, got: %s", resultText(t, missingSess))
	}

	// 4. Collections add_items and remove_items resolve collection_id by name.
	addRes := callTool(t, mc, "", "jellyfin_collections", map[string]any{
		"action":        "add_items",
		"collection_id": "sci-fi essentials",
		"item_ids":      []any{"10000000000000000000000000000001"},
	})
	if addRes.IsError || !slices.Contains(postedPaths, "/Collections/"+boxSetID+"/Items") {
		t.Errorf("collections add_items by name: err=%v paths=%v text=%s", addRes.IsError, postedPaths, resultText(t, addRes))
	}
	remPrompt := callTool(t, mc, "", "jellyfin_collections", map[string]any{
		"action":        "remove_items",
		"collection_id": "Sci-Fi Essentials",
		"item_ids":      []any{"10000000000000000000000000000001"},
	})
	if remPrompt.IsError || !strings.Contains(resultText(t, remPrompt), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected confirmation prompt for remove_items, got: %s", resultText(t, remPrompt))
	}
	remRes := callTool(t, mc, "", "jellyfin_collections", map[string]any{
		"action":        "remove_items",
		"collection_id": "Sci-Fi Essentials",
		"item_ids":      []any{"10000000000000000000000000000001"},
		"confirm":       true,
	})
	if remRes.IsError || !slices.Contains(deletedPaths, "/Collections/"+boxSetID+"/Items") {
		t.Errorf("collections remove_items by name: err=%v deleted=%v text=%s", remRes.IsError, deletedPaths, resultText(t, remRes))
	}
}

func TestResolveErrResult_CleanCapitalizationAcrossTools(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Users":
				return jsonInto([]map[string]any{{"Id": "90000000000000000000000000000001", "Name": "admin"}}, dest)
			case "/Library/VirtualFolders":
				return jsonInto([]map[string]any{{"Name": "Movies", "ItemId": "11111111111111111111111111111111"}}, dest)
			default:
				return &jf.APIError{StatusCode: 404}
			}
		},
	}

	for _, tc := range []struct {
		tool       string
		args       map[string]any
		wantPrefix string
	}{
		{"jellyfin_users", map[string]any{"action": "get", "user_id": "GhostUser"}, "No user has ID 'GhostUser'"},
		{"jellyfin_users", map[string]any{"action": "delete", "user_id": "GhostUser"}, "No user has ID 'GhostUser'"},
		{"jellyfin_playback_stats", map[string]any{"action": "playback_history", "user_id": "GhostUser"}, "No user has ID 'GhostUser'"},
		{"jellyfin_library_manage", map[string]any{"action": "scan", "item_id": "Anime"}, "No library matches 'Anime'"},
		{"jellyfin_analytics", map[string]any{"action": "library_stats", "parent_id": "Anime"}, "No library matches 'Anime'"},
	} {
		res := callTool(t, mc, "", tc.tool, tc.args)
		txt := resultText(t, res)
		if !res.IsError || !strings.HasPrefix(txt, tc.wantPrefix) {
			t.Errorf("%s(%v): got %q, want prefix %q", tc.tool, tc.args, txt, tc.wantPrefix)
		}
		if strings.Contains(txt, "Jellyfin API error") || strings.Contains(txt, "Failed to get user") {
			t.Errorf("%s(%v): misleading wrapper prefix in %q", tc.tool, tc.args, txt)
		}
	}

	// Verify troubleshoot prompt instructs log_type="ffmpeg" on jellyfin_system_info action="logs".
	cs := newFullSession(t, mc)
	pr, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{
		Name:      "troubleshoot",
		Arguments: map[string]string{"issue": "buffering on 4K HEVC"},
	})
	if err != nil {
		t.Fatal("GetPrompt(troubleshoot):", err)
	}
	var promptBody strings.Builder
	for _, msg := range pr.Messages {
		if tc, ok := msg.Content.(*mcp.TextContent); ok {
			promptBody.WriteString(tc.Text)
		}
	}
	if !strings.Contains(promptBody.String(), `log_type="ffmpeg"`) {
		t.Errorf("troubleshoot prompt missing log_type=\"ffmpeg\": %s", promptBody.String())
	}
}

func TestGetItem_ExplicitVideoSubtitleFieldsEndToEnd(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Items/movie-nosubs":
				return jsonInto(map[string]any{
					"Id":           "movie-nosubs",
					"Name":         "Test Movie C",
					"Type":         "Movie",
					"HasSubtitles": false,
					"MediaSources": []any{
						map[string]any{
							"Container": "mkv",
							"MediaStreams": []any{
								map[string]any{"Type": "Video", "Codec": "h264", "Width": float64(1920), "Height": float64(1080)},
								map[string]any{"Type": "Audio", "Index": float64(1), "Codec": "flac", "Channels": float64(1), "Language": "jpn"},
							},
						},
					},
				}, dest)
			case "/Items/series-1":
				return jsonInto(map[string]any{
					"Id":   "series-1",
					"Name": "Series Gamma",
					"Type": "Series",
				}, dest)
			case "/Items/audio-1":
				return jsonInto(map[string]any{
					"Id":        "audio-1",
					"Name":      "Main Theme",
					"Type":      "Audio",
					"HasLyrics": false,
					"MediaSources": []any{
						map[string]any{
							"Container": "flac",
							"MediaStreams": []any{
								map[string]any{"Type": "Audio", "Index": float64(0), "Codec": "flac", "Channels": float64(2)},
							},
						},
					},
				}, dest)
			default:
				return jsonInto(map[string]any{"Items": []map[string]any{}}, dest)
			}
		},
	}

	movieTxt := resultText(t, callTool(t, mc, "", "jellyfin_get_item", map[string]any{"item_id": "movie-nosubs"}))
	for _, want := range []string{`"has_subtitles":false`, `"subtitle_streams":[]`} {
		if !strings.Contains(movieTxt, want) {
			t.Errorf("unsubtitled movie missing %s: %s", want, movieTxt)
		}
	}
	if strings.Contains(movieTxt, `"has_lyrics"`) {
		t.Errorf("movie unexpectedly included has_lyrics: %s", movieTxt)
	}

	seriesTxt := resultText(t, callTool(t, mc, "", "jellyfin_get_item", map[string]any{"item_id": "series-1"}))
	for _, unwanted := range []string{`"has_subtitles"`, `"has_lyrics"`, `"subtitle_streams"`} {
		if strings.Contains(seriesTxt, unwanted) {
			t.Errorf("series unexpectedly included %s: %s", unwanted, seriesTxt)
		}
	}

	audioTxt := resultText(t, callTool(t, mc, "", "jellyfin_get_item", map[string]any{"item_id": "audio-1"}))
	if !strings.Contains(audioTxt, `"has_lyrics":false`) {
		t.Errorf("audio item missing explicit has_lyrics=false: %s", audioTxt)
	}
	for _, unwanted := range []string{`"has_subtitles"`, `"subtitle_streams"`} {
		if strings.Contains(audioTxt, unwanted) {
			t.Errorf("audio item unexpectedly included %s: %s", unwanted, audioTxt)
		}
	}
}

func TestPreGateValidation_RejectsGuessedSlugIDsWithSearchHint(t *testing.T) {
	const boxSetID = "20000000000000000000000000000002"
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch {
			case endpoint == "/Items" && params.Get("IncludeItemTypes") == "BoxSet":
				return jsonInto(map[string]any{
					"Items":            []map[string]any{{"Id": boxSetID, "Name": "Sci-Fi", "Type": "BoxSet"}},
					"TotalRecordCount": 1,
				}, dest)
			case endpoint == "/Items/"+boxSetID:
				return jsonInto(map[string]any{"Id": boxSetID, "Name": "Sci-Fi", "Type": "BoxSet"}, dest)
			case endpoint == "/Items" && params.Get("Ids") != "":
				// Jellyfin returns 400 on non-UUID slug strings in Ids=...
				return &jf.APIError{StatusCode: 400}
			default:
				return &jf.APIError{StatusCode: 404}
			}
		},
	}

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"metadata apply", "jellyfin_metadata", map[string]any{"action": "apply", "item_id": "test-movie-1982", "provider_name": "Tmdb", "provider_id": "78"}},
		{"metadata update dry_run", "jellyfin_metadata", map[string]any{"action": "update", "item_id": "test-movie-1982", "overview": "new", "dry_run": true}},
		{"metadata update gate", "jellyfin_metadata", map[string]any{"action": "update", "item_id": "test-movie-1982", "overview": "new"}},
		{"metadata batch_update dry_run", "jellyfin_metadata", map[string]any{"action": "batch_update", "item_ids": []any{"test-movie-1982-entry-1"}, "tags": []any{"4k"}}},
		{"metadata batch_update gate", "jellyfin_metadata", map[string]any{"action": "batch_update", "item_ids": []any{"test-movie-1982-entry-1"}, "tags": []any{"4k"}, "dry_run": false}},
		{"metadata merge_versions", "jellyfin_metadata", map[string]any{"action": "merge_versions", "item_ids": []any{"test-movie-1982-entry-1", "test-movie-1982-entry-2"}}},
		{"metadata split_versions", "jellyfin_metadata", map[string]any{"action": "split_versions", "item_id": "test-movie-1982-entry-1"}},
		{"subtitles batch_download_subtitles", "jellyfin_subtitles_lyrics", map[string]any{"action": "batch_download_subtitles", "item_ids": []any{"test-movie-1982-entry-1"}}},
		{"subtitles delete_lyrics", "jellyfin_subtitles_lyrics", map[string]any{"action": "delete_lyrics", "item_id": "test-movie-1982-entry-1"}},
		{"user_data set_user_data", "jellyfin_user_data", map[string]any{"action": "set_user_data", "item_id": "test-movie-1982-entry-1", "played": true}},
		{"collections remove_items", "jellyfin_collections", map[string]any{"action": "remove_items", "collection_id": boxSetID, "item_ids": []any{"test-movie-1982-entry-1"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, mc, "", tc.tool, tc.args)
			txt := resultText(t, res)
			if !res.IsError {
				t.Fatalf("expected pre-gate error for %s, got non-error: %s", tc.name, txt)
			}
			if strings.Contains(txt, "CONFIRMATION REQUIRED") {
				t.Errorf("confirmation gate fired on invalid ID for %s: %s", tc.name, txt)
			}
			if !strings.Contains(txt, "jellyfin_search") {
				t.Errorf("missing jellyfin_search recovery hint for %s: %s", tc.name, txt)
			}
		})
	}

	// Also verify missing-ID detection when /Items returns 200 with an empty Items slice (well-formed nonexistent hex ID).
	emptyItemsMC := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Items" {
				return jsonInto(map[string]any{"Items": []map[string]any{}}, dest)
			}
			return nil
		},
	}
	missingHex := callTool(t, emptyItemsMC, "", "jellyfin_metadata", map[string]any{
		"action":   "batch_update",
		"item_ids": []any{"deadbeefdeadbeefdeadbeefdeadbeef"},
		"tags":     []any{"classic"},
		"dry_run":  false,
	})
	if !missingHex.IsError || !strings.Contains(resultText(t, missingHex), "Use jellyfin_search or jellyfin_browse") {
		t.Errorf("expected missing-ID refusal for nonexistent hex ID, got: %s", resultText(t, missingHex))
	}
}
