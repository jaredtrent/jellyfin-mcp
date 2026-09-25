package tools_test

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"testing"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func TestTVShows_Seasons_ItemsRouteWithUserID(t *testing.T) {
	var gotEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint = endpoint
			gotParams = maps.Clone(params)
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "season-1", "Name": "Season 1", "IndexNumber": 1, "ChildCount": 8},
				},
				"TotalRecordCount": 1,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_tv_shows", map[string]any{
		"action":    "seasons",
		"series_id": "series-1",
	})

	out := structured[jf.TVShowsOutput](t, result)
	if gotEndpoint != "/Items" {
		t.Errorf("endpoint = %q, want /Items", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if got := gotParams.Get("ParentId"); got != "series-1" {
		t.Errorf("ParentId = %q, want series-1", got)
	}
	if out.Seasons == nil || len(*out.Seasons) != 1 {
		t.Fatalf("seasons = %+v, want one", out.Seasons)
	}
	if s := (*out.Seasons)[0]; s.ID != "season-1" || s.SeasonNumber != 1 || s.EpisodeCount != 8 {
		t.Errorf("seasons[0] = %+v, want season-1, number 1, 8 episodes", s)
	}
	if out.Episodes != nil || out.NextUp != nil {
		t.Errorf("episodes = %v, next_up = %v, want both absent for action seasons", out.Episodes, out.NextUp)
	}
}

func TestTVShows_Episodes_ItemsRouteWithUserID(t *testing.T) {
	type call struct {
		endpoint string
		params   url.Values
	}
	var calls []call
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			calls = append(calls, call{endpoint, maps.Clone(params)})
			switch params.Get("IncludeItemTypes") {
			case "Season":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Id": "season-1", "Name": "Season 1", "IndexNumber": 1},
						{"Id": "season-2", "Name": "Season 2", "IndexNumber": 2},
					},
				}, dest)
			case "Episode":
				return jsonInto(map[string]any{
					"Items": []map[string]any{
						{"Id": "episode-1", "Name": "Episode A", "IndexNumber": 1},
					},
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_tv_shows", map[string]any{
		"action":        "episodes",
		"series_id":     "series-1",
		"season_number": 2,
	})

	out := structured[jf.TVShowsOutput](t, result)
	if len(calls) != 2 {
		t.Fatalf("got %d Get calls, want 2 (season lookup, episodes)", len(calls))
	}
	for i, c := range calls {
		if c.endpoint != "/Items" {
			t.Errorf("call %d endpoint = %q, want /Items", i, c.endpoint)
		}
		if got := c.params.Get("UserId"); got != "test-user-id" {
			t.Errorf("call %d UserId = %q, want test-user-id", i, got)
		}
	}
	if got := calls[1].params.Get("ParentId"); got != "season-2" {
		t.Errorf("episodes ParentId = %q, want season-2", got)
	}
	if out.Episodes == nil || len(*out.Episodes) != 1 {
		t.Fatalf("episodes = %+v, want one", out.Episodes)
	}
	if e := (*out.Episodes)[0]; e.ID != "episode-1" || e.Name != "Episode A" || e.SeasonNumber != 2 || e.EpisodeNumber != 1 {
		t.Errorf("episodes[0] = %+v, want Episode A, season 2 episode 1", e)
	}
}

// musicGenresClient serves two music libraries and one movie library. The
// music libraries share one genre by Id and one Id-less genre by
// case-insensitive name. Each /Genres request's params are appended to
// genreCalls, and any other endpoint is appended to otherEndpoints.
func musicGenresClient(genreCalls *[]url.Values, otherEndpoints *[]string) *mockClient {
	genresByLibrary := map[string][]map[string]any{
		"lib-music-a": {
			{"Id": "genre-c", "Name": "Genre C", "Type": "MusicGenre"},
			{"Id": "genre-a", "Name": "Genre A", "Type": "MusicGenre"},
			{"Name": "Genre D", "Type": "MusicGenre"},
		},
		"lib-music-videos": {
			{"Id": "genre-a", "Name": "Genre A", "Type": "MusicGenre"},
		},
		"lib-music-b": {
			{"Id": "genre-c", "Name": "Genre C", "Type": "MusicGenre"},
			{"Id": "genre-b", "Name": "Genre B", "Type": "MusicGenre"},
			{"Name": "genre d", "Type": "MusicGenre"},
		},
	}
	return &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/UserViews":
				if got := params.Get("UserId"); got != "test-user-id" {
					return fmt.Errorf("/UserViews UserId = %q", got)
				}
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Name": "Music A", "Id": "lib-music-a", "CollectionType": "music"},
					{"Name": "Movies", "Id": "lib-movies", "CollectionType": "movies"},
					{"Name": "Music Videos", "Id": "lib-music-videos", "CollectionType": "musicvideos"},
					{"Name": "Music B", "Id": "lib-music-b", "CollectionType": "music"},
				}}, dest)
			case "/Genres":
				*genreCalls = append(*genreCalls, maps.Clone(params))
				items := genresByLibrary[params.Get("ParentId")]
				total := len(items)
				if limit, err := strconv.Atoi(params.Get("Limit")); err == nil && limit < total {
					items = items[:limit]
				}
				return jsonInto(map[string]any{"Items": items, "TotalRecordCount": total}, dest)
			default:
				*otherEndpoints = append(*otherEndpoints, endpoint)
				return nil
			}
		},
	}
}

