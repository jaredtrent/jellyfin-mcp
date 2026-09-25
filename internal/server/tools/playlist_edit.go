package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// playlistLocks serializes this server's changes to each playlist. A change
// reads the playlist and writes it in several requests, so two changes to one
// playlist that ran at once would each act on a state the other is replacing.
// A change keeps its playlist locked until it has read the result back, which
// it does even after its own request is cancelled.
type playlistLocks struct {
	mu    sync.Mutex
	locks map[string]*playlistLock
}

// playlistLock is one playlist's lock. It exists while a change holds or
// waits for it, so the map does not grow with every playlist ever edited.
type playlistLock struct {
	ch    chan struct{}
	users int
}

func newPlaylistLocks() *playlistLocks {
	return &playlistLocks{locks: make(map[string]*playlistLock)}
}

// lock waits until no other change to the playlist is running, or until ctx
// ends, and returns the function that releases the playlist.
func (l *playlistLocks) lock(ctx context.Context, playlistID string) (func(), error) {
	key := jf.NormalizeID(playlistID)
	l.mu.Lock()
	pl := l.locks[key]
	if pl == nil {
		pl = &playlistLock{ch: make(chan struct{}, 1)}
		l.locks[key] = pl
	}
	pl.users++
	l.mu.Unlock()
	leave := func() {
		l.mu.Lock()
		pl.users--
		if pl.users == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
	select {
	case pl.ch <- struct{}{}:
		return func() { <-pl.ch; leave() }, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}

// playlistEditEntry names one item in an edit report.
type playlistEditEntry struct {
	ItemID string `json:"item_id"`
	Name   string `json:"name,omitempty"`
}

// playlistEditRequests describes a jf.PlaylistEdit as the requests it sends,
// in the order it sends them.
type playlistEditRequests struct {
	RemoveEveryEntryOf []playlistEditEntry `json:"remove_every_entry_of,omitempty"`
	ThenAppendInOrder  []playlistEditEntry `json:"then_append_in_order,omitempty"`
}

func describeEdit(edit jf.PlaylistEdit, names map[string]string) playlistEditRequests {
	named := func(ids []string) []playlistEditEntry {
		out := make([]playlistEditEntry, 0, len(ids))
		for _, id := range ids {
			out = append(out, playlistEditEntry{ItemID: id, Name: names[id]})
		}
		return out
	}
	return playlistEditRequests{RemoveEveryEntryOf: named(edit.Remove), ThenAppendInOrder: named(edit.Append)}
}

// playlistEdit is one planned edit and what its report says about it.
type playlistEdit struct {
	current, target []string
	names           map[string]string
	requests        playlistEditRequests
	effect          string // the edit's effect, as a sentence
	report          any    // what the edit does, per item
	rebuild         string // the run of entries removed and added back, as a sentence
	hides           bool   // the edit may leave the playlist hidden from the configured user
	userID          string
}

// handlePlaylistEdit carries out the remove_items, deduplicate, and move_item
// actions. Each reads the whole playlist, computes the order it should have,
// and applies the jf.PlaylistEdit that reaches that order. The playlist is
// then read back, and the result is judged from what was read: an interrupted
// edit can leave entries removed but not yet added back, and the report then
// gives the steps that finish it.
func handlePlaylistEdit(ctx context.Context, req *mcp.CallToolRequest, client jf.Client, locks *playlistLocks, userID string, args jf.PlaylistsInput) (*mcp.CallToolResult, any, error) {
	switch {
	case args.PlaylistID == "":
		return jf.ErrResult("playlist_id is required for '%s' action.", args.Action), nil, nil
	case args.Action == "remove_items" && len(jf.SplitIDs(args.ItemIDs)) == 0:
		return jf.ErrResult("playlist_id and item_ids are required for 'remove_items' action."), nil, nil
	case args.Action == "move_item" && (args.ItemID == "" || args.NewIndex == nil):
		return jf.ErrResult("playlist_id, item_id, and new_index are required for 'move_item' action."), nil, nil
	}
	dryRun := args.DryRun != nil && *args.DryRun
	if args.Action == "deduplicate" && args.DryRun == nil {
		dryRun = true
	}

	unlock, err := locks.lock(ctx, args.PlaylistID)
	if err != nil {
		return jf.ErrResult("Stopped while waiting for another change to this playlist to finish: %v", err), nil, nil
	}
	defer unlock()

	// A call that carries the answer to the confirmation form runs again from
	// the top, on the same progress token for an older client, so it skips the
	// first step's notification to keep the reported progress increasing.
	if len(req.Params.InputResponses) == 0 {
		jf.ReportProgress(ctx, req, 0, 3, "Reading the playlist...")
	}
	entries, err := jf.FetchPlaylistEntries(ctx, client, args.PlaylistID, userID)
	if err != nil {
		return jf.ErrResult("Failed to read playlist: %v", err), nil, nil
	}
	playlist := itemName(ctx, client, args.PlaylistID, userID)
	e := playlistEdit{current: jf.PlaylistEntryItemIDs(entries), names: make(map[string]string, len(entries)), userID: userID}
	for _, entry := range entries {
		e.names[entry.ItemID] = entry.Name
	}

	var warning string
	switch args.Action {
	case "deduplicate":
		e.target = jf.DedupeTarget(e.current)
		if len(e.target) == len(e.current) {
			return jf.TextResult("No duplicate entries found in playlist."), nil, nil
		}
		e.report = duplicateReport(e.current, e.names)
		removing := len(e.current) - len(e.target)
		e.effect = fmt.Sprintf("%d duplicate %s, and the first entry of each item keeps its position.", removing, plural(removing, "entry is removed", "entries are removed"))
		var dupIDs []string
		for _, r := range e.report.([]map[string]any) {
			dupIDs = append(dupIDs, r["item_id"].(string))
		}
		warning = fmt.Sprintf("Remove %d duplicate %s of %s from playlist '%s'? The first entry of each stays in place.", removing, plural(removing, "entry", "entries"), nameList(dupIDs, e.names), playlist)

	case "remove_items":
		var removing, missing []string
		present := make(map[string]bool, len(e.current))
		for _, id := range e.current {
			present[id] = true
		}
		for _, raw := range jf.SplitIDs(args.ItemIDs) {
			id := jf.NormalizeID(raw)
			switch {
			case !present[id]:
				missing = append(missing, raw)
			case !slices.Contains(removing, id):
				removing = append(removing, id)
			}
		}
		if len(removing) == 0 {
			return jf.ErrResult("None of the item_ids are in the playlist, so nothing was removed. Use 'get' to list its entries."), nil, nil
		}
		e.target = jf.RemoveTarget(e.current, removing)
		counts := make(map[string]int, len(removing))
		for _, id := range e.current {
			counts[id]++
		}
		removed := make([]map[string]any, 0, len(removing))
		for _, id := range removing {
			removed = append(removed, map[string]any{"item_id": id, "name": e.names[id], "entries": counts[id]})
		}
		result := map[string]any{"removed": removed}
		if len(missing) > 0 {
			result["not_in_playlist"] = missing
		}
		e.report = result
		entryCount := len(e.current) - len(e.target)
		e.effect = fmt.Sprintf("Every entry of %d %s is removed (%d %s).", len(removing), plural(len(removing), "item", "items"), entryCount, plural(entryCount, "entry", "entries"))
		warning = fmt.Sprintf("Remove %s from playlist '%s'? Every entry of %s is removed (%d %s).", nameList(removing, e.names), playlist, plural(len(removing), "the item", "these items"), entryCount, plural(entryCount, "entry", "entries"))

	case "move_item":
		item := jf.NormalizeID(args.ItemID)
		e.target, err = jf.MoveTarget(e.current, item, *args.NewIndex)
		if err != nil {
			return jf.ErrResult("%v. Use 'get' to list the playlist's entries.", err), nil, nil
		}
		// MoveTarget places the entry at new_index, or last when new_index is
		// past the end.
		position := min(*args.NewIndex, len(e.current)-1)
		if slices.Equal(e.target, e.current) {
			return jf.TextResult(fmt.Sprintf("Moving the first entry of '%s' to position %d leaves the playlist unchanged.", e.names[item], position)), nil, nil
		}
		e.effect = fmt.Sprintf("The first entry of '%s' moves to position %d.", e.names[item], position)
	}

	edit := jf.PlanPlaylistEdit(e.current, e.target)
	e.hides, err = jf.CheckPlaylistEdit(ctx, client, userID, entries, edit)
	if err != nil {
		return jf.ErrResult("Cannot make this edit, so nothing was changed: %v.", err), nil, nil
	}
	e.requests = describeEdit(edit, e.names)
	if len(edit.Append) > 0 {
		e.rebuild = fmt.Sprintf("To keep the order, the last %d %s removed and added back at the end.", len(edit.Append), plural(len(edit.Append), "entry is", "entries are"))
	}
	if e.hides {
		e.effect += fmt.Sprintf(" This removes every entry user %s can see, so the playlist may disappear for them: Jellyfin 12 hides a playlist from a user with a parental rating limit while all of its remaining entries are above that limit.", userID)
	}
	if warning != "" {
		for _, extra := range []string{e.rebuild, hidesSentence(e)} {
			if extra != "" {
				warning += " " + extra
			}
		}
	}

	if dryRun {
		msg := "Dry run, no changes made.\n\n" + e.effect
		if e.report != nil {
			msg += "\n\n" + jf.FormatJSON(e.report)
		}
		if e.rebuild != "" {
			msg += "\n\n" + e.rebuild
		}
		msg += "\n\nThe edit sends these requests:\n\n" + jf.FormatJSON(e.requests)
		msg += "\n\nTo apply it, call again with dry_run=false"
		if warning != "" {
			msg += "; the user is asked to confirm"
		}
		return jf.TextResult(msg + "."), nil, nil
	}

	if warning != "" {
		if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
			return result, nil, nil
		}
	}

	// A delete with an API key is not checked against the playlist's owner,
	// so every edit confirms first that the configured user may edit it.
	if err := jf.CheckPlaylistEditable(ctx, client, args.PlaylistID, userID); err != nil {
		return jf.ErrResult("Nothing was changed: checking that user %s may edit this playlist failed (%v). "+
			"Playlist changes act as the configured user (JELLYFIN_USER_ID), who must own the playlist or have edit access to it.", userID, err), nil, nil
	}

	// Once the first write is sent, the edit and its check run to completion
	// even if the request is cancelled, because stopping between the delete
	// and the append would leave entries missing.
	writeCtx := context.WithoutCancel(ctx)
	jf.ReportProgress(ctx, req, 1, 3, "Editing the playlist...")
	applyErr := jf.ApplyPlaylistEdit(writeCtx, client, args.PlaylistID, userID, edit)

	jf.ReportProgress(ctx, req, 2, 3, "Checking the result...")
	after, readErr := jf.FetchPlaylistEntries(writeCtx, client, args.PlaylistID, userID)
	if readErr != nil {
		after, readErr = jf.FetchPlaylistEntries(writeCtx, client, args.PlaylistID, userID)
	}
	var result *mcp.CallToolResult
	if readErr != nil {
		result = e.unreadResult(applyErr, readErr)
	} else {
		result = e.checkedResult(applyErr, jf.PlaylistEntryItemIDs(after))
	}
	if result.IsError {
		log.Printf("jellyfin_playlists %s on playlist %s: %s", args.Action, args.PlaylistID, resultText(result))
	}
	return result, nil, nil
}

// hidesSentence is the confirmation's note that the edit may hide the
// playlist from the configured user.
func hidesSentence(e playlistEdit) string {
	if !e.hides {
		return ""
	}
	return fmt.Sprintf("The playlist may then disappear for user %s, whose parental rating limit hides a playlist whose remaining entries are all above it.", e.userID)
}

// finishSteps gives the edit's own requests as steps that reach its target
// from any state the edit can stop in, including an unchanged and a finished
// one: every appended item is also a removed one, so removing every entry of
// the removed items leaves the part the edit keeps, and the append completes
// it.
func (e playlistEdit) finishSteps() string {
	steps := "run remove_items with every item in remove_every_entry_of"
	if len(e.requests.ThenAppendInOrder) == 0 {
		steps += " (a reply that none of them are in the playlist means this is already done)."
	} else {
		steps += " (a reply that none of them are in the playlist is fine), then add_items with then_append_in_order in the order shown."
	}
	return steps + "\n\n" + jf.FormatJSON(e.requests)
}

// unreadResult reports an edit whose playlist could not be read back.
func (e playlistEdit) unreadResult(applyErr, readErr error) *mcp.CallToolResult {
	if applyErr == nil && e.hides && isStatus(readErr, http.StatusNotFound) {
		return jf.TextResult(e.effect + fmt.Sprintf("\n\nEvery request succeeded. The playlist can no longer be found as user %s, which is expected after removing every entry they can see.", e.userID))
	}
	var msg string
	if applyErr == nil {
		msg = fmt.Sprintf("Every request succeeded, but the playlist could not be read back to confirm the result (%v). If a later read shows another order, these steps reach the intended one from any state: ", readErr)
	} else {
		msg = fmt.Sprintf("The edit reported an error (%v), and the playlist could not be read back to check its state (%v). Once the playlist can be read, these steps finish the edit from whatever state it is in: ", applyErr, readErr)
	}
	return jf.ErrResult("%s%s", msg, e.finishSteps())
}

// checkedResult judges an edit by the playlist read back after it. Entries of
// items the edit's snapshot did not contain were added by someone else during
// the edit; they are left where they are and not counted against the result.
func (e playlistEdit) checkedResult(applyErr error, after []string) *mcp.CallToolResult {
	snapshot := make(map[string]bool, len(e.current))
	for _, id := range e.current {
		snapshot[id] = true
	}
	var own, others []string
	for _, id := range after {
		switch {
		case snapshot[id]:
			own = append(own, id)
		case !slices.Contains(others, id):
			others = append(others, id)
		}
	}
	othersNote := ""
	if len(others) > 0 {
		othersNote = fmt.Sprintf("\n\nEntries of these items were added by someone else during the edit and were left in place: %s", jf.FormatJSON(others))
	}

	switch {
	case slices.Equal(own, e.target):
		msg := e.effect
		if e.report != nil {
			msg += "\n\n" + jf.FormatJSON(e.report)
		}
		if e.rebuild != "" {
			msg += "\n\n" + e.rebuild
		}
		if applyErr != nil {
			msg += fmt.Sprintf("\n\nA request reported an error (%v), but the playlist was read back and has the intended order.", applyErr)
		}
		return jf.TextResult(msg + othersNote)

	case jf.OutcomeUnknown(applyErr):
		// Jellyfin may still apply the request that failed, so what was read
		// back need not be the playlist's final state.
		return jf.ErrResult("The edit stopped at a request whose outcome is unknown (%v). Jellyfin may still apply it, so the playlist read back afterward, with %d of its own entries where the intended result has %d, may not be its final state. "+
			"Check the playlist with 'get' after a short wait. If it does not have the intended order, these steps reach it from any state the edit can leave: %s%s",
			applyErr, len(own), len(e.target), e.finishSteps(), othersNote)

	case applyErr != nil && slices.Equal(own, e.current):
		return jf.ErrResult("Nothing was changed: %v.%s", applyErr, othersNote)
	}

	msg := "The edit did not produce the expected order"
	if applyErr != nil {
		msg = fmt.Sprintf("The edit stopped partway (%v)", applyErr)
	}
	msg += fmt.Sprintf(". The playlist now has %d of its own entries, and the intended result has %d.", len(own), len(e.target))
	remaining := describeEdit(jf.PlanPlaylistEdit(own, e.target), e.names)
	switch {
	case len(remaining.RemoveEveryEntryOf) > 0 && len(remaining.ThenAppendInOrder) > 0:
		msg += " To finish it, run remove_items with every item in remove_every_entry_of, then add_items with then_append_in_order in the order shown:"
	case len(remaining.ThenAppendInOrder) > 0:
		msg += " To finish it, run add_items with then_append_in_order in the order shown:"
	default:
		msg += " To finish it, run remove_items with every item in remove_every_entry_of:"
	}
	return jf.ErrResult("%s\n\n%s%s", msg, jf.FormatJSON(remaining), othersNote)
}

// isStatus reports whether err is a response with the given status.
func isStatus(err error, status int) bool {
	var apiErr *jf.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// duplicateReport lists each item with more than one entry, in the order of
// its first entry.
func duplicateReport(entries []string, names map[string]string) []map[string]any {
	counts := make(map[string]int, len(entries))
	for _, id := range entries {
		counts[id]++
	}
	var out []map[string]any
	for _, id := range jf.DedupeTarget(entries) {
		if counts[id] > 1 {
			out = append(out, map[string]any{
				"item_id":     id,
				"name":        names[id],
				"occurrences": counts[id],
				"removing":    counts[id] - 1,
			})
		}
	}
	return out
}

// resultText returns the text content of a result.
func resultText(result *mcp.CallToolResult) string {
	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}

// addItemsFailure reports an add_items call whose append failed after sent of
// its item IDs were in requests that succeeded. The failed request may have
// been applied anyway, so the playlist is counted again: a count that moved
// shows the add was at least partly applied, and an outcome-unknown error
// means Jellyfin may still apply it.
func addItemsFailure(ctx context.Context, client jf.Client, args jf.PlaylistsInput, userID string, before, sent int, err error) *mcp.CallToolResult {
	after, countErr := jf.CountPlaylistEntries(ctx, client, args.PlaylistID, userID)
	moved := countErr == nil && after != before
	if sent == 0 && !moved && !jf.OutcomeUnknown(err) {
		msg := fmt.Sprintf("Failed to add items: %v.", err)
		if isStatus(err, http.StatusForbidden) || isStatus(err, http.StatusNotFound) {
			msg += " Adding acts as the configured user (JELLYFIN_USER_ID), who must own the playlist or have edit access to it."
		}
		return jf.ErrResult("%s", msg)
	}
	msg := fmt.Sprintf("Adding stopped at a failed request (%v). %d of the %d item IDs were in requests that succeeded.", err, sent, len(args.ItemIDs))
	if countErr == nil {
		msg += fmt.Sprintf(" The playlist went from %d to %d entries.", before, after)
	}
	if jf.OutcomeUnknown(err) {
		msg += " The failed request's outcome is unknown, and Jellyfin may still apply it,"
	} else {
		msg += " The failed request may have been applied,"
	}
	msg += " so check the playlist with 'get' before adding the rest. These item IDs were not confirmed as added:\n\n" + jf.FormatJSON(args.ItemIDs[sent:])
	return jf.ErrResult("%s", msg)
}

// itemName returns the item's name for a message, or its
// ID when the name cannot be read.
func itemName(ctx context.Context, client jf.Client, itemID, userID string) string {
	var item map[string]any
	if userID == "" {
		userID, _ = client.GetUserID(ctx)
	}
	params := url.Values{}
	if userID != "" {
		params.Set("UserId", userID)
	}
	if err := client.Get(ctx, "/Items/"+jf.SanitizeID(itemID), params, &item); err == nil {
		if name := jf.GetString(item, "Name"); name != "" {
			return name
		}
	}
	return itemID
}

// itemNames looks up the names of ids in one request; an id that is not found
// is absent from the map, and nameList then shows the id itself.
func itemNames(ctx context.Context, client jf.Client, ids []string, userID string) map[string]string {
	names := make(map[string]string, len(ids))
	var out struct {
		Items []map[string]any `json:"Items"`
	}
	if err := client.Get(ctx, "/Items", url.Values{"Ids": {jf.JoinIDs(ids)}, "UserId": {userID}}, &out); err != nil {
		return names
	}
	for _, it := range out.Items {
		names[jf.NormalizeID(jf.GetString(it, "Id"))] = jf.GetString(it, "Name")
	}
	return names
}

// nameList names up to three items for a confirmation message, then counts
// the rest.
func nameList(ids []string, names map[string]string) string {
	quoted := make([]string, 0, len(ids))
	for i, id := range ids {
		if i == 3 {
			quoted = append(quoted, fmt.Sprintf("%d more", len(ids)-3))
			break
		}
		name := names[id]
		if name == "" {
			name = names[jf.NormalizeID(id)]
		}
		if name == "" {
			name = id
		}
		quoted = append(quoted, "'"+name+"'")
	}
	return strings.Join(quoted, ", ")
}

// plural picks the singular or plural form for n.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}
