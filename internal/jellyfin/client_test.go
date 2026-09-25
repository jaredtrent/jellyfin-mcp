package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "test-key-0123456789abcdef"

func newTestClient(t *testing.T, handler http.HandlerFunc) *JellyfinClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &JellyfinClient{
		baseURL:    srv.URL,
		apiKey:     testAPIKey,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestEveryVerbSendsAuthorizationHeader(t *testing.T) {
	calls := map[string]func(c *JellyfinClient) error{
		"Get": func(c *JellyfinClient) error {
			return c.Get(context.Background(), "/System/Info", nil, nil)
		},
		"GetRaw": func(c *JellyfinClient) error {
			_, err := c.GetRaw(context.Background(), "/System/Logs/Log", nil)
			return err
		},
		"Post": func(c *JellyfinClient) error {
			return c.Post(context.Background(), "/Playlists", nil, map[string]any{"Name": "x"}, nil)
		},
		"PostNoContent": func(c *JellyfinClient) error {
			return c.PostNoContent(context.Background(), "/Library/Refresh", nil, nil)
		},
		"PostRaw": func(c *JellyfinClient) error {
			return c.PostRaw(context.Background(), "/Items/abc/Images/Primary", nil, strings.NewReader("aW1n"), 4, "image/png")
		},
		"Del": func(c *JellyfinClient) error {
			return c.Del(context.Background(), "/Items/abc", nil)
		},
		"DoRequest": func(c *JellyfinClient) error {
			_, err := c.DoRequest(context.Background(), http.MethodGet, "/Sessions", nil, nil)
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var got *http.Request
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Clone(context.Background())
				_, _ = w.Write([]byte(`{}`))
			})
			if err := call(c); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			want := `MediaBrowser Token="` + testAPIKey + `"`
			if h := got.Header.Get("Authorization"); h != want {
				t.Errorf("Authorization = %q, want %q", h, want)
			}
			for _, legacy := range []string{"X-MediaBrowser-Token", "X-Emby-Token", "X-Emby-Authorization"} {
				if h := got.Header.Get(legacy); h != "" {
					t.Errorf("legacy header %s sent: %q", legacy, h)
				}
			}
			q := got.URL.Query()
			if q.Has("api_key") || q.Has("ApiKey") {
				t.Errorf("API key sent in the query string: %s", got.URL.RawQuery)
			}
		})
	}
}

func TestAuthorizationHeaderEscapesToken(t *testing.T) {
	c := &JellyfinClient{apiKey: `a"b,c d`}
	want := `MediaBrowser Token="a%22b%2Cc+d"`
	if got := c.authorizationHeader(); got != want {
		t.Errorf("authorizationHeader() = %q, want %q", got, want)
	}
}

func TestSendContentType(t *testing.T) {
	var got http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.PostNoContent(context.Background(), "/x", nil, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if ct := got.Get("Content-Type"); ct != "application/json" {
		t.Errorf("JSON body Content-Type = %q", ct)
	}

	if err := c.PostRaw(context.Background(), "/x", nil, io.MultiReader(strings.NewReader("aW"), strings.NewReader("1n")), 4, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if ct := got.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("raw body Content-Type = %q", ct)
	}
	if cl := got.Get("Content-Length"); cl != "4" {
		t.Errorf("streamed raw body Content-Length = %q, want 4", cl)
	}

	if err := c.Del(context.Background(), "/x", nil); err != nil {
		t.Fatal(err)
	}
	if ct := got.Get("Content-Type"); ct != "" {
		t.Errorf("bodyless request Content-Type = %q, want none", ct)
	}
}

func TestSendErrorStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	})
	for name, err := range map[string]error{
		"Get":     c.Get(context.Background(), "/x", nil, nil),
		"PostRaw": c.PostRaw(context.Background(), "/x", nil, strings.NewReader("a"), 1, "image/png"),
	} {
		if err == nil || !strings.Contains(err.Error(), "API error 401") {
			t.Errorf("%s error = %v, want API error 401", name, err)
		}
	}
}

// Users on the fake server. IDs are in Jellyfin's 32-hex form.
var testUsers = []map[string]any{
	{"Name": "Viewer", "Id": "11111111111111111111111111111111", "Policy": map[string]any{"IsAdministrator": false}},
	{"Name": "Admin", "Id": "22222222222222222222222222222222", "Policy": map[string]any{"IsAdministrator": true}},
	{"Name": "Kids", "Id": "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c", "Policy": map[string]any{"IsAdministrator": false}},
}

