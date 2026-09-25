package tools_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

const testPlaylistEndpoint = "/Playlists/playlist-1/Items"

// fakeEntry is one entry of a fakePlaylist.
type fakeEntry struct {
	id         string // item ID as the server writes it
	name       string
	folder     bool
	badEntryID bool // the entry reports an entry ID other than its item ID
}

// fakeRequest is one write request a fakePlaylist received.
type fakeRequest struct {
	method string
	params url.Values
}

// fakePlaylist behaves like Jellyfin's playlist items endpoint. A delete
// removes every entry whose ID equals one of entryIds exactly, in the
// lower-case form the server writes. An append adds the items to the end,
// skipping items already present and repeats on Jellyfin 10.11, and is
// allowed only for the owner. GET /Users/test-user-id reports ratingLimit as
// the user's MaxParentalRating. It is safe for concurrent tool calls.
type fakePlaylist struct {
	t           *testing.T
	names       map[string]string // names of items that can be appended
	version     string            // server version; empty means 12.1.0
	owner       string            // user allowed to append
	ratingLimit any

	failDelete     int  // the DELETE with this number, counting from 1, fails
	failAppend     int  // the non-empty append with this number, counting from 1, fails
	failStatus     int  // the status a failing request returns; zero means 500
	commitThenFail bool // a failing request is applied before it reports its error
	failReads      int  // this many reads after the first write fail
	unreadable     bool // every read of the playlist fails
	// hideWhenEmpty makes reads fail with 404 while no entries are left. The
	// playlist then also holds entries above the user's rating limit, which
	// reads never list, and Jellyfin 12 hides it from the user once those are
	// all that remain.
	hideWhenEmpty bool
	skipOnAdd     string // an item the server silently skips when it is appended
	// onProbe runs after each permission check, and onDelete after each delete
	// is applied. Both run without the fake's lock, so they may block while
	// another call uses the playlist.
	onProbe  func()
	onDelete func()
	verified chan struct{} // closed by the first read after a write

	mu       sync.Mutex
	entries  []fakeEntry
	probeCtx context.Context // the context of the latest permission check
	writes   []fakeRequest
	reads    int // reads of the playlist's entries
	deletes  int
	appends  int
}

func newFakePlaylist(t *testing.T, ids string) *fakePlaylist {
	p := &fakePlaylist{t: t, owner: "test-user-id", names: map[string]string{}, verified: make(chan struct{})}
	for _, id := range strings.Split(ids, ",") {
		p.names[id] = "Test Track " + strings.ToUpper(id[:1])
		p.entries = append(p.entries, fakeEntry{id: id, name: p.names[id]})
	}
	return p
}

func (p *fakePlaylist) ids() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.entries))
	for i, e := range p.entries {
		out[i] = e.id
	}
	return out
}

// add appends entries of the items, as another client would.
func (p *fakePlaylist) add(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range ids {
		p.entries = append(p.entries, fakeEntry{id: id, name: "Test Track " + strings.ToUpper(id[:1])})
	}
}

// drop removes every entry of the items, as another client would.
func (p *fakePlaylist) drop(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = slices.DeleteFunc(p.entries, func(e fakeEntry) bool { return slices.Contains(ids, e.id) })
}

// probed returns the context of the latest permission check.
func (p *fakePlaylist) probed() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probeCtx
}

func (p *fakePlaylist) readCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

// failure is the error a failing request returns.
func (p *fakePlaylist) failure(body string) error {
	status := p.failStatus
	if status == 0 {
		status = http.StatusInternalServerError
	}
	return &jf.APIError{StatusCode: status, Body: body}
}

func (p *fakePlaylist) get(ctx context.Context, endpoint string, params url.Values, dest any) error {
	if err := ctx.Err(); err != nil {
		return err // as an HTTP request made with a cancelled context fails
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if endpoint == "/Users/test-user-id" {
		return jsonInto(map[string]any{"Id": "test-user-id", "Policy": map[string]any{"MaxParentalRating": p.ratingLimit}}, dest)
	}
	if endpoint != testPlaylistEndpoint {
		return fmt.Errorf("unexpected GET %s", endpoint)
	}
	if params.Get("UserId") != "test-user-id" {
		p.t.Errorf("GET UserId = %q, want test-user-id", params.Get("UserId"))
	}
	p.reads++
	if p.unreadable {
		return &jf.APIError{StatusCode: http.StatusServiceUnavailable, Body: "unavailable"}
	}
	if len(p.writes) > 0 {
		select {
		case <-p.verified:
		default:
			defer close(p.verified)
		}
		if p.failReads > 0 {
			p.failReads--
			return &jf.APIError{StatusCode: http.StatusServiceUnavailable, Body: "unavailable"}
		}
	}
	if p.hideWhenEmpty && len(p.entries) == 0 {
		return &jf.APIError{StatusCode: http.StatusNotFound, Body: "Not Found"}
	}
	start, _ := strconv.Atoi(params.Get("StartIndex"))
	limit := len(p.entries)
	if params.Has("Limit") {
		limit, _ = strconv.Atoi(params.Get("Limit"))
	}
	items := []map[string]any{}
	for i := start; i < len(p.entries) && i < start+limit; i++ {
		e := p.entries[i]
		item := map[string]any{"Id": e.id, "PlaylistItemId": e.id, "Name": e.name, "IsFolder": e.folder}
		if e.badEntryID {
			item["PlaylistItemId"] = "entry-" + e.id
		}
		items = append(items, item)
	}
	return jsonInto(map[string]any{"Items": items, "TotalRecordCount": len(p.entries)}, dest)
}

func (p *fakePlaylist) del(ctx context.Context, endpoint string, params url.Values) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	applied, err := p.applyDelete(endpoint, params)
	if applied && p.onDelete != nil {
		p.onDelete()
	}
	return err
}

