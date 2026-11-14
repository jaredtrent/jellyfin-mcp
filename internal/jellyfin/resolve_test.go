package jellyfin

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type stubResolveClient struct {
	get       func(endpoint string, dest any) error
	getParams func(endpoint string, params url.Values, dest any) error
}

func (s stubResolveClient) Get(_ context.Context, endpoint string, params url.Values, dest any) error {
	if s.getParams != nil {
		return s.getParams(endpoint, params, dest)
	}
	if s.get != nil {
		return s.get(endpoint, dest)
	}
	return nil
}
func (stubResolveClient) GetRaw(context.Context, string, url.Values) (string, error) { return "", nil }
func (stubResolveClient) Post(context.Context, string, url.Values, any, any) error   { return nil }
func (stubResolveClient) PostNoContent(context.Context, string, url.Values, any) error {
	return nil
}
func (stubResolveClient) PostRaw(context.Context, string, url.Values, io.Reader, int64, string) error {
	return nil
}
func (stubResolveClient) Del(context.Context, string, url.Values) error { return nil }
func (stubResolveClient) DoRequest(context.Context, string, string, url.Values, any) ([]byte, error) {
	return nil, nil
}
func (stubResolveClient) GetUserID(context.Context) (string, error) { return "u1", nil }
func (stubResolveClient) ServerVersion(context.Context) (ServerVersion, error) {
	return ServerVersion{Major: 12, Minor: 1}, nil
}
func (stubResolveClient) BaseURL() string { return "http://localhost:8096" }