func TestGetUserID(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		me         map[string]any // nil makes /Users/Me return 400, as it does for an API key
		users      []map[string]any
		wantID     string
		wantErr    string
		wantMeCall bool
	}{
		{
			name:       "configured ID",
			configured: "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c",
			users:      testUsers,
			wantID:     "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c",
		},
		{
			name:       "configured dashed uppercase ID",
			configured: "3C3C3C3C-3C3C-3C3C-3C3C-3C3C3C3C3C3C",
			users:      testUsers,
			wantID:     "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c",
		},
		{
			name:       "configured username, any case",
			configured: "kIdS",
			users:      testUsers,
			wantID:     "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c",
		},
		{
			name:       "configured user wins over the token's user",
			configured: "Kids",
			me:         testUsers[0],
			users:      testUsers,
			wantID:     "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c",
		},
		{
			name:       "configured value matches nothing",
			configured: "nobody",
			users:      testUsers,
			wantErr:    `JELLYFIN_USER_ID "nobody" does not match`,
		},
		{
			name:       "configured unknown ID",
			configured: "44444444444444444444444444444444",
			users:      testUsers,
			wantErr:    `JELLYFIN_USER_ID "44444444444444444444444444444444" does not match`,
		},
		{
			name:       "user token resolves to its own user",
			me:         testUsers[0],
			users:      testUsers,
			wantID:     "11111111111111111111111111111111",
			wantMeCall: true,
		},
		{
			name:       "API key falls back to the first administrator",
			users:      testUsers,
			wantID:     "22222222222222222222222222222222",
			wantMeCall: true,
		},
		{
			name:       "no administrator falls back to the first user",
			users:      []map[string]any{testUsers[0], testUsers[2]},
			wantID:     "11111111111111111111111111111111",
			wantMeCall: true,
		},
		{
			name:       "no users",
			users:      []map[string]any{},
			wantErr:    "no users found",
			wantMeCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meCalled := false
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/Users/Me":
					meCalled = true
					if tt.me == nil {
						http.Error(w, "no user", http.StatusBadRequest)
						return
					}
					_ = json.NewEncoder(w).Encode(tt.me)
				case "/Users":
					_ = json.NewEncoder(w).Encode(tt.users)
				default:
					http.NotFound(w, r)
				}
			})
			c.configuredUser = tt.configured

			id, err := c.GetUserID(context.Background())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GetUserID() error = %v, want containing %q", err, tt.wantErr)
				}
				if c.userID != "" {
					t.Errorf("a failed resolution was cached as %q", c.userID)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetUserID() error = %v", err)
			}
			if id != tt.wantID {
				t.Errorf("GetUserID() = %q, want %q", id, tt.wantID)
			}
			if meCalled != tt.wantMeCall {
				t.Errorf("/Users/Me called = %v, want %v", meCalled, tt.wantMeCall)
			}
		})
	}
}