func TestMusicGenres_MergesMusicLibraries(t *testing.T) {
	var genreCalls []url.Values
	var otherEndpoints []string
	mc := musicGenresClient(&genreCalls, &otherEndpoints)

	result := callTool(t, mc, "", "jellyfin_music", map[string]any{
		"action": "genres",
		"query":  "Genre",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(otherEndpoints) != 0 {
		t.Errorf("unexpected endpoints called: %v", otherEndpoints)
	}
	wantLibraries := []string{"lib-music-a", "lib-music-videos", "lib-music-b"}
	if len(genreCalls) != len(wantLibraries) {
		t.Fatalf("got %d /Genres calls, want %d", len(genreCalls), len(wantLibraries))
	}
	for i, want := range wantLibraries {
		p := genreCalls[i]
		if got := p.Get("ParentId"); got != want {
			t.Errorf("call %d ParentId = %q, want %q", i, got, want)
		}
		if got := p.Get("UserId"); got != "test-user-id" {
			t.Errorf("call %d UserId = %q, want test-user-id", i, got)
		}
		if got := p.Get("SearchTerm"); got != "Genre" {
			t.Errorf("call %d SearchTerm = %q, want Genre", i, got)
		}
	}
	if !strings.Contains(text, "Music genres (4)") {
		t.Errorf("expected 4 merged genres, got: %s", text)
	}
	if n := strings.Count(text, "genre-c"); n != 1 {
		t.Errorf("genre shared by Id appears %d times, want 1: %s", n, text)
	}
	if strings.Contains(text, `"genre d"`) {
		t.Errorf("Id-less genre was not de-duplicated by name: %s", text)
	}
	idxA := strings.Index(text, "Genre A")
	idxB := strings.Index(text, "Genre B")
	idxC := strings.Index(text, "Genre C")
	idxD := strings.Index(text, "Genre D")
	if idxA < 0 || idxA > idxB || idxB > idxC || idxC > idxD {
		t.Errorf("genres not sorted by name (A=%d B=%d C=%d D=%d): %s", idxA, idxB, idxC, idxD, text)
	}
}

func TestMusicGenres_LimitAppliesToMergedList(t *testing.T) {
	var genreCalls []url.Values
	var otherEndpoints []string
	mc := musicGenresClient(&genreCalls, &otherEndpoints)

	result := callTool(t, mc, "", "jellyfin_music", map[string]any{
		"action": "genres",
		"limit":  2,
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "Music genres (2)") {
		t.Errorf("expected 2 genres, got: %s", text)
	}
	if !strings.Contains(text, "Genre A") || strings.Contains(text, "Genre C") {
		t.Errorf("expected the first two genres by name, got: %s", text)
	}
	for i, p := range genreCalls {
		if p.Has("SearchTerm") {
			t.Errorf("call %d sent SearchTerm without a query", i)
		}
	}
}

func TestMusicGenres_NoMusicLibrary(t *testing.T) {
	var genreCalls int
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/UserViews" {
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Name": "Movies", "Id": "lib-movies", "CollectionType": "movies"},
					{"Name": "Shows", "Id": "lib-shows", "CollectionType": "tvshows"},
				}}, dest)
			}
			genreCalls++
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_music", map[string]any{
		"action": "genres",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Errorf("expected a non-error result, got error: %s", text)
	}
	if !strings.Contains(text, "No music or music-video library") {
		t.Errorf("expected no-music-library message, got: %s", text)
	}
	if genreCalls != 0 {
		t.Errorf("got %d genre requests, want 0", genreCalls)
	}
}

