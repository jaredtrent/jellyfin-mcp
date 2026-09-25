package jellyfin

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ActivityQuery selects activity log entries. Its filters mean the same on
// every supported Jellyfin version. Jellyfin 12 applies all but UserID itself;
// on Jellyfin 10.11, whose endpoint takes only a minimum date and whether an
// entry has a user, or when the version is unknown, QueryActivity applies them
// to the most recent entries. The user filter is always applied here.
type ActivityQuery struct {
	MinDate, MaxDate time.Time // inclusive bounds; zero means unbounded
	Type             string    // entries whose type contains this, ignoring case
	ItemID           string    // entries about this item
	UserID           string    // entries about this user, in any ID form
	Severities       []string  // entries of any of these levels, from ActivitySeverities; empty means all
	SortBy           string    // one of ActivitySortFields; empty sorts by date
	Ascending        bool      // sort order; the default is descending
	Limit            int       // the most entries to return; must be positive
	// Lookback is the most entries to examine when filters are applied here;
	// zero means ActivityLogLookback.
	Lookback int
}

// ActivitySeverities are the levels an activity log entry can have, in
// Jellyfin's LogLevel order: Trace to Critical, then None.
var ActivitySeverities = []string{"Trace", "Debug", "Information", "Warning", "Error", "Critical", "None"}

// ActivitySortFields are the fields activity log entries can be sorted by, as
// Jellyfin names them.
var ActivitySortFields = []string{"DateCreated", "Name", "Type", "LogSeverity"}

// ActivityResult is what QueryActivity found.
type ActivityResult struct {
	Entries []map[string]any // raw entries in the requested order
	// Capped reports that filters were applied here to only the most recent
	// entries, the query's Lookback, so older matches may be missing.
	Capped bool
}

// QueryActivity returns the activity log entries q selects. The user filter
// is applied here on every version, because no version filters by user ID.
// Whenever a filter or the sort is applied here, the entries examined are the
// most recent ones, newest first, so the window means the same on every
// version.
func QueryActivity(ctx context.Context, client Client, q ActivityQuery) (ActivityResult, error) {
	v, err := client.ServerVersion(ctx)
	serverFilters := err == nil && v.AtLeast(12, 0)
	sortBy := cmp.Or(q.SortBy, "DateCreated")
	newestFirst := sortBy == "DateCreated" && !q.Ascending
	local := q.UserID != "" || (!serverFilters && (!q.MaxDate.IsZero() || q.Type != "" || q.ItemID != "" || len(q.Severities) > 0 || !newestFirst)) || len(q.Severities) > 1

	params := url.Values{}
	if !q.MinDate.IsZero() {
		params.Set("MinDate", activityTime(q.MinDate))
	}
	if q.UserID != "" {
		// Leaves out entries without a user, such as tasks and plugin
		// updates, so the window examined holds more of the user's entries.
		params.Set("HasUserId", "true")
	}
	if serverFilters {
		if !q.MaxDate.IsZero() {
			params.Set("MaxDate", activityTime(q.MaxDate))
		}
		if q.Type != "" {
			params.Set("Type", q.Type)
		}
		if q.ItemID != "" {
			params.Set("ItemId", q.ItemID)
		}
		if len(q.Severities) == 1 {
			// The endpoint takes one level; several are applied here.
			params.Set("Severity", q.Severities[0])
		}
		if !local && !newestFirst {
			// Each sort field is sent with its own order: Jellyfin fails a
			// request with more orders than fields. Ties go newest first.
			params.Add("SortBy", sortBy)
			params.Add("SortOrder", sortOrder(q.Ascending))
			if sortBy != "DateCreated" {
				params.Add("SortBy", "DateCreated")
				params.Add("SortOrder", "Descending")
			}
		}
	}

	if !local {
		raw, _, err := FetchAllPages(ctx, client, "/System/ActivityLog/Entries", params, q.Limit)
		if err != nil {
			return ActivityResult{}, err
		}
		return ActivityResult{Entries: toMaps(raw)}, nil
	}

	// The entries arrive newest first. In that order the scan can stop once
	// it has enough matches; any other order needs the whole window.
	lookback := cmp.Or(q.Lookback, ActivityLogLookback)
	var matched []map[string]any
	stop := func(m map[string]any) bool {
		if newestFirst && len(matched) >= q.Limit {
			return true
		}
		if q.matches(m) {
			matched = append(matched, m)
		}
		return false
	}
	raw, total, err := FetchAllPagesUntil(ctx, client, "/System/ActivityLog/Entries", params, lookback, stop)
	if err != nil {
		return ActivityResult{}, err
	}
	if !newestFirst {
		q.sort(matched)
	}
	complete := newestFirst && len(matched) >= q.Limit
	return ActivityResult{
		Entries: matched[:min(len(matched), q.Limit)],
		Capped:  !complete && len(raw) >= lookback && total > len(raw),
	}, nil
}