// applyDelete carries out a delete and reports whether it was applied.
func (p *fakePlaylist) applyDelete(endpoint string, params url.Values) (applied bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, fakeRequest{"DELETE", params})
	if endpoint != testPlaylistEndpoint {
		return false, fmt.Errorf("unexpected DELETE %s", endpoint)
	}
	p.deletes++
	failing := p.deletes == p.failDelete
	if failing && !p.commitThenFail {
		return false, p.failure("delete failed")
	}
	drop := strings.Split(params.Get("entryIds"), ",")
	kept := p.entries[:0:0]
	for _, e := range p.entries {
		if !slices.Contains(drop, e.id) {
			kept = append(kept, e)
		}
	}
	p.entries = kept
	if failing {
		return true, p.failure("delete failed")
	}
	return true, nil
}

func (p *fakePlaylist) post(ctx context.Context, endpoint string, params url.Values, _ any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	probe, err := p.applyPost(ctx, endpoint, params)
	if probe && p.onProbe != nil {
		p.onProbe()
	}
	return err
}

// applyPost carries out an append, or records the empty append that checks
// permission, and reports whether it was that check.
func (p *fakePlaylist) applyPost(ctx context.Context, endpoint string, params url.Values) (probe bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, fakeRequest{"POST", params})
	if endpoint != testPlaylistEndpoint {
		return false, fmt.Errorf("unexpected POST %s", endpoint)
	}
	if params.Get("UserId") != p.owner {
		return false, &jf.APIError{StatusCode: http.StatusForbidden, Body: "Forbidden"}
	}
	if params.Get("ids") == "" {
		p.probeCtx = ctx
		return true, nil
	}
	p.appends++
	failing := p.appends == p.failAppend
	if failing && !p.commitThenFail {
		return false, p.failure("append failed")
	}
	legacy := p.version != "" && !strings.HasPrefix(p.version, "12.")
	present := map[string]bool{}
	for _, e := range p.entries {
		present[e.id] = true
	}
	for _, id := range strings.Split(params.Get("ids"), ",") {
		if id == p.skipOnAdd || (legacy && present[id]) {
			continue
		}
		present[id] = true
		p.entries = append(p.entries, fakeEntry{id: id, name: p.names[id]})
	}
	if failing {
		return false, p.failure("append failed")
	}
	return false, nil
}

func (p *fakePlaylist) client() *mockClient {
	return &mockClient{serverVersion: p.version, getFunc: p.get, delFunc: p.del, postNoContentFunc: p.post}
}

// methods returns the write requests' methods, marking the empty append that
// checks permission as PROBE.
func (p *fakePlaylist) methods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, w := range p.writes {
		if w.method == "POST" && w.params.Get("ids") == "" {
			out = append(out, "PROBE")
		} else {
			out = append(out, w.method)
		}
	}
	return out
}

func callPlaylists(t *testing.T, p *fakePlaylist, args map[string]any) (string, bool) {
	t.Helper()
	args["playlist_id"] = "playlist-1"
	result := callTool(t, p.client(), "", "jellyfin_playlists", args)
	return resultText(t, result), result.IsError
}

func assertContains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in result, got: %s", want, text)
		}
	}
}

// assertInOrder fails unless the quoted item IDs appear in text in order,
// after the first occurrence of key.
func assertInOrder(t *testing.T, text, key string, ids ...string) {
	t.Helper()
	at := strings.Index(text, key)
	if at < 0 {
		t.Fatalf("no %s in result: %s", key, text)
	}
	rest := text[at:]
	for _, id := range ids {
		i := strings.Index(rest, `"`+id+`"`)
		if i < 0 {
			t.Fatalf("%s does not list %s in order: %s", key, id, text[at:])
		}
		rest = rest[i+1:]
	}
}

// waitFor fails the test unless ch is closed within five seconds.
func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestPlaylistEdit_DeduplicatePreviewsByDefault(t *testing.T) {
	p := newFakePlaylist(t, "a,b,a,c,b")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(p.writes) != 0 {
		t.Errorf("a dry run sent %v", p.methods())
	}
	assertContains(t, text, "Dry run", "2 duplicate entries are removed", `"occurrences": 2`, "the last 3 entries are removed and added back", "the user is asked to confirm")
	if strings.Index(text, "remove_every_entry_of") > strings.Index(text, "then_append_in_order") {
		t.Errorf("the requests must be listed in the order they are sent: %s", text)
	}
}

