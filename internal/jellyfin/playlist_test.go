package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// applyEdit mimics the Jellyfin server: the delete removes every entry of each
// listed item, and the append adds items at the end. On Jellyfin 10.11 the
// append skips items already in the playlist and repeats within the request.
func applyEdit(playlist []string, e PlaylistEdit, v10dot11 bool) []string {
	drop := make(map[string]bool)
	for _, id := range e.Remove {
		drop[id] = true
	}
	out := make([]string, 0, len(playlist)+len(e.Append))
	for _, id := range playlist {
		if !drop[id] {
			out = append(out, id)
		}
	}
	present := make(map[string]bool)
	for _, id := range out {
		present[id] = true
	}
	for _, id := range e.Append {
		if v10dot11 && present[id] {
			continue
		}
		present[id] = true
		out = append(out, id)
	}
	return out
}

func split(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func TestPlanPlaylistEdit(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		target     string
		wantRemove string
		wantAppend string
	}{
		{name: "no change", current: "a,b,c", target: "a,b,c"},
		{name: "empty playlist", current: "", target: ""},
		{name: "remove every entry of one item is a single delete", current: "a,x,b,x,c", target: "a,b,c", wantRemove: "x"},
		{name: "remove first item", current: "x,a,b", target: "a,b", wantRemove: "x"},
		{name: "remove last item", current: "a,b,x", target: "a,b", wantRemove: "x"},
		{name: "remove everything", current: "a,b", target: "", wantRemove: "a,b"},
		{name: "dedupe trailing repeat", current: "a,b,c,b", target: "a,b,c", wantRemove: "b,c", wantAppend: "b,c"},
		{name: "dedupe adjacent repeat", current: "a,b,b,c", target: "a,b,c", wantRemove: "b,c", wantAppend: "b,c"},
		{name: "dedupe repeat of the first item", current: "a,b,a", target: "a,b", wantRemove: "a,b", wantAppend: "a,b"},
		{name: "dedupe repeat of the last item", current: "a,b,c,c", target: "a,b,c", wantRemove: "c", wantAppend: "c"},
		{name: "dedupe interleaved repeats", current: "a,b,a,c,b", target: "a,b,c", wantRemove: "a,b,c", wantAppend: "a,b,c"},
		{name: "dedupe keeps an untouched prefix", current: "p,q,a,b,a", target: "p,q,a,b", wantRemove: "a,b", wantAppend: "a,b"},
		{name: "move to end", current: "a,b,c", target: "b,c,a", wantRemove: "a", wantAppend: "a"},
		{name: "move to front", current: "a,b,c", target: "c,a,b", wantRemove: "a,b", wantAppend: "a,b"},
		{name: "swap last two", current: "a,b,c,d", target: "a,b,d,c", wantRemove: "c", wantAppend: "c"},
		{name: "move later keeps the entries it passes", current: "a,b,c,d,e", target: "a,c,d,b,e", wantRemove: "b,e", wantAppend: "b,e"},
		{name: "move of a repeated item re-adds only the moved item", current: "a,b,b", target: "b,b,a", wantRemove: "a", wantAppend: "a"},
		{name: "target item missing from current", current: "a,c", target: "a,b,c", wantRemove: "c", wantAppend: "b,c"},
		{name: "target repeats an item current has once", current: "a,b", target: "a,b,b", wantRemove: "b", wantAppend: "b,b"},
		{name: "removal combined with rebuild", current: "a,x,b,c,b", target: "a,b,c", wantRemove: "x,b,c", wantAppend: "b,c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current, target := split(tt.current), split(tt.target)
			got := PlanPlaylistEdit(current, target)
			if !slices.Equal(got.Remove, split(tt.wantRemove)) {
				t.Errorf("Remove = %v, want %v", got.Remove, split(tt.wantRemove))
			}
			if !slices.Equal(got.Append, split(tt.wantAppend)) {
				t.Errorf("Append = %v, want %v", got.Append, split(tt.wantAppend))
			}
			for _, v10 := range []bool{false, true} {
				if v10 && got.AppendsDuplicates() {
					continue // refused on 10.11 before any change
				}
				if result := applyEdit(current, got, v10); !slices.Equal(result, target) {
					t.Errorf("applying %+v (10.11=%v) gives %v, want %v", got, v10, result, target)
				}
			}
		})
	}
}