// matches applies q's filters to an entry with Jellyfin 12's meaning.
func (q ActivityQuery) matches(m map[string]any) bool {
	if q.UserID != "" && NormalizeID(GetString(m, "UserId")) != NormalizeID(q.UserID) {
		return false
	}
	if q.ItemID != "" && NormalizeID(GetString(m, "ItemId")) != NormalizeID(q.ItemID) {
		return false
	}
	if q.Type != "" && !strings.Contains(strings.ToLower(GetString(m, "Type")), strings.ToLower(q.Type)) {
		return false
	}
	if len(q.Severities) > 0 && !slices.Contains(q.Severities, GetString(m, "Severity")) {
		return false
	}
	if !q.MaxDate.IsZero() {
		if t, ok := ParseTime(GetString(m, "Date")); !ok || t.After(q.MaxDate) {
			return false
		}
	}
	return true
}

// sort orders entries as Jellyfin 12 does: by q's field, then newest first.
func (q ActivityQuery) sort(entries []map[string]any) {
	date := func(m map[string]any) time.Time {
		t, _ := ParseTime(GetString(m, "Date"))
		return t
	}
	key := func(a, b map[string]any) int {
		switch q.SortBy {
		case "Name", "Type":
			return strings.Compare(GetString(a, q.SortBy), GetString(b, q.SortBy))
		case "LogSeverity":
			return cmp.Compare(slices.Index(ActivitySeverities, GetString(a, "Severity")), slices.Index(ActivitySeverities, GetString(b, "Severity")))
		}
		return date(a).Compare(date(b))
	}
	slices.SortStableFunc(entries, func(a, b map[string]any) int {
		c := key(a, b)
		if !q.Ascending {
			c = -c
		}
		if c == 0 {
			c = date(b).Compare(date(a))
		}
		return c
	})
}

// activityTime formats a bound with the seven fractional digits .NET reads,
// so a bound such as the last nanosecond of a day is not rounded to a whole
// second that leaves out that second's entries.
func activityTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.0000000Z07:00")
}

func sortOrder(ascending bool) string {
	if ascending {
		return "Ascending"
	}
	return "Descending"
}

func toMaps(raw []any) []map[string]any {
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		out = append(out, ToMap(r))
	}
	return out
}

// PlayedItemIDs returns the normalized IDs of the items userID has played,
// as the activity log records it: a VideoPlaybackStopped or
// AudioPlaybackStopped entry, looking back at most ActivityLogLookback
// entries. Marking an item played by hand writes no such entry.
func PlayedItemIDs(ctx context.Context, client Client, userID string) (map[string]bool, error) {
	result, err := QueryActivity(ctx, client, ActivityQuery{Type: "PlaybackStopped", UserID: userID, Limit: ActivityLogLookback})
	if err != nil {
		return nil, fmt.Errorf("reading playback from the activity log: %w", err)
	}
	played := make(map[string]bool, len(result.Entries))
	for _, m := range result.Entries {
		if id := GetString(m, "ItemId"); id != "" {
			played[NormalizeID(id)] = true
		}
	}
	return played, nil
}