func TestPlaylistEdit_DeduplicateKeepsFirstEntries(t *testing.T) {
	for _, version := range []string{"12.1.0", "10.11.11"} {
		t.Run(version, func(t *testing.T) {
			p := newFakePlaylist(t, "p,a,b,a,c,b")
			p.version = version
			text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
			if isErr {
				t.Fatalf("unexpected error: %s", text)
			}
			if got, want := p.ids(), []string{"p", "a", "b", "c"}; !slices.Equal(got, want) {
				t.Errorf("playlist = %v, want %v", got, want)
			}
			if got, want := p.methods(), []string{"PROBE", "DELETE", "POST"}; !slices.Equal(got, want) {
				t.Errorf("writes = %v, want %v", got, want)
			}
			// The repeat of a follows a's first entry, so the rebuild starts there.
			if got := p.writes[2].params; got.Get("ids") != "a,b,c" || got.Get("UserId") != "test-user-id" {
				t.Errorf("append = %v, want ids=a,b,c as test-user-id", got)
			}
			assertContains(t, text, "2 duplicate entries are removed", "the last 3 entries are removed and added back")
		})
	}
}

func TestPlaylistEdit_DeduplicateNeedsConfirmation(t *testing.T) {
	p := newFakePlaylist(t, "p,a,b,a")
	text, _ := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false})
	if !strings.Contains(text, "CONFIRMATION REQUIRED") || len(p.writes) != 0 {
		t.Errorf("expected a confirmation request and no writes, got %v: %s", p.methods(), text)
	}
	// The warning a person confirms names the entries that are rebuilt.
	assertContains(t, text, "Remove 1 duplicate entry of 'Test Track A' from playlist", "the last 2 entries are removed and added back")
}

// A client that supports forms confirms through one. The edit is planned
// again from the playlist as it stands once the person answers, so a change
// another client makes while the form is open is not undone, and the progress
// reported across the form keeps increasing.
func TestPlaylistEdit_ConfirmationFormReplansAfterTheAnswer(t *testing.T) {
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			p := newFakePlaylist(t, "a,b,a,c")
			var asked []string
			var (
				mu       sync.Mutex
				progress []float64
			)
			lastStep := make(chan struct{})
			opts := &mcp.ClientOptions{
				ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					asked = append(asked, req.Params.Message)
					p.drop("c")
					return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
				},
				// Notifications are handled in order, but possibly after the
				// reply to the call has arrived.
				ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
					mu.Lock()
					defer mu.Unlock()
					progress = append(progress, req.Params.Progress)
					if req.Params.Progress == 2 {
						close(lastStep)
					}
				},
			}
			cs := newClientSession(t, p.client(), "", opts, &mcp.ClientSessionOptions{ProtocolVersion: version})
			params := &mcp.CallToolParams{Name: "jellyfin_playlists", Arguments: map[string]any{
				"action": "deduplicate", "playlist_id": "playlist-1", "dry_run": false,
			}}
			params.SetProgressToken("edit-1")
			result, err := cs.CallTool(t.Context(), params)
			if err != nil {
				t.Fatal(err)
			}
			if text := resultText(t, result); result.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			if len(asked) != 1 || !strings.Contains(asked[0], "Remove 1 duplicate entry of 'Test Track A'") {
				t.Errorf("forms shown = %q, want one naming the duplicate", asked)
			}
			if got, want := p.ids(), []string{"a", "b"}; !slices.Equal(got, want) {
				t.Errorf("playlist = %v, want %v", got, want)
			}
			waitFor(t, lastStep, "the last progress step")
			mu.Lock()
			defer mu.Unlock()
			if want := []float64{0, 1, 2}; !slices.Equal(progress, want) {
				t.Errorf("progress = %v, want %v", progress, want)
			}
		})
	}
}

func TestPlaylistEdit_NoDuplicates(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c")
	text, _ := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !strings.Contains(text, "No duplicate entries") || len(p.writes) != 0 {
		t.Errorf("expected no change, got %v: %s", p.methods(), text)
	}
}

func TestPlaylistEdit_RemoveItemsMatchesIDsInAnyForm(t *testing.T) {
	const itemA = "0123456789abcdef0123456789abcdef"
	p := newFakePlaylist(t, itemA+",b,"+itemA+",c")
	text, isErr := callPlaylists(t, p, map[string]any{
		"action":   "remove_items",
		"item_ids": []string{"01234567-89AB-CDEF-0123-456789ABCDEF", "not-in-playlist"},
		"confirm":  true,
	})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"b", "c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	if got, want := p.methods(), []string{"PROBE", "DELETE"}; !slices.Equal(got, want) {
		t.Errorf("writes = %v, want the permission check and a single delete", got)
	}
	if got := p.writes[1].params.Get("entryIds"); got != itemA {
		t.Errorf("entryIds = %q, want the ID in the server's form %q", got, itemA)
	}
	assertContains(t, text, "Every entry of 1 item is removed (2 entries)", `"not_in_playlist"`, "not-in-playlist")
}