// TestPlanPlaylistEditExhaustive checks every playlist of up to five entries
// over three items against every dedupe, removal, and move target.
func TestPlanPlaylistEditExhaustive(t *testing.T) {
	items := []string{"a", "b", "c"}
	var playlists [][]string
	var gen func(prefix []string)
	gen = func(prefix []string) {
		playlists = append(playlists, slices.Clone(prefix))
		if len(prefix) == 5 {
			return
		}
		for _, it := range items {
			gen(append(prefix, it))
		}
	}
	gen(nil)

	check := func(t *testing.T, current, target []string) {
		t.Helper()
		edit := PlanPlaylistEdit(current, target)
		if result := applyEdit(current, edit, false); !slices.Equal(result, target) {
			t.Fatalf("12.x: %v -> %v via %+v gives %v", current, target, edit, result)
		}
		if !edit.AppendsDuplicates() {
			if result := applyEdit(current, edit, true); !slices.Equal(result, target) {
				t.Fatalf("10.11: %v -> %v via %+v gives %v", current, target, edit, result)
			}
		}
		if slices.Equal(current, target) != (len(edit.Remove) == 0 && len(edit.Append) == 0) {
			t.Fatalf("%v -> %v gives edit %+v; an edit must be empty exactly when nothing changes", current, target, edit)
		}
	}

	for _, current := range playlists {
		check(t, current, DedupeTarget(current))
		for _, it := range items {
			check(t, current, RemoveTarget(current, []string{it}))
			for idx := 0; idx <= len(current); idx++ {
				if target, err := MoveTarget(current, it, idx); err == nil {
					check(t, current, target)
				}
			}
		}
	}
}

func TestDedupeTargetNeverAppendsDuplicates(t *testing.T) {
	for _, current := range [][]string{split("a,b,a,c,b,a"), split("a,a,a"), split("a,b,c")} {
		if edit := PlanPlaylistEdit(current, DedupeTarget(current)); edit.AppendsDuplicates() {
			t.Errorf("dedupe of %v appends duplicates: %+v", current, edit)
		}
	}
}

func TestAppendsDuplicates(t *testing.T) {
	// Moving a to the front of b,a,b keeps a and adds both entries of b back.
	edit := PlanPlaylistEdit(split("b,a,b"), split("a,b,b"))
	if !edit.AppendsDuplicates() {
		t.Errorf("edit %+v should report duplicate appends", edit)
	}
	if got := applyEdit(split("b,a,b"), edit, true); slices.Equal(got, split("a,b,b")) {
		t.Errorf("the 10.11 simulation should collapse the duplicate, got %v", got)
	}
}

// longestKeptStart returns, by the definition PlanPlaylistEdit documents, the
// longest start of target that deleting whole items from current leaves in
// place: the entries its items have in current are exactly that start, and
// none of its items appears again after it.
func longestKeptStart(current, target []string) int {
	for s := len(target); s > 0; s-- {
		items := make(map[string]bool)
		for _, id := range target[:s] {
			items[id] = true
		}
		crosses := false
		for _, id := range target[s:] {
			crosses = crosses || items[id]
		}
		var left []string
		for _, id := range current {
			if items[id] {
				left = append(left, id)
			}
		}
		if !crosses && slices.Equal(left, target[:s]) {
			return s
		}
	}
	return 0
}