func TestPeople_Persons_SingleRequest(t *testing.T) {
	var calls []url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if endpoint != "/Persons" {
				return nil
			}
			calls = append(calls, maps.Clone(params))
			// Mirrors Jellyfin 10.11: StartIndex is ignored and the total is
			// the number of items returned.
			n, _ := strconv.Atoi(params.Get("Limit"))
			n = min(n, 250)
			items := make([]map[string]any, n)
			for i := range items {
				items[i] = map[string]any{"Id": fmt.Sprintf("person-%d", i), "Name": fmt.Sprintf("Person %d", i), "Type": "Person"}
			}
			return jsonInto(map[string]any{"Items": items, "TotalRecordCount": n}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_people", map[string]any{
		"action": "persons",
		"query":  "Person",
		"limit":  500,
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d /Persons requests, want 1", len(calls))
	}
	p := calls[0]
	if got := p.Get("Limit"); got != "500" {
		t.Errorf("Limit = %q, want 500", got)
	}
	if p.Has("StartIndex") {
		t.Errorf("StartIndex sent (%q); /Persons cannot be paged on Jellyfin 10.11", p.Get("StartIndex"))
	}
	if got := p.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if got := p.Get("SearchTerm"); got != "Person" {
		t.Errorf("SearchTerm = %q, want Person", got)
	}
	// Jellyfin 12 lists music credits among persons; the artists actions
	// list them on every version.
	if got := p.Get("ExcludePersonTypes"); got != "Artist,AlbumArtist" {
		t.Errorf("ExcludePersonTypes = %q, want Artist,AlbumArtist", got)
	}
	if !strings.Contains(text, "Found 250 persons:") {
		t.Errorf("expected 250 persons, got: %.200s", text)
	}
}

func TestPeople_Persons_ReportsTotal(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint != "/Persons" {
				return nil
			}
			return jsonInto(map[string]any{
				"Items": []map[string]any{
					{"Id": "person-1", "Name": "Person A", "Type": "Person"},
					{"Id": "person-2", "Name": "Person B", "Type": "Person"},
				},
				"TotalRecordCount": 40,
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_people", map[string]any{
		"action": "persons",
		"limit":  2,
	})

	text := resultText(t, result)
	if !strings.Contains(text, "Found 2 persons (of 40 total)") {
		t.Errorf("expected total record count in result, got: %s", text)
	}
}

func TestMetadata_Update_GetsItemByItemsRoute(t *testing.T) {
	var gotEndpoint, postEndpoint string
	var gotParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gotEndpoint = endpoint
			gotParams = maps.Clone(params)
			return jsonInto(map[string]any{"Id": "item-1", "Name": "Test Movie"}, dest)
		},
		postNoContentFunc: func(_ context.Context, endpoint string, _ url.Values, _ any) error {
			postEndpoint = endpoint
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
		"action":  "update",
		"item_id": "item-1",
		"confirm": true,
		"name":    "Test Movie Renamed",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotEndpoint != "/Items/item-1" {
		t.Errorf("GET endpoint = %q, want /Items/item-1", gotEndpoint)
	}
	if got := gotParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId = %q, want test-user-id", got)
	}
	if postEndpoint != "/Items/item-1" {
		t.Errorf("POST endpoint = %q, want /Items/item-1", postEndpoint)
	}
}

func TestMetadata_BatchUpdate_GetsItemsByItemsRoute(t *testing.T) {
	type call struct {
		endpoint string
		params   url.Values
	}
	var calls []call
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			calls = append(calls, call{endpoint, maps.Clone(params)})
			return jsonInto(map[string]any{"Name": "Test Movie"}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
		"action":   "batch_update",
		"item_ids": []string{"item-1", "item-2"},
		"name":     "Test Movie Renamed",
		"dry_run":  false,
		"confirm":  true,
	})

	text := resultText(t, result)
	if !strings.Contains(text, "Updated 2 of 2") {
		t.Errorf("expected both items updated, got: %s", text)
	}
	want := []string{"/Items/item-1", "/Items/item-2"}
	if len(calls) != len(want) {
		t.Fatalf("got %d GET calls, want %d", len(calls), len(want))
	}
	for i, c := range calls {
		if c.endpoint != want[i] {
			t.Errorf("call %d endpoint = %q, want %q", i, c.endpoint, want[i])
		}
		if got := c.params.Get("UserId"); got != "test-user-id" {
			t.Errorf("call %d UserId = %q, want test-user-id", i, got)
		}
	}
}