func TestPlaylistEdit_RemoveItemsCountsEachItemOnce(t *testing.T) {
	p := newFakePlaylist(t, "a,b,a,c,a")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "remove_items", "item_ids": []string{"a", "b", "a"}, "confirm": true})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	assertContains(t, text, "Every entry of 2 items is removed (4 entries)", `"entries": 3`, `"entries": 1`)
	if n := strings.Count(text, `"item_id": "a"`); n != 1 {
		t.Errorf("a is reported %d times, want once: %s", n, text)
	}
}

func TestPlaylistEdit_RemoveItemsNotInPlaylist(t *testing.T) {
	p := newFakePlaylist(t, "a,b")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "remove_items", "item_ids": []string{"z"}, "confirm": true})
	if !isErr || !strings.Contains(text, "None of the item_ids are in the playlist") || len(p.writes) != 0 {
		t.Errorf("expected an error and no writes, got %v: %s", p.methods(), text)
	}
}

func TestPlaylistEdit_RemoveItemsNeedsConfirmation(t *testing.T) {
	p := newFakePlaylist(t, "a,b")
	text, _ := callPlaylists(t, p, map[string]any{"action": "remove_items", "item_ids": []string{"a"}})
	if !strings.Contains(text, "CONFIRMATION REQUIRED") || len(p.writes) != 0 {
		t.Errorf("expected a confirmation request and no writes, got %v: %s", p.methods(), text)
	}
}

// An API-key delete is not checked against the playlist's owner, so every
// edit checks edit access first, removals included.
func TestPlaylistEdit_RefusedBeforeAnyChangeWithoutEditAccess(t *testing.T) {
	tests := map[string]map[string]any{
		"remove_items": {"action": "remove_items", "item_ids": []string{"a"}, "confirm": true},
		"deduplicate":  {"action": "deduplicate", "dry_run": false, "confirm": true},
		"move_item":    {"action": "move_item", "item_id": "b", "new_index": 0},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			p := newFakePlaylist(t, "a,b,a")
			p.owner = "another-user"
			text, isErr := callPlaylists(t, p, args)
			if !isErr || !strings.Contains(text, "Nothing was changed") || !strings.Contains(text, "JELLYFIN_USER_ID") {
				t.Errorf("expected a permission error, got: %s", text)
			}
			if got, want := p.methods(), []string{"PROBE"}; !slices.Equal(got, want) {
				t.Errorf("writes = %v, want only the permission check", got)
			}
			if got, want := p.ids(), []string{"a", "b", "a"}; !slices.Equal(got, want) {
				t.Errorf("playlist = %v, want it unchanged", got)
			}
		})
	}
}

func TestPlaylistEdit_MoveItem(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,d")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "d", "new_index": 1})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"a", "d", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	if got, want := p.methods(), []string{"PROBE", "DELETE", "POST"}; !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	// The moved entry stays, and only the entries it passed are rebuilt.
	if got := p.writes[1].params.Get("entryIds"); got != "b,c" {
		t.Errorf("entryIds = %q, want b,c", got)
	}
	assertContains(t, text, "moves to position 1", "the last 2 entries are removed and added back")
}

func TestPlaylistEdit_MoveItemMatchesIDsInAnyForm(t *testing.T) {
	const itemA = "0123456789abcdef0123456789abcdef"
	p := newFakePlaylist(t, itemA+",b,c")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "01234567-89AB-CDEF-0123-456789ABCDEF", "new_index": 2})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"b", "c", itemA}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	assertContains(t, text, "'Test Track 0' moves to position 2")
}

func TestPlaylistEdit_MoveItemToTheEndRebuildsOnlyThatItem(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,d")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 3})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"b", "c", "d", "a"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	if got := p.writes[1].params.Get("entryIds"); got != "a" {
		t.Errorf("entryIds = %q, want only a", got)
	}
}

func TestPlaylistEdit_MoveItemPreview(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 99, "dry_run": true})
	if isErr || len(p.writes) != 0 {
		t.Fatalf("expected a preview without writes, got %v: %s", p.methods(), text)
	}
	if !strings.Contains(text, "moves to position 2") || strings.Contains(text, "asked to confirm") {
		t.Errorf("unexpected preview: %s", text)
	}
}

// With a later entry of the same item, the moved entry's position is the index
// asked for, not the item's first index afterwards.
func TestPlaylistEdit_MoveItemReportsTheMovedEntrysPosition(t *testing.T) {
	p := newFakePlaylist(t, "a,b,a")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 2})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"b", "a", "a"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	assertContains(t, text, "moves to position 2")
}