// TestPlanPlaylistEditKeepsTheLongestStart checks the planner against the
// definition on every pair of playlists of up to five entries over three
// items, including targets that no action produces, which recovery plans for.
func TestPlanPlaylistEditKeepsTheLongestStart(t *testing.T) {
	var playlists [][]string
	var gen func(prefix []string)
	gen = func(prefix []string) {
		playlists = append(playlists, slices.Clone(prefix))
		if len(prefix) == 5 {
			return
		}
		for _, it := range []string{"a", "b", "c"} {
			gen(append(prefix, it))
		}
	}
	gen(nil)
	for _, current := range playlists {
		for _, target := range playlists {
			edit := PlanPlaylistEdit(current, target)
			if result := applyEdit(current, edit, false); !slices.Equal(result, target) {
				t.Fatalf("%v -> %v via %+v gives %v", current, target, edit, result)
			}
			if want := len(target) - longestKeptStart(current, target); len(edit.Append) != want {
				t.Fatalf("%v -> %v appends %v, want the last %d entries", current, target, edit.Append, want)
			}
		}
	}
}

func TestDedupeTarget(t *testing.T) {
	if got := DedupeTarget(split("a,b,a,c,b")); !slices.Equal(got, split("a,b,c")) {
		t.Errorf("DedupeTarget = %v", got)
	}
}

func TestRemoveTarget(t *testing.T) {
	if got := RemoveTarget(split("a,x,b,x"), []string{"x", "missing"}); !slices.Equal(got, split("a,b")) {
		t.Errorf("RemoveTarget = %v", got)
	}
}

func TestMoveTarget(t *testing.T) {
	tests := []struct {
		entries string
		item    string
		index   int
		want    string
		wantErr bool
	}{
		{entries: "a,b,c", item: "a", index: 2, want: "b,c,a"},
		{entries: "a,b,c", item: "c", index: 0, want: "c,a,b"},
		{entries: "a,b,c", item: "b", index: 1, want: "a,b,c"},
		{entries: "a,b,c", item: "a", index: 99, want: "b,c,a"},
		{entries: "a,b,a", item: "a", index: 2, want: "b,a,a"},
		{entries: "a,b,c", item: "z", index: 0, wantErr: true},
		{entries: "a,b,c", item: "a", index: -1, wantErr: true},
	}
	for _, tt := range tests {
		got, err := MoveTarget(split(tt.entries), tt.item, tt.index)
		if (err != nil) != tt.wantErr {
			t.Errorf("MoveTarget(%s, %s, %d) error = %v", tt.entries, tt.item, tt.index, err)
			continue
		}
		if !tt.wantErr && !slices.Equal(got, split(tt.want)) {
			t.Errorf("MoveTarget(%s, %s, %d) = %v, want %s", tt.entries, tt.item, tt.index, got, tt.want)
		}
	}
}

// playlistEntries builds entries for item IDs given as a comma list.
func playlistEntries(ids string) []PlaylistEntry {
	var out []PlaylistEntry
	for _, id := range split(ids) {
		out = append(out, PlaylistEntry{ItemID: id, Name: "Test Track " + id})
	}
	return out
}

// newCheckClient serves the server version and user-1's policy, whose
// MaxParentalRating is ratingLimit (nil for none), and counts the requests.
// An empty version makes the version request fail, and noUser makes the user
// request fail.
func newCheckClient(t *testing.T, version string, ratingLimit any, noUser bool) (*JellyfinClient, *int) {
	t.Helper()
	requests := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.URL.Path == "/System/Info/Public" && version != "":
			_ = json.NewEncoder(w).Encode(map[string]any{"Version": version})
		case r.URL.Path == "/Users/user-1" && !noUser:
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "user-1", "Policy": map[string]any{"MaxParentalRating": ratingLimit}})
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	})
	return c, &requests
}

