package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"
)

// activityLog is a Jellyfin server of the given version whose activity log
// holds entries, newest first. It pages them as the endpoint does, applies
// only the minimum date and HasUserId, the filters every version takes, and
// records every query.
type activityLog struct {
	Client
	version ServerVersion
	entries []map[string]any
	err     error
	queries []url.Values
}

func (l *activityLog) ServerVersion(context.Context) (ServerVersion, error) { return l.version, nil }

func (l *activityLog) Get(_ context.Context, endpoint string, params url.Values, dest any) error {
	if endpoint != "/System/ActivityLog/Entries" {
		return fmt.Errorf("unexpected endpoint %s", endpoint)
	}
	sent, _ := url.ParseQuery(params.Encode())
	l.queries = append(l.queries, sent)
	if l.err != nil {
		return l.err
	}
	var kept []map[string]any
	for _, e := range l.entries {
		if params.Get("HasUserId") == "true" && GetString(e, "UserId") == "" {
			continue
		}
		if min := params.Get("MinDate"); min != "" {
			t, _ := ParseTime(GetString(e, "Date"))
			bound, _ := time.Parse(time.RFC3339, min)
			if t.Before(bound) {
				continue
			}
		}
		kept = append(kept, e)
	}
	start, _ := strconv.Atoi(params.Get("StartIndex"))
	limit, _ := strconv.Atoi(params.Get("Limit"))
	start = min(start, len(kept))
	end := min(start+limit, len(kept))
	body, _ := json.Marshal(map[string]any{"Items": kept[start:end], "TotalRecordCount": len(kept)})
	return json.Unmarshal(body, dest)
}

var (
	jellyfin10 = ServerVersion{10, 11, 6}
	jellyfin12 = ServerVersion{12, 0, 0}
)

// activityEntries is a small log, newest first.
func activityEntries() []map[string]any {
	return []map[string]any{
		{"Id": 6, "Date": "2026-03-06T10:00:00.0000000Z", "Name": "Delta", "Type": "AudioPlaybackStopped", "Severity": "Information", "UserId": "AAAA-1111", "ItemId": "item-b"},
		{"Id": 5, "Date": "2026-03-05T23:59:59.5000000Z", "Name": "Alpha", "Type": "AuthenticationFailed", "Severity": "Error", "UserId": "bbbb2222"},
		{"Id": 4, "Date": "2026-03-05T12:00:00.0000000Z", "Name": "Charlie", "Type": "VideoPlaybackStopped", "Severity": "Information", "UserId": "aaaa1111", "ItemId": "ITEM-A"},
		{"Id": 3, "Date": "2026-03-04T12:00:00.0000000Z", "Name": "Bravo", "Type": "TaskFailed", "Severity": "Error"},
		{"Id": 2, "Date": "2026-03-03T12:00:00.0000000Z", "Name": "Alpha", "Type": "VideoPlaybackStart", "Severity": "Information", "UserId": "aaaa1111", "ItemId": "item-a"},
		{"Id": 1, "Date": "2026-03-02T12:00:00.0000000Z", "Name": "Echo", "Type": "SessionStarted", "Severity": "Warning", "UserId": "aaaa1111"},
	}
}

func entryIDs(entries []map[string]any) []int {
	ids := make([]int, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, GetInt(e, "Id"))
	}
	return ids
}