func TestPlaylistEdit_MoveItemErrors(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"not in playlist", map[string]any{"item_id": "z", "new_index": 0}, "not in the playlist"},
		{"negative index", map[string]any{"item_id": "a", "new_index": -1}, "0 or greater"},
		{"already there", map[string]any{"item_id": "b", "new_index": 1}, "to position 1 leaves the playlist unchanged"},
		{"missing index", map[string]any{"item_id": "a"}, "new_index are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakePlaylist(t, "a,b,c")
			tt.args["action"] = "move_item"
			text, _ := callPlaylists(t, p, tt.args)
			if !strings.Contains(text, tt.want) || len(p.writes) != 0 {
				t.Errorf("expected %q and no writes, got %v: %s", tt.want, p.methods(), text)
			}
		})
	}
}

// Moving a to the front of b,a,b adds both entries of b back, which Jellyfin
// 10.11 would collapse into one.
func TestPlaylistEdit_DuplicateAddBackNeedsJellyfin12(t *testing.T) {
	p := newFakePlaylist(t, "b,a,b")
	p.version = "10.11.11"
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 0})
	if !isErr || !strings.Contains(text, "needs Jellyfin 12 or later") || len(p.writes) != 0 {
		t.Fatalf("expected a refusal without writes on 10.11, got %v: %s", p.methods(), text)
	}

	p = newFakePlaylist(t, "b,a,b")
	text, isErr = callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 0})
	if isErr {
		t.Fatalf("unexpected error on 12.1: %s", text)
	}
	if got, want := p.ids(), []string{"a", "b", "b"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
}

// Moving a to the end of a,b,b adds back only a, which Jellyfin 10.11 handles.
func TestPlaylistEdit_MoveBesideARepeatedItemOn1011(t *testing.T) {
	p := newFakePlaylist(t, "a,b,b")
	p.version = "10.11.11"
	text, isErr := callPlaylists(t, p, map[string]any{"action": "move_item", "item_id": "a", "new_index": 2})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"b", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
}

// Jellyfin 12 hides a playlist from a user with a rating limit when none of
// its remaining entries passes the limit, so an edit that removes every
// visible entry before adding entries back is refused for such a user.
func TestPlaylistEdit_FullRebuildWithARatingLimit(t *testing.T) {
	tests := []struct {
		version     string
		ratingLimit any
		wantRefusal bool
	}{
		{"12.1.0", 13, true},
		{"12.1.0", nil, false},
		{"10.11.11", 13, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s limit %v", tt.version, tt.ratingLimit), func(t *testing.T) {
			p := newFakePlaylist(t, "a,b,a")
			p.version, p.ratingLimit = tt.version, tt.ratingLimit
			text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
			if tt.wantRefusal {
				if !isErr || !strings.Contains(text, "parental rating limit") || len(p.writes) != 0 {
					t.Errorf("expected a refusal without writes, got %v: %s", p.methods(), text)
				}
				return
			}
			if isErr {
				t.Fatalf("unexpected error: %s", text)
			}
			if got, want := p.ids(), []string{"a", "b"}; !slices.Equal(got, want) {
				t.Errorf("playlist = %v, want %v", got, want)
			}
		})
	}
}

// A removal that leaves a user with a rating limit no entry they can see is
// carried out, and the playlist then disappearing for them is its expected
// result rather than a failure.
func TestPlaylistEdit_RemovingEveryVisibleEntryWithARatingLimit(t *testing.T) {
	p := newFakePlaylist(t, "a,b")
	p.ratingLimit, p.hideWhenEmpty = 13, true
	args := map[string]any{"action": "remove_items", "item_ids": []string{"a", "b"}}
	text, _ := callPlaylists(t, p, args)
	assertContains(t, text, "CONFIRMATION REQUIRED", "The playlist may then disappear for user test-user-id")

	args["confirm"] = true
	text, isErr := callPlaylists(t, p, args)
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got := p.ids(); len(got) != 0 {
		t.Errorf("playlist = %v, want every visible entry removed", got)
	}
	assertContains(t, text, "the playlist may disappear for them", "can no longer be found as user test-user-id")
}

func TestPlaylistEdit_ReportsHowToFinishAFailedAppend(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failAppend = 1
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr {
		t.Fatalf("expected an error, got: %s", text)
	}
	if got := p.ids(); len(got) != 0 {
		t.Fatalf("playlist = %v, want every entry removed by the delete", got)
	}
	assertContains(t, text, "stopped partway", "append failed", "now has 0 of its own entries", "intended result has 3", "run add_items")
	if strings.Contains(text, "remove_items") {
		t.Errorf("no removal is left to do, so none should be asked for: %s", text)
	}
	assertInOrder(t, text, "then_append_in_order", "a", "b", "c")
}

func TestPlaylistEdit_NothingChangedWhenTheFirstDeleteFails(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failDelete = 1
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr || !strings.HasPrefix(text, "Nothing was changed") || strings.Contains(text, "remove_items") {
		t.Errorf("expected a plain failure without steps, got: %s", text)
	}
}

