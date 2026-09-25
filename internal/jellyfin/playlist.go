package jellyfin

import (
	"context"
	"fmt"
	"net/url"
)

// PlaylistEdit is the server-side work that turns a playlist's current entry
// order into a target order.
//
// Jellyfin identifies a playlist entry by its item's ID, so the only removal
// the API offers deletes every entry of an item. The only insertion available
// on every supported version is an append: Jellyfin 10.11 has no insert
// position, and the move route resolves the playlist through the caller's own
// user, which an API key does not have. An edit is therefore a delete followed
// by an append: every entry of each Remove item is deleted, then the Append
// items are added to the end in order.
type PlaylistEdit struct {
	Remove []string // item IDs whose entries are all deleted
	Append []string // item IDs appended after the delete, in order
}

// AppendsDuplicates reports whether Append adds the same item more than once.
// Jellyfin 10.11 silently collapses such an append to one entry, so the edit
// can be carried out faithfully only on Jellyfin 12 and later.
func (e PlaylistEdit) AppendsDuplicates() bool {
	seen := make(map[string]bool, len(e.Append))
	for _, id := range e.Append {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// PlanPlaylistEdit computes the edit that turns current into target, where
// both are lists of item IDs in playlist order.
//
// Deleting every entry of some items leaves the other entries in their order,
// so the edit keeps the longest start of target that deletion alone can leave
// in place, deletes every other item, and appends the rest of target. A start
// target[:s] can be kept when the entries its items have in current are
// exactly target[:s], in the same order, and none of its items appears again
// in target[s:]. Every appended item is then among the deleted ones, so it has
// no entry left when the append runs.
func PlanPlaylistEdit(current, target []string) PlaylistEdit {
	positions := make(map[string][]int, len(current))
	for i, id := range current {
		positions[id] = append(positions[id], i)
	}
	count := make(map[string]int, len(target))
	last := make(map[string]int, len(target))
	for i, id := range target {
		count[id]++
		last[id] = i
	}

	// Walk target, matching the k-th entry of each item with that item's k-th
	// entry in current. The walk stops at the first item whose entry count
	// differs between the two lists, or whose match falls before the previous
	// one; no start reaching that far can be kept. A position where every item
	// seen so far has had its last target entry is a boundary that no item
	// crosses, and the furthest such boundary is the start that is kept.
	keep, prev, reach := 0, -1, -1
	matched := make(map[string]int, len(target))
	for i, id := range target {
		k := matched[id]
		pos := positions[id]
		if k == 0 && len(pos) != count[id] {
			break
		}
		if pos[k] < prev {
			break
		}
		matched[id] = k + 1
		prev = pos[k]
		reach = max(reach, last[id])
		if reach == i {
			keep = i + 1
		}
	}

	kept := make(map[string]bool, keep)
	for _, id := range target[:keep] {
		kept[id] = true
	}
	var edit PlaylistEdit
	removed := make(map[string]bool)
	for _, id := range current {
		if !kept[id] && !removed[id] {
			removed[id] = true
			edit.Remove = append(edit.Remove, id)
		}
	}
	if keep < len(target) {
		edit.Append = append([]string(nil), target[keep:]...)
	}
	return edit
}

// DedupeTarget returns entries with every repeat of an item dropped, keeping
// each item's first entry in place.
func DedupeTarget(entries []string) []string {
	seen := make(map[string]bool, len(entries))
	out := make([]string, 0, len(entries))
	for _, id := range entries {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RemoveTarget returns entries without any entry of the given items.
func RemoveTarget(entries, remove []string) []string {
	drop := make(map[string]bool, len(remove))
	for _, id := range remove {
		drop[id] = true
	}
	out := make([]string, 0, len(entries))
	for _, id := range entries {
		if !drop[id] {
			out = append(out, id)
		}
	}
	return out
}

// MoveTarget returns entries with the first entry of item moved to newIndex,
// counted in the resulting order. An index past the end moves the entry to
// the end.
func MoveTarget(entries []string, item string, newIndex int) ([]string, error) {
	if newIndex < 0 {
		return nil, fmt.Errorf("new_index must be 0 or greater")
	}
	from := -1
	for i, id := range entries {
		if id == item {
			from = i
			break
		}
	}
	if from < 0 {
		return nil, fmt.Errorf("item %s is not in the playlist", item)
	}
	rest := make([]string, 0, len(entries))
	rest = append(rest, entries[:from]...)
	rest = append(rest, entries[from+1:]...)
	if newIndex > len(rest) {
		newIndex = len(rest)
	}
	out := make([]string, 0, len(entries))
	out = append(out, rest[:newIndex]...)
	out = append(out, item)
	out = append(out, rest[newIndex:]...)
	return out, nil
}

// MaxPlaylistEditEntries is the largest playlist an edit reads in full. An
// edit plans against a complete snapshot, so a longer playlist is refused
// rather than edited from a partial one.
const MaxPlaylistEditEntries = 10000

// playlistIDsPerRequest bounds how many item IDs one delete or append carries.
// The IDs travel in the query string, and Jellyfin's web server rejects a
// request line longer than 8 KiB, so a long list is sent in several requests.
const playlistIDsPerRequest = 100

// PlaylistEntry is one entry of a playlist as the server lists it.
type PlaylistEntry struct {
	ItemID   string // the entry's item, in the form NormalizeID produces
	Name     string
	IsFolder bool
}

// PlaylistEntryItemIDs returns the item ID of each entry, in playlist order.
func PlaylistEntryItemIDs(entries []PlaylistEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ItemID
	}
	return ids
}

func playlistItemsEndpoint(playlistID string) string {
	return fmt.Sprintf("/Playlists/%s/Items", SanitizeID(playlistID))
}

// FetchPlaylistEntries returns every entry of a playlist in order, as userID
// sees it. Jellyfin leaves out entries whose items userID cannot see. An edit
// keeps those entries, because its deletes name only the items it read, but a
// hidden entry inside the part an edit rebuilds ends up ahead of the entries
// the edit adds back.
//
// Every edit deletes entries by item ID, which matches because Jellyfin
// reports each entry's ID (PlaylistItemId) as its item's ID. A playlist whose
// entries report another ID is refused rather than edited.
func FetchPlaylistEntries(ctx context.Context, client Client, playlistID, userID string) ([]PlaylistEntry, error) {
	params := url.Values{
		"UserId":         {userID},
		"EnableImages":   {"false"},
		"EnableUserData": {"false"},
	}
	raw, total, err := FetchAllPages(ctx, client, playlistItemsEndpoint(playlistID), params, MaxPlaylistEditEntries)
	if err != nil {
		return nil, err
	}
	if total > len(raw) {
		return nil, fmt.Errorf("the playlist has %d entries, and an edit reads at most %d", total, MaxPlaylistEditEntries)
	}
	entries := make([]PlaylistEntry, 0, len(raw))
	for _, r := range raw {
		m := ToMap(r)
		entry := PlaylistEntry{
			ItemID:   NormalizeID(GetString(m, "Id")),
			Name:     GetString(m, "Name"),
			IsFolder: GetBool(m, "IsFolder"),
		}
		if NormalizeID(GetString(m, "PlaylistItemId")) != entry.ItemID {
			return nil, fmt.Errorf("the server reported an entry ID for %q that differs from its item ID, so removing entries by item ID would not match them", entry.Name)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// CheckPlaylistEdit reports why edit cannot be carried out faithfully on a
// playlist with the given entries, as userID. It also reports whether the
// edit may hide the playlist from userID once it is done: Jellyfin 12 hides a
// playlist from a user with a parental rating limit while none of its
// remaining entries passes that limit, and the entries userID cannot see are
// never listed. It reads the server version and the user's policy only for
// the edits that depend on them.
func CheckPlaylistEdit(ctx context.Context, client Client, userID string, entries []PlaylistEntry, edit PlaylistEdit) (hides bool, err error) {
	// Jellyfin expands an appended folder, such as an album, into the items
	// inside it, so a folder entry cannot be added back as itself.
	appending := make(map[string]bool, len(edit.Append))
	for _, id := range edit.Append {
		appending[id] = true
	}
	for _, e := range entries {
		if appending[e.ItemID] && e.IsFolder {
			return false, fmt.Errorf("%q is a folder entry, and adding it back would add its contents instead of the entry itself", e.Name)
		}
	}

	duplicates := edit.AppendsDuplicates()
	deletesAllVisible := len(RemoveTarget(PlaylistEntryItemIDs(entries), edit.Remove)) == 0
	if !duplicates && !deletesAllVisible {
		return false, nil
	}
	version, err := client.ServerVersion(ctx)
	if err != nil {
		return false, fmt.Errorf("the edit depends on the server version, which could not be read: %w", err)
	}
	if duplicates && !version.AtLeast(12, 0) {
		return false, fmt.Errorf("the edit adds back an item that appears more than once, and Jellyfin %s keeps only one entry of an item added twice; it needs Jellyfin 12 or later", version)
	}
	if !deletesAllVisible || !version.AtLeast(12, 0) {
		return false, nil
	}

	// The delete leaves none of the entries userID can see. If userID has a
	// rating limit, only entries above it may remain, and the playlist is then
	// hidden from userID: an append after the delete would be refused, and a
	// pure removal leaves the playlist hidden from userID. When the user cannot
	// be read, a pure removal proceeds without the hides warning.
	var user map[string]any
	if err := client.Get(ctx, "/Users/"+SanitizeID(userID), nil, &user); err != nil {
		if len(edit.Append) == 0 {
			return false, nil
		}
		return false, fmt.Errorf("the edit removes every entry before adding entries back, and user %s could not be read to check their parental rating limit: %w", userID, err)
	}
	if ToMap(user["Policy"])["MaxParentalRating"] == nil {
		return false, nil
	}
	if len(edit.Append) > 0 {
		return false, fmt.Errorf("the edit removes every entry before adding entries back, and user %s has a parental rating limit; Jellyfin %s hides such a user's playlist while all of its remaining entries are above that limit, which would refuse the entries being added back. "+
			"Set JELLYFIN_USER_ID to a user without a rating limit who can edit the playlist, or make this change in a Jellyfin client", userID, version)
	}
	return true, nil
}

// CheckPlaylistEditable confirms that userID may change the playlist. It
// sends an append with no items, which the server authorizes like any append,
// for the playlist's owner or a user it is shared with for editing, and then
// leaves the playlist unchanged.
func CheckPlaylistEditable(ctx context.Context, client Client, playlistID, userID string) error {
	return client.PostNoContent(ctx, playlistItemsEndpoint(playlistID), url.Values{"UserId": {userID}}, nil)
}

// ApplyPlaylistEdit carries out edit: it deletes every entry of the Remove
// items, then appends the Append items as userID. Both lists are sent in
// order, split across requests when long. An error can leave the playlist
// partly edited, so the caller reads it back to learn its state.
func ApplyPlaylistEdit(ctx context.Context, client Client, playlistID, userID string, edit PlaylistEdit) error {
	endpoint := playlistItemsEndpoint(playlistID)
	for _, ids := range chunkIDs(edit.Remove) {
		if err := client.Del(ctx, endpoint, url.Values{"entryIds": {JoinIDs(ids)}}); err != nil {
			return fmt.Errorf("removing entries: %w", err)
		}
	}
	if _, err := AppendPlaylistItems(ctx, client, playlistID, userID, edit.Append); err != nil {
		return fmt.Errorf("adding entries back: %w", err)
	}
	return nil
}

// AppendPlaylistItems adds items to the end of a playlist, in order, as
// userID, who must own the playlist or be allowed to edit it. It returns how
// many of the items were in requests that succeeded. A failed request may
// still have been applied by the server.
func AppendPlaylistItems(ctx context.Context, client Client, playlistID, userID string, items []string) (int, error) {
	endpoint := playlistItemsEndpoint(playlistID)
	sent := 0
	for _, ids := range chunkIDs(items) {
		params := url.Values{"ids": {JoinIDs(ids)}, "UserId": {userID}}
		if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
			return sent, err
		}
		sent += len(ids)
	}
	return sent, nil
}

// CountPlaylistEntries returns how many entries of the playlist userID can see.
func CountPlaylistEntries(ctx context.Context, client Client, playlistID, userID string) (int, error) {
	var result map[string]any
	params := url.Values{"UserId": {userID}, "Limit": {"0"}}
	if err := client.Get(ctx, playlistItemsEndpoint(playlistID), params, &result); err != nil {
		return 0, err
	}
	return GetInt(result, "TotalRecordCount"), nil
}

func chunkIDs(ids []string) [][]string {
	var chunks [][]string
	for len(ids) > 0 {
		n := min(len(ids), playlistIDsPerRequest)
		chunks = append(chunks, ids[:n])
		ids = ids[n:]
	}
	return chunks
}