// Jellyfin 12 applies the filters and the sort itself. Each sort field is sent
// with its own order, and ties go newest first.
func TestQueryActivity_Jellyfin12PushesTheFilters(t *testing.T) {
	log := &activityLog{version: jellyfin12, entries: activityEntries()}
	_, err := QueryActivity(t.Context(), log, ActivityQuery{
		MinDate:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		MaxDate:    time.Date(2026, 3, 5, 23, 59, 59, 999999999, time.UTC),
		Type:       "PlaybackStopped",
		ItemID:     "item-a",
		Severities: []string{"Information"},
		SortBy:     "Name",
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := log.queries[0]
	for key, want := range map[string]string{
		"MinDate":  "2026-03-01T00:00:00.0000000Z",
		"MaxDate":  "2026-03-05T23:59:59.9999999Z",
		"Type":     "PlaybackStopped",
		"ItemId":   "item-a",
		"Severity": "Information",
		"Limit":    "10",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got, want := q["SortBy"], []string{"Name", "DateCreated"}; !slices.Equal(got, want) {
		t.Errorf("SortBy = %v, want %v", got, want)
	}
	if got, want := q["SortOrder"], []string{"Descending", "Descending"}; !slices.Equal(got, want) {
		t.Errorf("SortOrder = %v, want %v", got, want)
	}
	if len(log.queries) != 1 {
		t.Errorf("made %d requests, want one page", len(log.queries))
	}

	log = &activityLog{version: jellyfin12}
	if _, err := QueryActivity(t.Context(), log, ActivityQuery{Ascending: true, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if q := log.queries[0]; !slices.Equal(q["SortBy"], []string{"DateCreated"}) || !slices.Equal(q["SortOrder"], []string{"Ascending"}) {
		t.Errorf("date sort sent SortBy %v and SortOrder %v, want DateCreated Ascending alone", q["SortBy"], q["SortOrder"])
	}
}

// With a user filter, which is applied here on every version, Jellyfin 12
// still applies the other filters but sends entries newest first, so the
// window examined is the most recent entries, and the sort is applied here.
func TestQueryActivity_Jellyfin12WithAUserSortsHere(t *testing.T) {
	log := &activityLog{version: jellyfin12, entries: activityEntries()}
	found, err := QueryActivity(t.Context(), log, ActivityQuery{UserID: "aaaa1111", Type: "Playback", SortBy: "Name", Ascending: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	q := log.queries[0]
	if q.Has("SortBy") || q.Has("SortOrder") || q.Get("Type") != "Playback" || q.Get("HasUserId") != "true" {
		t.Errorf("query %v, want the type and HasUserId sent and no sort", q)
	}
	// The fake applies no type filter, so the type is matched here too.
	if got, want := entryIDs(found.Entries), []int{2, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("entries %v, want %v", got, want)
	}
}

// Jellyfin 10.11 takes only the minimum date, so the other filters and the
// sort are applied here, with Jellyfin 12's meaning.
func TestQueryActivity_Jellyfin10AppliesTheFilters(t *testing.T) {
	endOfMarch5 := time.Date(2026, 3, 5, 23, 59, 59, 999999999, time.UTC)
	for _, tt := range []struct {
		name string
		q    ActivityQuery
		want []int
	}{
		{"type contains, ignoring case", ActivityQuery{Type: "playbackstopped"}, []int{6, 4}},
		{"item in any ID form", ActivityQuery{ItemID: "Item-A"}, []int{4, 2}},
		{"user in any ID form", ActivityQuery{UserID: "aaaa1111"}, []int{6, 4, 2, 1}},
		{"severity", ActivityQuery{Severities: []string{"Error"}}, []int{5, 3}},
		{"max date includes its last instant", ActivityQuery{MaxDate: endOfMarch5}, []int{5, 4, 3, 2, 1}},
		{"max date is inclusive", ActivityQuery{MaxDate: time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)}, []int{4, 3, 2, 1}},
		{"min date is sent", ActivityQuery{MinDate: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}, []int{6, 5, 4}},
		{"by name, ties newest first", ActivityQuery{SortBy: "Name", Ascending: true}, []int{5, 2, 3, 4, 6, 1}},
		{"by severity", ActivityQuery{SortBy: "LogSeverity"}, []int{5, 3, 1, 6, 4, 2}},
		{"by type", ActivityQuery{SortBy: "Type", Ascending: true}, []int{6, 5, 1, 3, 2, 4}},
		{"oldest first", ActivityQuery{Ascending: true}, []int{1, 2, 3, 4, 5, 6}},
		{"limit", ActivityQuery{UserID: "aaaa1111", Limit: 2}, []int{6, 4}},
		{"limit after a sort", ActivityQuery{SortBy: "Name", Ascending: true, Limit: 2}, []int{5, 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			log := &activityLog{version: jellyfin10, entries: activityEntries()}
			tt.q.Limit = cmpOr(tt.q.Limit, 10)
			found, err := QueryActivity(t.Context(), log, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := entryIDs(found.Entries); !slices.Equal(got, tt.want) {
				t.Errorf("entries %v, want %v", got, tt.want)
			}
			if found.Capped {
				t.Error("a scan of the whole log reported a cap")
			}
			for _, q := range log.queries {
				for key := range q {
					if key != "MinDate" && key != "HasUserId" && key != "Limit" && key != "StartIndex" {
						t.Errorf("sent %s, which Jellyfin 10.11 does not take", key)
					}
				}
			}
		})
	}
}

func cmpOr(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

// Newest first, the scan stops once it has enough matches. When it reaches
// the lookback without them, the result says older matches may be missing.
func TestQueryActivity_Jellyfin10Scan(t *testing.T) {
	many := make([]map[string]any, 0, ActivityLogLookback+50)
	for i := range ActivityLogLookback + 50 {
		many = append(many, map[string]any{"Id": i, "Date": time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute).Format(time.RFC3339), "Type": "SessionStarted", "UserId": "aaaa1111"})
	}

	log := &activityLog{version: jellyfin10, entries: many}
	found, err := QueryActivity(t.Context(), log, ActivityQuery{UserID: "aaaa1111", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryIDs(found.Entries); !slices.Equal(got, []int{0, 1, 2}) || found.Capped {
		t.Errorf("entries %v capped %v, want [0 1 2] uncapped", got, found.Capped)
	}
	if len(log.queries) != 1 {
		t.Errorf("made %d requests, want to stop after the first page", len(log.queries))
	}

	log = &activityLog{version: jellyfin12, entries: many}
	if _, err := QueryActivity(t.Context(), log, ActivityQuery{UserID: "aaaa1111", Type: "Session", Limit: 3}); err != nil {
		t.Fatal(err)
	}
	if len(log.queries) != 1 {
		t.Errorf("Jellyfin 12 made %d requests, want to stop after the first page", len(log.queries))
	}

	// The limit is reached on the last entry of the window: complete.
	edge := slices.Clone(many)
	for _, i := range []int{0, 1, ActivityLogLookback - 1} {
		edge[i] = maps.Clone(edge[i])
		edge[i]["UserId"] = "cccc3333"
	}
	log = &activityLog{version: jellyfin10, entries: edge}
	found, err = QueryActivity(t.Context(), log, ActivityQuery{UserID: "cccc3333", Limit: 3})
	if err != nil || len(found.Entries) != 3 || found.Capped {
		t.Errorf("limit met at the window's end: entries %v capped %v err %v, want 3 uncapped", entryIDs(found.Entries), found.Capped, err)
	}

	log = &activityLog{version: jellyfin10, entries: many}
	found, err = QueryActivity(t.Context(), log, ActivityQuery{UserID: "someone-else", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Entries) != 0 || !found.Capped {
		t.Errorf("entries %v capped %v, want none and capped", entryIDs(found.Entries), found.Capped)
	}

	log = &activityLog{version: jellyfin10, entries: many[:10]}
	found, err = QueryActivity(t.Context(), log, ActivityQuery{UserID: "someone-else", Limit: 3})
	if err != nil || len(found.Entries) != 0 || found.Capped {
		t.Errorf("a short log: entries %v capped %v err %v, want none, uncapped", entryIDs(found.Entries), found.Capped, err)
	}
}

// Playback of video and audio both count, for the user in any ID form, and
// an unreadable log is an error rather than no playback.
func TestPlayedItemIDs(t *testing.T) {
	for _, v := range []ServerVersion{jellyfin10, jellyfin12} {
		log := &activityLog{version: v, entries: activityEntries()}
		played, err := PlayedItemIDs(t.Context(), log, "AAAA1111")
		if err != nil {
			t.Fatal(err)
		}
		if v == jellyfin12 {
			// The fake does not apply the Type filter Jellyfin 12 applies
			// server-side, so only the request is checked.
			if log.queries[0].Get("Type") != "PlaybackStopped" {
				t.Errorf("Type = %q, want PlaybackStopped", log.queries[0].Get("Type"))
			}
			continue
		}
		if want := map[string]bool{"itemb": true, "itema": true}; len(played) != 2 || !played["itema"] || !played["itemb"] {
			t.Errorf("played %v, want %v", played, want)
		}
	}
	log := &activityLog{version: jellyfin10, err: errors.New("API error 500: failed")}
	if _, err := PlayedItemIDs(t.Context(), log, "aaaa1111"); err == nil {
		t.Error("an unreadable activity log gave no error")
	}
}