// An entry another client adds during a failed edit does not count as a change
// the edit made.
func TestPlaylistEdit_NothingChangedDespiteAnotherClientsAddition(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failDelete = 1
	p.onProbe = func() { p.add("z") }
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr || !strings.HasPrefix(text, "Nothing was changed") || strings.Contains(text, "remove_items") {
		t.Errorf("expected a plain failure without steps, got: %s", text)
	}
	assertContains(t, text, "added by someone else during the edit", `"z"`)
}

// Jellyfin may still apply a request whose outcome is unknown, so the report
// never says that nothing changed, and its steps work from any state.
func TestPlaylistEdit_OutcomeUnknown(t *testing.T) {
	tests := map[string]func(p *fakePlaylist){
		"delete": func(p *fakePlaylist) { p.failDelete = 1 },
		"append": func(p *fakePlaylist) { p.failAppend = 1 },
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			p := newFakePlaylist(t, "a,b,c,a")
			p.failStatus = http.StatusGatewayTimeout
			fail(p)
			text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
			if !isErr {
				t.Fatalf("expected an error, got: %s", text)
			}
			if strings.Contains(text, "Nothing was changed") {
				t.Errorf("an unknown outcome must not be reported as no change: %s", text)
			}
			assertContains(t, text, "outcome is unknown", "API error 504", "Check the playlist with 'get' after a short wait",
				"run remove_items with every item in remove_every_entry_of", "then add_items with then_append_in_order")
			assertInOrder(t, text, "remove_every_entry_of", "a", "b", "c")
			assertInOrder(t, text, "then_append_in_order", "a", "b", "c")
		})
	}
}

// The steps that finish a partial edit name only the kinds of request left.
func TestPlaylistEdit_FinishStepsNameTheRemainingRequests(t *testing.T) {
	items := testIDs(150)

	t.Run("removals and appends", func(t *testing.T) {
		// Dropping the repeat of item0 deletes both of its entries, so the
		// whole playlist is rebuilt. The first delete removes item0 to item99,
		// and the second fails.
		p := newFakePlaylist(t, strings.Join(items, ",")+",item0")
		p.failDelete = 2
		text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
		if !isErr {
			t.Fatalf("expected an error, got: %s", text)
		}
		assertContains(t, text, "stopped partway", "now has 50 of its own entries", "intended result has 150",
			"run remove_items with every item in remove_every_entry_of, then add_items with then_append_in_order")
		assertInOrder(t, text, "remove_every_entry_of", "item100", "item149")
		assertInOrder(t, text, "then_append_in_order", "item0", "item149")
	})

	t.Run("removals only", func(t *testing.T) {
		p := newFakePlaylist(t, "k,"+strings.Join(items, ","))
		p.failDelete = 2
		text, isErr := callPlaylists(t, p, map[string]any{"action": "remove_items", "item_ids": items, "confirm": true})
		if !isErr {
			t.Fatalf("expected an error, got: %s", text)
		}
		assertContains(t, text, "stopped partway", "now has 51 of its own entries", "intended result has 1",
			"To finish it, run remove_items with every item in remove_every_entry_of:")
		assertInOrder(t, text, "remove_every_entry_of", "item100", "item149")
		if strings.Contains(text, "add_items") {
			t.Errorf("nothing is left to add, so no add should be asked for: %s", text)
		}
	})

	t.Run("removals only, not read back", func(t *testing.T) {
		p := newFakePlaylist(t, "a,b")
		p.failReads = 2
		text, isErr := callPlaylists(t, p, map[string]any{"action": "remove_items", "item_ids": []string{"a"}, "confirm": true})
		if !isErr {
			t.Fatalf("expected an error, got: %s", text)
		}
		assertContains(t, text, "could not be read back", "means this is already done")
		if strings.Contains(text, "add_items") {
			t.Errorf("the edit adds nothing, so no add should be asked for: %s", text)
		}
	})
}

// A request that the server applied but whose response was lost still leaves
// the intended order, which the read-back confirms.
func TestPlaylistEdit_SucceedsWhenALostResponseWasApplied(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failAppend, p.commitThenFail = 1, true
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if isErr {
		t.Fatalf("expected success, got: %s", text)
	}
	if got, want := p.ids(), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	assertContains(t, text, "A request reported an error", "has the intended order")
}

func TestPlaylistEdit_ReportsAnUnexpectedResult(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,b")
	p.skipOnAdd = "c"
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr {
		t.Fatalf("expected an error, got: %s", text)
	}
	assertContains(t, text, "did not produce the expected order", "now has 2 of its own entries", "intended result has 3")
	assertInOrder(t, text, "then_append_in_order", "c")
}

func TestPlaylistEdit_ReadBackFailureGivesStepsThatWorkFromAnyState(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failAppend, p.failReads = 1, 2
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr {
		t.Fatalf("expected an error, got: %s", text)
	}
	assertContains(t, text, "could not be read back", "append failed", "run remove_items with every item in remove_every_entry_of", "then add_items")
	assertInOrder(t, text, "remove_every_entry_of", "a", "b", "c")
	assertInOrder(t, text, "then_append_in_order", "a", "b", "c")
}

