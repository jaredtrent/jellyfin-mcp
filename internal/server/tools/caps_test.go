package tools_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// pagedItems answers a paged /Items query from n generated items, honoring
// StartIndex and Limit the way Jellyfin does.
func pagedItems(n int, item func(i int) map[string]any, params url.Values, dest any) error {
	start, _ := strconv.Atoi(params.Get("StartIndex"))
	limit, _ := strconv.Atoi(params.Get("Limit"))
	var page []map[string]any
	for i := start; i < n && i < start+limit; i++ {
		page = append(page, item(i))
	}
	return jsonInto(map[string]any{"Items": page, "TotalRecordCount": n}, dest)
}

// The codec report and the duplicate check read every item of the type, past
// the 2,000 a single paged fetch stops at.
func TestAnalytics_WholeLibraryScans(t *testing.T) {
	const n = jf.DefaultMaxItems + 300
	mc := &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
		return pagedItems(n, func(i int) map[string]any {
			name := fmt.Sprintf("Test Movie %d", i)
			if i == n-1 {
				name = "Test Movie 0" // a copy of the first item, found only by reading to the end
			}
			return map[string]any{"Id": fmt.Sprintf("m%d", i), "Name": name, "MediaSources": []map[string]any{{"Container": "mkv"}}}
		}, params, dest)
	}}
	codec := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "codec_report"}))
	if codec.CodecReport == nil || codec.CodecReport.Containers["mkv"] != n {
		t.Errorf("codec report counted %v mkv containers, want %d", codec.CodecReport, n)
	}
	dups := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "duplicate_check"}))
	if dups.Duplicates == nil || len(*dups.Duplicates) != 1 {
		t.Errorf("duplicates = %v, want the one pair whose second copy is the last item", dups.Duplicates)
	}
}

// A list cut at its limit says how many it shows of how many, and how to see
// more.
func TestLists_SayWhenCut(t *testing.T) {
	mc := &mockClient{getFunc: func(_ context.Context, _ string, params url.Values, dest any) error {
		return pagedItems(30, func(i int) map[string]any {
			return map[string]any{"Id": fmt.Sprintf("e%d", i), "Name": fmt.Sprintf("Test Episode %d", i), "Type": "Episode"}
		}, params, dest)
	}}
	next := structured[jf.TVShowsOutput](t, callTool(t, mc, "", "jellyfin_tv_shows", map[string]any{"action": "next_up", "limit": 10}))
	if len(next.Notes) != 1 || !strings.Contains(next.Notes[0], "Showing 10 of 30") || !strings.Contains(next.Notes[0], "Increase limit") {
		t.Errorf("next_up notes = %q", next.Notes)
	}
	resume := structured[jf.SessionsOutput](t, callTool(t, mc, "", "jellyfin_sessions", map[string]any{"action": "resume", "limit": 10}))
	if len(resume.Notes) != 1 || !strings.Contains(resume.Notes[0], "Showing 10 of 30") {
		t.Errorf("resume notes = %q", resume.Notes)
	}
	whole := structured[jf.SessionsOutput](t, callTool(t, mc, "", "jellyfin_sessions", map[string]any{"action": "resume", "limit": 50}))
	if len(whole.Notes) != 0 {
		t.Errorf("a complete list has notes: %q", whole.Notes)
	}
}

// The device list counts every active device, not only the ones it shows.
func TestDevices_ActiveCountIsNotCappedByLimit(t *testing.T) {
	now := time.Now().UTC()
	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
		var devices []map[string]any
		for i := range 5 {
			devices = append(devices, map[string]any{"Id": fmt.Sprintf("d%d", i), "Name": fmt.Sprintf("Test Device %d", i), "DateLastActivity": now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339)})
		}
		return jsonInto(map[string]any{"Items": devices}, dest)
	}}
	text := resultText(t, callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "list", "limit": 2}))
	if !strings.Contains(text, "5 total, 5 active in last 30 days, showing 2") {
		t.Errorf("header = %.120s", text)
	}
}

// played_status names the users whose play state could not be read instead
// of leaving them out silently.
func TestAnalytics_PlayedStatus_NamesUnreadUsers(t *testing.T) {
	mc := &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
		switch endpoint {
		case "/Users":
			return jsonInto([]map[string]any{{"Id": "user-a", "Name": "User A"}, {"Id": "user-b", "Name": "User B"}}, dest)
		case "/Items/m1":
			if params.Get("UserId") == "user-b" {
				return errors.New("API error 404: not found")
			}
			return jsonInto(map[string]any{"Id": "m1", "Name": "Test Movie", "Type": "Movie", "UserData": map[string]any{"Played": true}}, dest)
		}
		return nil
	}}
	out := structured[jf.AnalyticsOutput](t, callTool(t, mc, "", "jellyfin_analytics", map[string]any{"action": "played_status", "item_id": "m1"}))
	if len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "User B") || strings.Contains(out.Notes[0], "User A") {
		t.Errorf("notes = %q, want a note naming only User B", out.Notes)
	}
}