func TestCheckPlaylistEdit(t *testing.T) {
	folder := playlistEntries("a,b,c")
	folder[2].IsFolder = true
	rebuildAll := PlaylistEdit{Remove: []string{"a", "b"}, Append: []string{"a", "b"}}
	removeAll := PlaylistEdit{Remove: []string{"a", "b"}}

	tests := []struct {
		name        string
		entries     []PlaylistEntry
		edit        PlaylistEdit
		version     string // empty makes the version request fail
		ratingLimit any
		noUser      bool // the user request fails
		wantErr     string
		wantHides   bool
		noRequests  bool
	}{
		{name: "pure delete", entries: playlistEntries("a,b,c"), edit: PlaylistEdit{Remove: []string{"b"}}, noRequests: true},
		{name: "rebuild that keeps a visible entry", entries: playlistEntries("a,b,c"), edit: PlaylistEdit{Remove: []string{"b", "c"}, Append: []string{"c", "b"}}, ratingLimit: 10, noRequests: true},
		{name: "folder added back", entries: folder, edit: PlaylistEdit{Remove: []string{"c"}, Append: []string{"c"}}, wantErr: "folder entry", noRequests: true},
		{name: "folder only removed", entries: folder, edit: PlaylistEdit{Remove: []string{"c"}}, noRequests: true},
		{name: "folder kept while others are rebuilt", entries: folder, edit: PlaylistEdit{Remove: []string{"a"}, Append: []string{"a"}}, noRequests: true},
		{name: "duplicate append on 12", entries: playlistEntries("b,a,b"), edit: PlaylistEdit{Remove: []string{"b"}, Append: []string{"b", "b"}}, version: "12.1.0"},
		{name: "duplicate append on 10.11", entries: playlistEntries("b,a,b"), edit: PlaylistEdit{Remove: []string{"b"}, Append: []string{"b", "b"}}, version: "10.11.11", wantErr: "Jellyfin 10.11.11 keeps only one entry"},
		{name: "duplicate append, version unknown", entries: playlistEntries("b,a,b"), edit: PlaylistEdit{Remove: []string{"b"}, Append: []string{"b", "b"}}, wantErr: "could not be read"},
		{name: "full rebuild with a rating limit on 12", entries: playlistEntries("a,b,a"), edit: rebuildAll, version: "12.1.0", ratingLimit: 10, wantErr: "parental rating limit"},
		{name: "full rebuild, rating limit unreadable", entries: playlistEntries("a,b,a"), edit: rebuildAll, version: "12.1.0", noUser: true, wantErr: "could not be read to check their parental rating limit"},
		{name: "full rebuild without a rating limit on 12", entries: playlistEntries("a,b,a"), edit: rebuildAll, version: "12.1.0"},
		{name: "full rebuild with a rating limit on 10.11", entries: playlistEntries("a,b,a"), edit: rebuildAll, version: "10.11.11", ratingLimit: 10},
		{name: "removing everything with a rating limit on 12", entries: playlistEntries("a,b"), edit: removeAll, version: "12.1.0", ratingLimit: 10, wantHides: true},
		{name: "removing everything, rating limit unreadable", entries: playlistEntries("a,b"), edit: removeAll, version: "12.1.0", noUser: true},
		{name: "removing everything without a rating limit on 12", entries: playlistEntries("a,b"), edit: removeAll, version: "12.1.0"},
		{name: "removing everything with a rating limit on 10.11", entries: playlistEntries("a,b"), edit: removeAll, version: "10.11.11", ratingLimit: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, requests := newCheckClient(t, tt.version, tt.ratingLimit, tt.noUser)
			hides, err := CheckPlaylistEdit(context.Background(), c, "user-1", tt.entries, tt.edit)
			if tt.noRequests && *requests != 0 {
				t.Errorf("sent %d requests for an edit that depends on no server fact", *requests)
			}
			if hides != tt.wantHides {
				t.Errorf("hides = %v, want %v", hides, tt.wantHides)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("CheckPlaylistEdit() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("CheckPlaylistEdit() error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestOutcomeUnknown(t *testing.T) {
	statuses := map[int]bool{
		http.StatusBadGateway:          true,
		http.StatusGatewayTimeout:      true,
		http.StatusInternalServerError: false,
		http.StatusForbidden:           false,
		http.StatusNotFound:            false,
		http.StatusServiceUnavailable:  false,
	}
	for status, want := range statuses {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failed", status)
		})
		err := c.PostNoContent(context.Background(), "/Playlists/playlist-1/Items", nil, nil)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("API error %d", status)) {
			t.Fatalf("status %d: error = %v", status, err)
		}
		if got := OutcomeUnknown(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("OutcomeUnknown(status %d) = %v, want %v", status, got, want)
		}
	}

	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {})
	c.baseURL = "http://127.0.0.1:1" // nothing listens here
	err := c.PostNoContent(context.Background(), "/Playlists/playlist-1/Items", nil, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "connection error: ") || !OutcomeUnknown(err) {
		t.Errorf("a request without a response gives %v, want an outcome-unknown connection error", err)
	}
	if OutcomeUnknown(errors.New("API error 504: text only")) {
		t.Error("an error that is not from the client must not be classified")
	}
}

// playlistRequest is one request the fake playlist endpoint received.
type playlistRequest struct {
	method string
	query  url.Values
	line   int // length of the request line: method, target, and protocol
}

// newPlaylistClient records every request to /Playlists/playlist-1/Items and
// fails the request numbered failAt (counting from 0); a negative failAt never
// fails.
func newPlaylistClient(t *testing.T, failAt int) (*JellyfinClient, *[]playlistRequest) {
	t.Helper()
	var reqs []playlistRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Playlists/playlist-1/Items" {
			http.NotFound(w, r)
			return
		}
		line := len(r.Method) + 1 + len(r.URL.RequestURI()) + 1 + len(r.Proto)
		reqs = append(reqs, playlistRequest{method: r.Method, query: r.URL.Query(), line: line})
		if len(reqs)-1 == failAt {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return c, &reqs
}

func testItemIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s%031x", prefix, i)
	}
	return ids
}