func TestPlaylistEdit_RetriesAFailedReadBack(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,a")
	p.failReads = 1
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if isErr {
		t.Fatalf("expected the second read to confirm the edit, got: %s", text)
	}
	assertContains(t, text, "1 duplicate entry is removed")
}

func TestPlaylistEdit_LeavesOtherClientsAdditionsInPlace(t *testing.T) {
	p := newFakePlaylist(t, "p,a,b,a")
	p.onDelete = func() { p.add("z") }
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"p", "z", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	assertContains(t, text, "added by someone else during the edit", `"z"`)
}

// The steps that finish a partial edit leave out entries another client added
// during it, and the report lists each such item once.
func TestPlaylistEdit_FinishStepsLeaveOutOtherClientsEntries(t *testing.T) {
	p := newFakePlaylist(t, "p,a,b,a")
	p.failAppend = 1
	p.onDelete = func() { p.add("z", "z") }
	text, isErr := callPlaylists(t, p, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr {
		t.Fatalf("expected an error, got: %s", text)
	}
	assertContains(t, text, "stopped partway", "now has 1 of its own entries", "intended result has 3", "run add_items", "added by someone else during the edit")
	if strings.Contains(text, "remove_every_entry_of") {
		t.Errorf("another client's entries must not be removed: %s", text)
	}
	assertInOrder(t, text, "then_append_in_order", "a", "b")
	if n := strings.Count(text, `"z"`); n != 1 {
		t.Errorf("z is listed %d times, want once: %s", n, text)
	}
}

// Stopping between the delete and the append would leave entries missing, so
// an edit that has started runs to completion when its request is cancelled.
func TestPlaylistEdit_FinishesAfterTheRequestIsCancelled(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,d")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p.onDelete = func() {
		cancel()
		select {
		case <-p.probed().Done():
		case <-time.After(5 * time.Second):
			t.Error("the cancellation did not reach the tool call")
		}
	}
	cs := newTestSession(t, p.client(), "")
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "jellyfin_playlists", Arguments: map[string]any{
		"action": "move_item", "playlist_id": "playlist-1", "item_id": "d", "new_index": 0,
	}})

	waitFor(t, p.verified, "the edit to be read back after the cancellation")
	if got, want := p.ids(), []string{"d", "a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want the finished edit %v", got, want)
	}
}

// A change to a playlist waits for an earlier change to it to finish, and a
// cancelled change keeps the playlist until it has read back its result.
func TestPlaylistEdit_WaitsForAnEarlierChangeToFinish(t *testing.T) {
	p := newFakePlaylist(t, "a,b,c,d")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	p.onDelete = func() {
		// The first change is cancelled and then held inside its delete.
		once.Do(func() {
			cancel()
			select {
			case <-p.probed().Done():
			case <-time.After(5 * time.Second):
				t.Error("the cancellation did not reach the tool call")
			}
			close(stopped)
			<-release
		})
	}
	mc := p.client()
	started := make(chan struct{})
	var calls atomic.Int32
	mc.getUserIDFunc = func(context.Context) (string, error) {
		if calls.Add(1) == 2 {
			close(started)
		}
		return "test-user-id", nil
	}
	cs := newTestSession(t, mc, "")
	move := func(ctx context.Context, item string) (*mcp.CallToolResult, error) {
		return cs.CallTool(ctx, &mcp.CallToolParams{Name: "jellyfin_playlists", Arguments: map[string]any{
			"action": "move_item", "playlist_id": "playlist-1", "item_id": item, "new_index": 0,
		}})
	}

	go func() { _, _ = move(ctx, "d") }()
	waitFor(t, stopped, "the first change to stop inside its delete")
	type outcome struct {
		result *mcp.CallToolResult
		err    error
	}
	second := make(chan outcome, 1)
	go func() {
		result, err := move(t.Context(), "c")
		second <- outcome{result, err}
	}()
	waitFor(t, started, "the second change to start")
	time.Sleep(100 * time.Millisecond)
	if n := p.readCount(); n != 1 {
		t.Errorf("the playlist was read %d times while the first change held it, want only the first change's own read", n)
	}
	close(release)

	var o outcome
	select {
	case o = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("the second change did not finish")
	}
	if o.err != nil {
		t.Fatal(o.err)
	}
	if text := resultText(t, o.result); o.result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	// The second change read the first one's result, not the playlist the
	// first one started from.
	if got, want := p.ids(), []string{"c", "d", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want both moves applied in turn %v", got, want)
	}
}