func fillJSON(v any, dest any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

func TestIsJellyfinID(t *testing.T) {
	for _, valid := range []string{
		"a1b2c3d4e5f60718293a4b5c6d7e8f90",
		"A1B2C3D4-E5F6-0718-293A-4B5C6D7E8F90",
	} {
		if !IsJellyfinID(valid) {
			t.Errorf("IsJellyfinID(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{
		"",
		"Movies",
		"lib-1",
		"g1b2c3d4e5f60718293a4b5c6d7e8f90",
		"a1b2-c3d4-e5f6-0718-293a-4b5c-6d7e-8f90",
		"a1b2c3d4-e5f60718-293a-4b5c-6d7e8f90",
	} {
		if IsJellyfinID(invalid) {
			t.Errorf("IsJellyfinID(%q) = true, want false", invalid)
		}
	}
}

func TestResolveLibraryID(t *testing.T) {
	calls := 0
	client := stubResolveClient{get: func(endpoint string, dest any) error {
		calls++
		return fillJSON([]map[string]any{
			{"Name": "Movies", "ItemId": "11111111111111111111111111111111"},
			{"Name": "TV Shows", "ItemId": "22222222222222222222222222222222"},
			{"Name": "Anime", "ItemId": "33333333333333333333333333333333"},
			{"Name": "anime", "ItemId": "44444444444444444444444444444444"},
		}, dest)
	}}

	// UUID bypasses HTTP entirely
	uuid := "a1b2c3d4-e5f6-0718-293a-4b5c6d7e8f90"
	got, err := ResolveLibraryID(context.Background(), client, uuid)
	if err != nil || got != uuid || calls != 0 {
		t.Fatalf("UUID resolve = (%q, %v), calls=%d", got, err, calls)
	}

	// Case-insensitive unique name
	got, err = ResolveLibraryID(context.Background(), client, "tv shows")
	if err != nil || got != "22222222222222222222222222222222" {
		t.Errorf("case-insensitive resolve = (%q, %v)", got, err)
	}

	// Exact case disambiguates case-colliding libraries ("Anime" vs "anime")
	got, err = ResolveLibraryID(context.Background(), client, "Anime")
	if err != nil || got != "33333333333333333333333333333333" {
		t.Errorf("exact-case resolve = (%q, %v)", got, err)
	}

	// Ambiguous case-insensitive ("ANIME") returns error
	if _, err := ResolveLibraryID(context.Background(), client, "ANIME"); err == nil || !strings.Contains(err.Error(), "matches multiple libraries") {
		t.Errorf("ambiguous resolve err = %v", err)
	}

	// Missing library lists available names
	if _, err := ResolveLibraryID(context.Background(), client, "Documentaries"); err == nil || !strings.Contains(err.Error(), "Movies") {
		t.Errorf("missing library err = %v", err)
	}
}

func TestResolveUser(t *testing.T) {
	const directUUID = "cccccccc111111112222222233333333"
	var seen []string
	client := stubResolveClient{get: func(endpoint string, dest any) error {
		seen = append(seen, endpoint)
		switch endpoint {
		case "/Users/" + directUUID:
			return fillJSON(map[string]any{"Id": directUUID, "Name": "DirectUser"}, dest)
		case "/Users":
			return fillJSON([]map[string]any{
				{"Id": "aaaaaaaa111111112222222233333333", "Name": "UserA"},
				{"Id": "bbbbbbbb111111112222222233333333", "Name": "UserB"},
			}, dest)
		default:
			t.Errorf("unexpected GET %s (must not send non-GUID username to /Users/{id})", endpoint)
			return &APIError{StatusCode: 400}
		}
	}}

	seen = nil
	id, name, _, err := ResolveUser(context.Background(), client, "usera")
	if err != nil || id != "aaaaaaaa111111112222222233333333" || name != "UserA" {
		t.Errorf("ResolveUser(usera) = (%q, %q, %v)", id, name, err)
	}
	if !slices.Equal(seen, []string{"/Users"}) {
		t.Errorf("ResolveUser(usera) endpoints = %v, want [/Users]", seen)
	}

	seen = nil
	id, name, _, err = ResolveUser(context.Background(), client, directUUID)
	if err != nil || id != directUUID || name != "DirectUser" {
		t.Errorf("ResolveUser(directUUID) = (%q, %q, %v)", id, name, err)
	}
	if !slices.Equal(seen, []string{"/Users/" + directUUID}) {
		t.Errorf("ResolveUser(directUUID) endpoints = %v, want [/Users/%s]", seen, directUUID)
	}

	seen = nil
	if _, _, _, err := ResolveUser(context.Background(), client, "ghost"); err == nil || !strings.Contains(err.Error(), "UserA") {
		t.Errorf("ResolveUser(ghost) err = %v", err)
	}
	if !slices.Equal(seen, []string{"/Users"}) {
		t.Errorf("ResolveUser(ghost) endpoints = %v, want [/Users]", seen)
	}

	// Minimal mock without /Users falls back to /Users/{id}.
	fallbackClient := stubResolveClient{get: func(endpoint string, dest any) error {
		if endpoint == "/Users/u-direct" {
			return fillJSON(map[string]any{"Id": "u-direct", "Name": "FallbackUser"}, dest)
		}
		return nil
	}}
	if id, name, _, err := ResolveUser(context.Background(), fallbackClient, "u-direct"); err != nil || id != "u-direct" || name != "FallbackUser" {
		t.Errorf("ResolveUser fallback = (%q, %q, %v)", id, name, err)
	}

	// Auth/server APIErrors propagate rather than silently falling back to raw strings.
	authErrClient := stubResolveClient{get: func(_ string, _ any) error {
		return &APIError{StatusCode: 401}
	}}
	if _, err := ResolveLibraryID(context.Background(), authErrClient, "Movies"); err == nil {
		t.Error("ResolveLibraryID swallowed 401 APIError")
	}
	if _, err := ResolveUserID(context.Background(), authErrClient, "UserA"); err == nil {
		t.Error("ResolveUserID swallowed 401 APIError")
	}

	ids, names, err := ResolveLibraryIDs(context.Background(), stubResolveClient{get: func(_ string, dest any) error {
		return fillJSON([]map[string]any{{"Name": "Movies", "ItemId": "lib-m"}}, dest)
	}}, []string{"movies"})
	if err != nil || !slices.Equal(ids, []string{"lib-m"}) || names["lib-m"] != "Movies" {
		t.Errorf("ResolveLibraryIDs = (%v, %v, %v)", ids, names, err)
	}
}

func TestResolveSeriesAndPlaylistID(t *testing.T) {
	calls := 0
	seriesClient := stubResolveClient{get: func(endpoint string, dest any) error {
		calls++
		if endpoint != "/Items" {
			t.Fatalf("unexpected endpoint %s", endpoint)
		}
		return fillJSON(map[string]any{
			"Items": []map[string]any{
				{"Id": "20000000000000000000000000000001", "Name": "Series Alpha", "ProductionYear": 2022},
				{"Id": "20000000000000000000000000000002", "Name": "Remake Show", "ProductionYear": 2001},
				{"Id": "20000000000000000000000000000003", "Name": "Remake Show", "ProductionYear": 2005},
			},
			"TotalRecordCount": 3,
		}, dest)
	}}

	// GUID bypasses network call.
	guid := "20000000000000000000000000000001"
	if got, err := ResolveSeriesID(context.Background(), seriesClient, "u1", guid); err != nil || got != guid || calls != 0 {
		t.Fatalf("GUID fast-path = (%q, %v), calls=%d", got, err, calls)
	}

	// Case-insensitive series name resolution.
	if got, err := ResolveSeriesID(context.Background(), seriesClient, "u1", "series alpha"); err != nil || got != guid {
		t.Errorf("ResolveSeriesID(series alpha) = (%q, %v), want %q", got, err, guid)
	}

	// Ambiguous series name lists years and IDs, and passing "Name (YYYY)" resolves cleanly.
	if _, err := ResolveSeriesID(context.Background(), seriesClient, "u1", "Remake Show"); err == nil || !strings.Contains(err.Error(), "2001") || !strings.Contains(err.Error(), "2005") {
		t.Errorf("ResolveSeriesID(Remake Show) err = %v, want disambiguation with years", err)
	}
	if got, err := ResolveSeriesID(context.Background(), seriesClient, "u1", "Remake Show (2005)"); err != nil || got != "20000000000000000000000000000003" {
		t.Errorf("ResolveSeriesID(Remake Show (2005)) = (%q, %v), want 2005 series ID", got, err)
	}
	if got, err := ResolveSeriesID(context.Background(), seriesClient, "u1", "remake show (2001)"); err != nil || got != "20000000000000000000000000000002" {
		t.Errorf("ResolveSeriesID(remake show (2001)) = (%q, %v), want 2001 series ID", got, err)
	}

	// Punctuation-sensitive SearchTerm returning 0 items falls back to full series listing.
	punctSeriesClient := stubResolveClient{getParams: func(_ string, params url.Values, dest any) error {
		if params.Get("SearchTerm") != "" {
			return fillJSON(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		}
		return fillJSON(map[string]any{
			"Items": []map[string]any{
				{"Id": "20000000000000000000000000000009", "Name": "A. & B. Story", "ProductionYear": 2024},
			},
			"TotalRecordCount": 1,
		}, dest)
	}}
	if got, err := ResolveSeriesID(context.Background(), punctSeriesClient, "u1", "A. & B. Story"); err != nil || got != "20000000000000000000000000000009" {
		t.Errorf("ResolveSeriesID fallback after empty SearchTerm = (%q, %v)", got, err)
	}

	// Collection (BoxSet) resolution: exact, case-insensitive, missing with available list.
	collectionClient := stubResolveClient{getParams: func(_ string, params url.Values, dest any) error {
		if params.Get("IncludeItemTypes") != "BoxSet" {
			t.Errorf("expected IncludeItemTypes=BoxSet, got %q", params.Get("IncludeItemTypes"))
		}
		return fillJSON(map[string]any{
			"Items": []map[string]any{
				{"Id": "50000000000000000000000000000001", "Name": "Sci-Fi Essentials"},
				{"Id": "50000000000000000000000000000002", "Name": "Classic Picks"},
			},
			"TotalRecordCount": 2,
		}, dest)
	}}
	if got, err := ResolveCollectionID(context.Background(), collectionClient, "u1", "sci-fi essentials"); err != nil || got != "50000000000000000000000000000001" {
		t.Errorf("ResolveCollectionID(sci-fi essentials) = (%q, %v)", got, err)
	}
	if _, err := ResolveCollectionID(context.Background(), collectionClient, "u1", "Horror Classics"); err == nil || !strings.Contains(err.Error(), "Sci-Fi Essentials") {
		t.Errorf("ResolveCollectionID(Horror Classics) err = %v, want available collections", err)
	}

	// Playlist resolution: exact, case-insensitive, missing with available list.
	playlistClient := stubResolveClient{get: func(_ string, dest any) error {
		return fillJSON(map[string]any{
			"Items": []map[string]any{
				{"Id": "40000000000000000000000000000001", "Name": "Late Night Synth"},
				{"Id": "40000000000000000000000000000002", "Name": "Chill"},
				{"Id": "40000000000000000000000000000003", "Name": "chill"},
			},
			"TotalRecordCount": 3,
		}, dest)
	}}
	if got, err := ResolvePlaylistID(context.Background(), playlistClient, "u1", "late night synth"); err != nil || got != "40000000000000000000000000000001" {
		t.Errorf("ResolvePlaylistID(late night synth) = (%q, %v)", got, err)
	}
	if got, err := ResolvePlaylistID(context.Background(), playlistClient, "u1", "Chill"); err != nil || got != "40000000000000000000000000000002" {
		t.Errorf("ResolvePlaylistID(Chill) = (%q, %v)", got, err)
	}
	if _, err := ResolvePlaylistID(context.Background(), playlistClient, "u1", "CHILL"); err == nil || !strings.Contains(err.Error(), "matches multiple playlists") {
		t.Errorf("ResolvePlaylistID(CHILL) err = %v", err)
	}
	if _, err := ResolvePlaylistID(context.Background(), playlistClient, "u1", "Road Trip"); err == nil || !strings.Contains(err.Error(), "Late Night Synth") {
		t.Errorf("ResolvePlaylistID(Road Trip) err = %v, want available playlists", err)
	}

	// Empty playlist list still reports not found when /Items envelope is present.
	emptyClient := stubResolveClient{get: func(_ string, dest any) error {
		return fillJSON(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
	}}
	if _, err := ResolvePlaylistID(context.Background(), emptyClient, "u1", "Anything"); err == nil || !strings.Contains(err.Error(), "no playlist matches") {
		t.Errorf("ResolvePlaylistID on empty server err = %v", err)
	}

	// Minimal unit-test mock with no Items envelope falls back to raw string.
	mockFallback := stubResolveClient{get: func(_ string, dest any) error {
		return fillJSON(map[string]any{"Id": "playlist-1", "Name": "Mock"}, dest)
	}}
	if got, err := ResolvePlaylistID(context.Background(), mockFallback, "u1", "playlist-1"); err != nil || got != "playlist-1" {
		t.Errorf("ResolvePlaylistID mock fallback = (%q, %v)", got, err)
	}

	// Auth errors propagate.
	authErrClient := stubResolveClient{get: func(_ string, _ any) error {
		return &APIError{StatusCode: 500, Body: "boom"}
	}}
	if _, err := ResolveSeriesID(context.Background(), authErrClient, "u1", "Series Alpha"); err == nil {
		t.Error("ResolveSeriesID swallowed 500 APIError")
	}
	if _, err := ResolvePlaylistID(context.Background(), authErrClient, "u1", "Synth"); err == nil {
		t.Error("ResolvePlaylistID swallowed 500 APIError")
	}
}

func TestResolveErrResult(t *testing.T) {
	res := ResolveErrResult(&APIError{StatusCode: 502, Body: "bad gateway"})
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	userRes := ResolveErrResult(context.DeadlineExceeded)
	if !userRes.IsError {
		t.Fatal("expected IsError=true on deadline")
	}
	connErr := noResponseError{err: io.ErrUnexpectedEOF}
	connErrClient := stubResolveClient{get: func(_ string, _ any) error { return connErr }}
	if _, err := ResolveLibraryID(context.Background(), connErrClient, "Movies"); err == nil {
		t.Error("ResolveLibraryID swallowed connection error")
	}
	if _, err := ResolveUserID(context.Background(), connErrClient, "UserA"); err == nil {
		t.Error("ResolveUserID swallowed connection error")
	}
	if _, err := ResolveSeriesID(context.Background(), connErrClient, "u1", "Series Alpha"); err == nil {
		t.Error("ResolveSeriesID swallowed connection error")
	}
	connRes := ResolveErrResult(connErr)
	if !connRes.IsError || !strings.Contains(text(connRes), "Jellyfin error: connection error:") {
		t.Errorf("ResolveErrResult(noResponseError) = %q, want Jellyfin error prefix", text(connRes))
	}
	_, _, _, err := ResolveUser(context.Background(), stubResolveClient{get: func(_ string, _ any) error {
		return &APIError{StatusCode: 404}
	}}, "nobody")
	formatted := ResolveErrResult(err)
	if !formatted.IsError {
		t.Fatal("expected IsError=true")
	}

	// HTML/invalid JSON syntax errors (such as proxy error pages) propagate.
	htmlErrClient := stubResolveClient{get: func(_ string, dest any) error {
		return json.Unmarshal([]byte("<html>502</html>"), dest)
	}}
	if _, err := ResolveLibraryID(context.Background(), htmlErrClient, "Movies"); err == nil {
		t.Error("ResolveLibraryID swallowed json.SyntaxError")
	}

	// Single-object minimal unit-test mocks fall back to raw IDs when slice endpoints are unstubbed.
	objMockClient := stubResolveClient{get: func(_ string, dest any) error {
		return fillJSON(map[string]any{"Id": "user-1", "Name": "User A"}, dest)
	}}
	if got, err := ResolveLibraryID(context.Background(), objMockClient, "lib-1"); err != nil || got != "lib-1" {
		t.Errorf("ResolveLibraryID single-object mock fallback = (%q, %v), want (\"lib-1\", nil)", got, err)
	}
	if got, err := ResolveUserID(context.Background(), objMockClient, "user-1"); err != nil || got != "user-1" {
		t.Errorf("ResolveUserID single-object mock fallback = (%q, %v), want (\"user-1\", nil)", got, err)
	}
}

func TestEffectiveUserID(t *testing.T) {
	calls := 0
	client := stubResolveClient{get: func(endpoint string, dest any) error {
		calls++
		if endpoint != "/Users" {
			t.Fatalf("unexpected endpoint %s", endpoint)
		}
		return fillJSON([]map[string]any{
			{"Id": "aaaaaaaa111111112222222233333333", "Name": "UserA"},
			{"Id": "bbbbbbbb111111112222222233333333", "Name": "UserB"},
		}, dest)
	}}

	// Empty and whitespace fall back to client.GetUserID ("u1") with zero /Users calls.
	for _, blank := range []string{"", "   "} {
		got, err := EffectiveUserID(context.Background(), client, blank)
		if err != nil || got != "u1" || calls != 0 {
			t.Fatalf("EffectiveUserID(%q) = (%q, %v), calls=%d", blank, got, err, calls)
		}
	}

	// 32-hex GUID returns immediately with zero /Users calls.
	guid := "cccccccc111111112222222233333333"
	got, err := EffectiveUserID(context.Background(), client, guid)
	if err != nil || got != guid || calls != 0 {
		t.Fatalf("EffectiveUserID(guid) = (%q, %v), calls=%d", got, err, calls)
	}

	// Username resolves case-insensitively via /Users.
	got, err = EffectiveUserID(context.Background(), client, "userb")
	if err != nil || got != "bbbbbbbb111111112222222233333333" || calls != 1 {
		t.Fatalf("EffectiveUserID(userb) = (%q, %v), calls=%d", got, err, calls)
	}

	// Unknown username returns an error listing available users.
	if _, err := EffectiveUserID(context.Background(), client, "unknown"); err == nil || !strings.Contains(err.Error(), "UserA") {
		t.Errorf("EffectiveUserID(unknown) err = %v", err)
	}
}

func TestResolveItemByTitle(t *testing.T) {
	client := stubResolveClient{getParams: func(endpoint string, params url.Values, dest any) error {
		if endpoint != "/Items" {
			t.Fatalf("unexpected endpoint %s", endpoint)
		}
		items := []map[string]any{
			{"Id": "f0000000000000000000000000000001", "Name": "Sample Film", "Type": "Folder"},
			{"Id": "b0000000000000000000000000000001", "Name": "Solo Movie", "Type": "BoxSet"},
			{"Id": "p0000000000000000000000000000001", "Name": "Solo Movie", "Type": "Playlist"},
			{"Id": "10000000000000000000000000000001", "Name": "Sample Film", "Type": "Movie", "ProductionYear": 1984},
			{"Id": "10000000000000000000000000000002", "Name": "Sample Film", "Type": "Movie", "ProductionYear": 2021},
			{"Id": "10000000000000000000000000000003", "Name": "Solo Movie", "Type": "Movie", "ProductionYear": 2016},
			{"Id": "10000000000000000000000000000004", "Name": "Shared Title", "Type": "Movie", "ProductionYear": 2015},
			{"Id": "20000000000000000000000000000004", "Name": "Shared Title", "Type": "Series", "ProductionYear": 2015},
		}
		if wantType := params.Get("IncludeItemTypes"); wantType != "" {
			var typed []map[string]any
			for _, it := range items {
				if strings.EqualFold(GetString(it, "Type"), wantType) {
					typed = append(typed, it)
				}
			}
			items = typed
		}
		return fillJSON(map[string]any{
			"Items":            items,
			"TotalRecordCount": len(items),
		}, dest)
	}}

	// BoxSet and Playlist sharing the same title do not collide with Movie.
	if got, err := ResolveItemByTitle(context.Background(), client, "u1", "solo movie"); err != nil || got != "10000000000000000000000000000003" {
		t.Errorf("ResolveItemByTitle(solo movie) = (%q, %v)", got, err)
	}
	if _, err := ResolveItemByTitle(context.Background(), client, "u1", "Sample Film"); err == nil || !strings.Contains(err.Error(), "1984") || !strings.Contains(err.Error(), "2021") {
		t.Errorf("ResolveItemByTitle(Sample Film) err = %v, want year disambiguation", err)
	}
	if got, err := ResolveItemByTitle(context.Background(), client, "u1", "Sample Film (2021)"); err != nil || got != "10000000000000000000000000000002" {
		t.Errorf("ResolveItemByTitle(Sample Film (2021)) = (%q, %v)", got, err)
	}

	// Same-title, same-year Movie + Series disambiguates by Type in the error and resolves cleanly with ResolveItemByTitleTyped.
	_, err := ResolveItemByTitle(context.Background(), client, "u1", "Shared Title (2015)")
	if err == nil || !strings.Contains(err.Error(), "Movie, 2015") || !strings.Contains(err.Error(), "Series, 2015") {
		t.Errorf("ResolveItemByTitle(Shared Title (2015)) err = %v, want Movie/Series disambiguation", err)
	}
	if got, err := ResolveItemByTitleTyped(context.Background(), client, "u1", "Shared Title (2015)", "Series"); err != nil || got != "20000000000000000000000000000004" {
		t.Errorf("ResolveItemByTitleTyped(Shared Title (2015), Series) = (%q, %v)", got, err)
	}
}