func TestApplyPlaylistEditSplitsLongLists(t *testing.T) {
	edit := PlaylistEdit{Remove: testItemIDs("a", 250), Append: testItemIDs("b", 150)}
	c, reqs := newPlaylistClient(t, -1)
	if err := ApplyPlaylistEdit(context.Background(), c, "playlist-1", "user-1", edit); err != nil {
		t.Fatal(err)
	}

	var gotRemove, gotAppend []string
	var methods []string
	for _, r := range *reqs {
		methods = append(methods, r.method)
		if r.line > 8<<10 {
			t.Errorf("%s request line is %d bytes, over the server's 8 KiB limit", r.method, r.line)
		}
		switch r.method {
		case http.MethodDelete:
			if len(r.query) != 1 {
				t.Errorf("DELETE query = %v, want only entryIds", r.query)
			}
			gotRemove = append(gotRemove, strings.Split(r.query.Get("entryIds"), ",")...)
		case http.MethodPost:
			if r.query.Get("UserId") != "user-1" || len(r.query) != 2 {
				t.Errorf("POST query = %v, want ids and UserId=user-1", r.query)
			}
			gotAppend = append(gotAppend, strings.Split(r.query.Get("ids"), ",")...)
		}
	}
	if want := []string{"DELETE", "DELETE", "DELETE", "POST", "POST"}; !slices.Equal(methods, want) {
		t.Errorf("requests = %v, want %v", methods, want)
	}
	if !slices.Equal(gotRemove, edit.Remove) {
		t.Errorf("deleted %d IDs, want the %d Remove IDs in order", len(gotRemove), len(edit.Remove))
	}
	if !slices.Equal(gotAppend, edit.Append) {
		t.Errorf("appended %d IDs, want the %d Append IDs in order", len(gotAppend), len(edit.Append))
	}
}