func TestPlaylistEdit_RefusesEntriesItCannotRebuild(t *testing.T) {
	folder := newFakePlaylist(t, "a,b,a")
	folder.entries[1].folder = true
	text, isErr := callPlaylists(t, folder, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr || !strings.Contains(text, "is a folder entry") || len(folder.writes) != 0 {
		t.Errorf("expected a refusal without writes, got %v: %s", folder.methods(), text)
	}

	mismatched := newFakePlaylist(t, "a,b,a")
	mismatched.entries[1].badEntryID = true
	text, isErr = callPlaylists(t, mismatched, map[string]any{"action": "deduplicate", "dry_run": false, "confirm": true})
	if !isErr || !strings.Contains(text, "differs from its item ID") || len(mismatched.writes) != 0 {
		t.Errorf("expected a refusal without writes, got %v: %s", mismatched.methods(), text)
	}
}

func TestPlaylists_AddItemsActsAsTheUserAndCountsEntries(t *testing.T) {
	p := newFakePlaylist(t, "a,b")
	p.version = "10.11.11"
	p.names["c"] = "Test Track C"
	text, isErr := callPlaylists(t, p, map[string]any{"action": "add_items", "item_ids": []string{"c", "a"}})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.ids(), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("playlist = %v, want %v", got, want)
	}
	if len(p.writes) != 1 || p.writes[0].params.Get("UserId") != "test-user-id" || p.writes[0].params.Get("ids") != "c,a" {
		t.Errorf("writes = %+v, want one append of c,a as test-user-id", p.writes)
	}
	assertContains(t, text, "Added 1 entry", "now has 3", "2 item IDs were sent")
}

// The first request of an add can fail after the server applied it, so the
// playlist is counted again, and only a refusal suggests a permission problem.
func TestPlaylists_AddItemsReportsAFailedFirstRequest(t *testing.T) {
	tests := []struct {
		name string
		fail func(p *fakePlaylist)
		want []string
		hint bool
	}{
		{"refused", func(p *fakePlaylist) { p.owner = "another-user" }, []string{"Failed to add items: API error 403"}, true},
		{"failed", func(p *fakePlaylist) { p.failAppend = 1 }, []string{"Failed to add items: API error 500"}, false},
		{"applied, then failed", func(p *fakePlaylist) { p.failAppend, p.commitThenFail = 1, true },
			[]string{"went from 1 to 2 entries", "The failed request may have been applied", "check the playlist with 'get'"}, false},
		{"outcome unknown", func(p *fakePlaylist) { p.failAppend, p.failStatus = 1, http.StatusGatewayTimeout },
			[]string{"went from 1 to 1 entries", "outcome is unknown, and Jellyfin may still apply it", "check the playlist with 'get'"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakePlaylist(t, "a")
			tt.fail(p)
			text, isErr := callPlaylists(t, p, map[string]any{"action": "add_items", "item_ids": []string{"b"}})
			if !isErr {
				t.Fatalf("expected an error, got: %s", text)
			}
			assertContains(t, text, tt.want...)
			if hint := strings.Contains(text, "JELLYFIN_USER_ID"); hint != tt.hint {
				t.Errorf("permission hint = %v, want %v: %s", hint, tt.hint, text)
			}
		})
	}
}

func TestPlaylists_AddItemsNeedsTheCountBefore(t *testing.T) {
	p := newFakePlaylist(t, "a")
	p.unreadable = true
	text, isErr := callPlaylists(t, p, map[string]any{"action": "add_items", "item_ids": []string{"b"}})
	if !isErr || !strings.Contains(text, "Failed to read playlist") || len(p.writes) != 0 {
		t.Errorf("expected an error and no writes, got %v: %s", p.methods(), text)
	}
}

// testIDs returns n item IDs in the form the server writes, which item ID
// normalization leaves unchanged.
func testIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("item%d", i)
	}
	return ids
}

func TestPlaylists_AddItemsSplitsLongLists(t *testing.T) {
	p := newFakePlaylist(t, "a")
	text, isErr := callPlaylists(t, p, map[string]any{"action": "add_items", "item_ids": testIDs(150)})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	if got, want := p.methods(), []string{"POST", "POST"}; !slices.Equal(got, want) {
		t.Errorf("writes = %v, want two appends", got)
	}
	assertContains(t, text, "Added 150 entries", "now has 151")
	if strings.Contains(text, "item IDs were sent") {
		t.Errorf("an add of exactly the IDs sent needs no explanation: %s", text)
	}
}

func TestPlaylists_AddItemsReportsAPartialAdd(t *testing.T) {
	p := newFakePlaylist(t, "a")
	ids := testIDs(250)
	p.failAppend = 2
	text, isErr := callPlaylists(t, p, map[string]any{"action": "add_items", "item_ids": ids})
	if !isErr {
		t.Fatalf("expected an error, got: %s", text)
	}
	assertContains(t, text, "100 of the 250 item IDs were in requests that succeeded", "went from 1 to 101 entries", "check the playlist with 'get'", `"item100"`, `"item249"`)
	if strings.Contains(text, "must own the playlist") || strings.Contains(text, `"item99"`) {
		t.Errorf("a partial add must not blame permissions or list added IDs: %s", text)
	}
}

func TestPlaylists_GetReportsTheTotal(t *testing.T) {
	p := newFakePlaylist(t, strings.Join(testIDs(1200), ","))
	text, isErr := callPlaylists(t, p, map[string]any{"action": "get"})
	if isErr || !strings.Contains(text, "showing the first 1000 of 1200") {
		t.Errorf("expected the truncated count, got: %.200s", text)
	}
}