func TestGetUserIDCachesSuccess(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/Users" {
			_ = json.NewEncoder(w).Encode(testUsers)
			return
		}
		http.Error(w, "no user", http.StatusBadRequest)
	})
	for range 3 {
		if _, err := c.GetUserID(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 { // /Users/Me and /Users, once
		t.Errorf("server calls = %d, want 2", calls)
	}
}

// pageRequest is the paging window of one request to the fake list endpoint.
type pageRequest struct{ start, limit int }

// newPagedClient serves count items, numbered by Seq from 0, from a list
// endpoint that honors StartIndex and Limit. A request whose StartIndex is
// failAt gets a 500; a negative failAt never fails. It returns the client and
// the requests it received.
func newPagedClient(t *testing.T, count, failAt int) (*JellyfinClient, *[]pageRequest) {
	t.Helper()
	var reqs []pageRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.Atoi(q.Get("StartIndex"))
		limit, _ := strconv.Atoi(q.Get("Limit"))
		reqs = append(reqs, pageRequest{start, limit})
		if start == failAt {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		items := []map[string]any{}
		for i := start; i < count && i < start+limit; i++ {
			items = append(items, map[string]any{"Id": fmt.Sprintf("item-%d", i), "Seq": i})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": count})
	})
	return c, &reqs
}

func stopAtSeq(n int) func(map[string]any) bool {
	return func(item map[string]any) bool { return GetInt(item, "Seq") >= n }
}

func TestFetchAllPagesUntil(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		maxItems  int
		stop      func(map[string]any) bool
		wantItems int
		wantTotal int
		wantReqs  []pageRequest
	}{
		{
			name:      "never stops",
			count:     450,
			stop:      func(map[string]any) bool { return false },
			wantItems: 450,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}, {200, 200}, {400, 200}},
		},
		{
			name:      "nil stop",
			count:     450,
			wantItems: 450,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}, {200, 200}, {400, 200}},
		},
		{
			name:      "stops mid-page",
			count:     450,
			stop:      stopAtSeq(250),
			wantItems: 250,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}, {200, 200}},
		},
		{
			name:      "stops on the first item of a page",
			count:     450,
			stop:      stopAtSeq(200),
			wantItems: 200,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}, {200, 200}},
		},
		{
			name:      "stops on the last item of a page",
			count:     450,
			stop:      stopAtSeq(199),
			wantItems: 199,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}},
		},
		{
			name:      "stops on the first item",
			count:     450,
			stop:      stopAtSeq(0),
			wantItems: 0,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}},
		},
		{
			name:      "maxItems caps the scan",
			count:     450,
			maxItems:  250,
			stop:      func(map[string]any) bool { return false },
			wantItems: 250,
			wantTotal: 450,
			wantReqs:  []pageRequest{{0, 200}, {200, 50}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, reqs := newPagedClient(t, tt.count, -1)
			items, total, err := FetchAllPagesUntil(context.Background(), c, "/Items", url.Values{}, tt.maxItems, tt.stop)
			if err != nil {
				t.Fatalf("FetchAllPagesUntil() error = %v", err)
			}
			if len(items) != tt.wantItems {
				t.Errorf("got %d items, want %d", len(items), tt.wantItems)
			}
			for i, raw := range items {
				if seq := GetInt(ToMap(raw), "Seq"); seq != i {
					t.Fatalf("item %d has Seq %d; items must keep the server's order", i, seq)
				}
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if !reflect.DeepEqual(*reqs, tt.wantReqs) {
				t.Errorf("requests = %v, want %v", *reqs, tt.wantReqs)
			}
		})
	}
}

// A failed page fails the whole fetch, including a page after the first, so a
// caller never mistakes a list cut short for a complete one.
func TestFetchAllPagesUntilPageError(t *testing.T) {
	tests := []struct {
		name     string
		failAt   int
		wantReqs []pageRequest
	}{
		{"first page", 0, []pageRequest{{0, 200}}},
		{"later page", DefaultPageSize, []pageRequest{{0, 200}, {200, 200}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, reqs := newPagedClient(t, 450, tt.failAt)
			items, total, err := FetchAllPagesUntil(context.Background(), c, "/Items", url.Values{}, 0, nil)
			if err == nil || !strings.Contains(err.Error(), "API error 500") {
				t.Fatalf("error = %v, want API error 500", err)
			}
			if items != nil || total != 0 {
				t.Errorf("got %d items and total %d on a page error, want none", len(items), total)
			}
			if !reflect.DeepEqual(*reqs, tt.wantReqs) {
				t.Errorf("requests = %v, want %v", *reqs, tt.wantReqs)
			}
		})
	}
}

func TestFetchAllPagesCollectsEverything(t *testing.T) {
	c, reqs := newPagedClient(t, 201, -1)
	items, total, err := FetchAllPages(context.Background(), c, "/Items", url.Values{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 201 || total != 201 {
		t.Errorf("got %d items and total %d, want 201 and 201", len(items), total)
	}
	if want := []pageRequest{{0, 200}, {200, 200}}; !reflect.DeepEqual(*reqs, want) {
		t.Errorf("requests = %v, want %v", *reqs, want)
	}
}

// A list result never carries more Items than the Limit asked for, whatever
// the server sends.
func TestGetTrimsItemsToLimit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Items":[{"Id":"1"},{"Id":"2"},{"Id":"3"}],"TotalRecordCount":3}`))
	})
	var result map[string]any
	if err := c.Get(context.Background(), "/Items", url.Values{"Limit": {"2"}}, &result); err != nil {
		t.Fatal(err)
	}
	if got := len(ToSlice(result["Items"])); got != 2 {
		t.Errorf("Items = %d, want 2", got)
	}
	result = nil
	if err := c.Get(context.Background(), "/Items", nil, &result); err != nil {
		t.Fatal(err)
	}
	if got := len(ToSlice(result["Items"])); got != 3 {
		t.Errorf("without Limit: Items = %d, want 3", got)
	}
}