func TestApplyPlaylistEditStopsAtTheFirstFailure(t *testing.T) {
	edit := PlaylistEdit{Remove: testItemIDs("a", 150), Append: testItemIDs("b", 10)}
	c, reqs := newPlaylistClient(t, 1)
	err := ApplyPlaylistEdit(context.Background(), c, "playlist-1", "user-1", edit)
	if err == nil || !strings.Contains(err.Error(), "removing entries") {
		t.Fatalf("error = %v, want a removal error", err)
	}
	if len(*reqs) != 2 {
		t.Errorf("sent %d requests, want 2: nothing after the failed delete", len(*reqs))
	}
}

func TestCheckPlaylistEditableSendsAnEmptyAppend(t *testing.T) {
	c, reqs := newPlaylistClient(t, -1)
	if err := CheckPlaylistEditable(context.Background(), c, "playlist-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodPost {
		t.Fatalf("requests = %+v, want one POST", *reqs)
	}
	if q := (*reqs)[0].query; len(q) != 1 || q.Get("UserId") != "user-1" {
		t.Errorf("query = %v, want only UserId=user-1 and no ids", q)
	}
}

func TestFetchPlaylistEntries(t *testing.T) {
	var got url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Items": []map[string]any{
				{"Id": "0123456789ABCDEF0123456789ABCDEF", "PlaylistItemId": "0123456789abcdef0123456789abcdef", "Name": "Test Track A"},
				{"Id": "00000000000000000000000000000001", "PlaylistItemId": "00000000000000000000000000000001", "Name": "Test Album", "IsFolder": true},
			},
			"TotalRecordCount": 2,
		})
	})
	entries, err := FetchPlaylistEntries(context.Background(), c, "playlist-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []PlaylistEntry{
		{ItemID: "0123456789abcdef0123456789abcdef", Name: "Test Track A"},
		{ItemID: "00000000000000000000000000000001", Name: "Test Album", IsFolder: true},
	}
	if !slices.Equal(entries, want) {
		t.Errorf("entries = %+v, want %+v", entries, want)
	}
	if got.Get("UserId") != "user-1" || got.Get("EnableImages") != "false" || got.Get("EnableUserData") != "false" {
		t.Errorf("query = %v", got)
	}
}

// Removal matches entries by item ID, so a playlist whose entries report a
// different ID cannot be edited.
func TestFetchPlaylistEntriesRefusesEntryIDsThatAreNotItemIDs(t *testing.T) {
	for name, entryID := range map[string]any{"different": "fedcba9876543210fedcba9876543210", "missing": nil} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Items":            []map[string]any{{"Id": "0123456789abcdef0123456789abcdef", "PlaylistItemId": entryID, "Name": "Test Track A"}},
					"TotalRecordCount": 1,
				})
			})
			_, err := FetchPlaylistEntries(context.Background(), c, "playlist-1", "user-1")
			if err == nil || !strings.Contains(err.Error(), "differs from its item ID") {
				t.Errorf("error = %v, want a refusal", err)
			}
		})
	}
}

func TestAppendPlaylistItemsReportsWhatWasSent(t *testing.T) {
	c, reqs := newPlaylistClient(t, 1)
	sent, err := AppendPlaylistItems(context.Background(), c, "playlist-1", "user-1", testItemIDs("b", 250))
	if err == nil {
		t.Fatal("expected the second request to fail")
	}
	if sent != 100 || len(*reqs) != 2 {
		t.Errorf("sent = %d after %d requests, want 100 after 2", sent, len(*reqs))
	}
}

func TestFetchPlaylistEntriesRefusesAnOversizedPlaylist(t *testing.T) {
	total := MaxPlaylistEditEntries + 1
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("Limit"))
		items := []map[string]any{}
		for i := start; i < total && i < start+limit; i++ {
			id := fmt.Sprintf("%032x", i)
			items = append(items, map[string]any{"Id": id, "PlaylistItemId": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": total})
	})
	_, err := FetchPlaylistEntries(context.Background(), c, "playlist-1", "user-1")
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("has %d entries", total)) {
		t.Errorf("error = %v, want a refusal naming the entry count", err)
	}
}
