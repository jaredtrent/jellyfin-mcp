package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// typedCall is one action of a typed tool with the arguments it takes, the
// keys its structured result carries when the server has nothing to list,
// and the keys it may carry besides those. Keys of the tool's output struct
// that are in neither set must be absent from the result.
type typedCall struct {
	tool string
	args map[string]any
	// want maps each key that an empty result must carry to its decoded
	// JSON value: []any{} for a list and float64(0) for a count.
	want map[string]any
	// also names keys an empty result may carry without being checked.
	also []string
}

// typedCalls covers every action of the eight typed tools.
var typedCalls = []typedCall{
	{tool: "jellyfin_libraries", want: map[string]any{"count": float64(0), "libraries": []any{}}},
	{tool: "jellyfin_search", args: map[string]any{"query": "Test"}, want: emptyPage()},
	{tool: "jellyfin_browse", args: map[string]any{}, want: emptyPage()},
	{tool: "jellyfin_browse", args: map[string]any{"min_date_created": "2020-01-01"}, want: emptyPage()},
	{tool: "jellyfin_browse", args: map[string]any{"sort_by": "DatePlayed"}, want: emptyPage()},
	{tool: "jellyfin_get_item", args: map[string]any{"item_id": "item-1"}, want: map[string]any{"id": "item-1"}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "next_up"}, want: map[string]any{"items": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "suggestions"}, want: map[string]any{"items": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "latest"}, want: map[string]any{"items": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "similar", "item_id": "item-1"}, want: map[string]any{"items": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "movie_recs"}, want: map[string]any{"categories": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "upcoming"}, want: map[string]any{"items": []any{}}},
	{tool: "jellyfin_recommendations", args: map[string]any{"action": "recently_played"}, want: map[string]any{"items": []any{}, "total_count": float64(0)}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "library_stats"}, want: map[string]any{"stats": []any{}}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "library_size"}, want: map[string]any{"by_type": []any{}, "total_size_mb": float64(0), "total_size_gb": "0.00"}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "size_report"}, want: map[string]any{"size_report": []any{}, "size_report_type": "Series"}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "codec_report"}, want: map[string]any{"codec_report": map[string]any{
		"total_media_sources": float64(0), "video_codecs": map[string]any{}, "audio_codecs": map[string]any{}, "containers": map[string]any{},
		"resolutions": map[string]any{}, "video_ranges": map[string]any{}, "bit_depths": map[string]any{},
	}}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "never_played"}, want: map[string]any{"items": []any{}, "total_count": float64(0), "shown": float64(0)}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "recently_added"}, want: map[string]any{"items": []any{}, "total_count": float64(0), "shown": float64(0), "days": float64(30)}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "duplicate_check"}, want: map[string]any{"duplicates": []any{}}},
	{tool: "jellyfin_analytics", args: map[string]any{"action": "played_status", "item_id": "item-1"}, want: map[string]any{"users": []any{}}, also: []string{"item_summary"}},
	{tool: "jellyfin_tv_shows", args: map[string]any{"action": "seasons", "series_id": "series-1"}, want: map[string]any{"seasons": []any{}}},
	{tool: "jellyfin_tv_shows", args: map[string]any{"action": "episodes", "series_id": "series-1", "season_number": 1}, want: map[string]any{"episodes": []any{}}},
	{tool: "jellyfin_tv_shows", args: map[string]any{"action": "next_up"}, want: map[string]any{"next_up": []any{}}},
	{tool: "jellyfin_sessions", args: map[string]any{"action": "list"}, want: map[string]any{"sessions": []any{}}},
	{tool: "jellyfin_sessions", args: map[string]any{"action": "resume"}, want: map[string]any{"resume": []any{}}},
}

// emptyPage is what an empty jellyfin_search or jellyfin_browse result
// carries.
func emptyPage() map[string]any {
	return map[string]any{"total_count": float64(0), "shown": float64(0), "items": []any{}}
}

// outputKeys lists, per tool, every key of the output struct that is
// omitted when unset, so a test can assert the other actions' keys are
// absent. Notes are guidance, not facts, so they are never asserted absent.
var outputKeys = map[string][]string{
	"jellyfin_libraries":       {"count", "libraries"},
	"jellyfin_search":          {"total_count", "total_is_lower_bound", "shown", "next_start_index", "undated_count", "items"},
	"jellyfin_browse":          {"total_count", "total_is_lower_bound", "shown", "next_start_index", "undated_count", "items"},
	"jellyfin_recommendations": {"items", "categories", "total_count"},
	"jellyfin_analytics": {
		"stats", "unread_types", "total_size_gb", "total_size_mb", "by_type", "size_report_type", "size_report", "codec_report",
		"items", "total_count", "total_is_lower_bound", "shown", "days", "undated_count", "duplicates", "item_summary", "users",
	},
	"jellyfin_tv_shows": {"seasons", "episodes", "next_up"},
	"jellyfin_sessions": {"sessions", "resume"},
}

// bareItemRoute matches GET /Items/{id}, and not the /Items routes that list
// items, such as /Items/Latest.
var bareItemRoute = regexp.MustCompile(`^/Items/item-[^/]+$`)

// fixtureItem is a raw item that satisfies every typed tool: it is inside a
// recent date window, has a season number, belongs to a series, and carries
// a media source with video and audio streams.
func fixtureItem(id string) map[string]any {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z")
	return map[string]any{
		"Id": id, "Name": "Test Item", "Type": "Movie", "ProductionYear": 2001, "Overview": "An overview.",
		"IndexNumber": 1, "ChildCount": 3, "DateCreated": now, "PremiereDate": "2001-02-03T00:00:00.0000000Z",
		"SeriesId": "series-1", "SeriesName": "Test Series", "Path": "/media/" + id + ".mkv",
		"UserData": map[string]any{"Played": true, "PlayedPercentage": 40.0, "LastPlayedDate": now, "PlayCount": 1, "UnplayedItemCount": 1},
		"MediaSources": []map[string]any{{
			"Size": 1_000_000_000, "Container": "mkv",
			"MediaStreams": []map[string]any{
				{"Type": "Video", "Codec": "hevc", "Width": 1920, "Height": 1080, "BitDepth": 10, "VideoRange": "HDR", "VideoRangeType": "HDR10"},
				{"Type": "Audio", "Codec": "aac", "Channels": 2, "Language": "eng"},
			},
		}},
	}
}

// fixtureClient answers every endpoint the typed tools call. With populated
// set, each list holds two items and each object is a full item; otherwise
// every list is empty, except that the season lookup of jellyfin_tv_shows
// episodes finds its season so the episode list can be empty, and the one
// user is disabled so played_status lists nobody.
func fixtureClient(populated bool) *mockClient {
	items := []map[string]any{}
	if populated {
		items = []map[string]any{fixtureItem("item-1"), fixtureItem("item-2")}
	}
	return &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
		switch {
		case endpoint == "/Sessions":
			sessions := []map[string]any{}
			if populated {
				sessions = append(sessions, map[string]any{"Id": "session-1", "UserName": "testuser", "Client": "Jellyfin Web", "DeviceName": "TV", "NowPlayingItem": fixtureItem("item-1"), "PlayState": map[string]any{"PositionTicks": 600000000}})
			}
			return jsonInto(sessions, dest)
		case endpoint == "/Users":
			user := map[string]any{"Id": "user-1", "Name": "User A"}
			if !populated {
				user["Policy"] = map[string]any{"IsDisabled": true}
			}
			return jsonInto([]map[string]any{user}, dest)
		case endpoint == "/Library/VirtualFolders":
			libs := []map[string]any{}
			if populated {
				libs = append(libs, map[string]any{"Name": "Movies", "ItemId": "lib-1", "CollectionType": "movies", "Locations": []string{"/media/movies"}})
			}
			return jsonInto(libs, dest)
		case endpoint == "/Movies/Recommendations":
			recs := []map[string]any{}
			if populated {
				recs = append(recs, map[string]any{"RecommendationType": "SimilarToRecentlyPlayed", "Items": items})
			}
			return jsonInto(recs, dest)
		case endpoint == "/Items/Latest", strings.HasSuffix(endpoint, "/SpecialFeatures"):
			return jsonInto(items, dest)
		case strings.HasSuffix(endpoint, "/Collections"):
			return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		case bareItemRoute.MatchString(endpoint):
			if populated {
				return jsonInto(fixtureItem("item-1"), dest)
			}
			return jsonInto(map[string]any{"Id": "item-1", "Name": "Test Item", "Type": "Movie"}, dest)
		case !populated && params.Get("IncludeItemTypes") == "Season" && !params.Has("Fields"):
			// The season lookup of action episodes asks for no fields;
			// action seasons asks for ChildCount and gets an empty list.
			return jsonInto(map[string]any{"Items": []map[string]any{{"Id": "season-1", "Name": "Season 1", "IndexNumber": 1}}, "TotalRecordCount": 1}, dest)
		default:
			return jsonInto(map[string]any{"Items": items, "TotalRecordCount": len(items)}, dest)
		}
	}}
}

func (c typedCall) name() string {
	if c.args == nil {
		return c.tool
	}
	b, _ := json.Marshal(c.args)
	return fmt.Sprintf("%s %s", c.tool, b)
}

// A typed tool's text content is its structured content as JSON: the two say
// the same thing, and neither is empty.
func TestTypedTools_TextIsTheStructuredContent(t *testing.T) {
	for _, tc := range typedCalls {
		t.Run(tc.name(), func(t *testing.T) {
			result := callTool(t, fixtureClient(true), "", tc.tool, tc.args)
			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(t, result))
			}
			if result.StructuredContent == nil {
				t.Fatal("result has no structured content")
			}
			text := resultText(t, result)
			if !strings.HasPrefix(text, "{") {
				t.Errorf("text does not start with an object: %s", text)
			}
			var fromText, fromStructured any
			if err := json.Unmarshal([]byte(text), &fromText); err != nil {
				t.Fatalf("text is not JSON: %v: %s", err, text)
			}
			if err := jsonInto(result.StructuredContent, &fromStructured); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fromText, fromStructured) {
				t.Errorf("text and structured content differ:\ntext: %s\nstructured: %v", text, result.StructuredContent)
			}
			if obj, ok := fromStructured.(map[string]any); !ok || len(obj) == 0 {
				t.Errorf("structured content is not a non-empty object: %v", fromStructured)
			}
		})
	}
}

// When the server has nothing to list, a typed tool's result still carries
// the action's own lists and counts, as empty and zero, and none of the
// other actions' keys.
func TestTypedTools_EmptySuccessCarriesTheActionsKeys(t *testing.T) {
	for _, tc := range typedCalls {
		t.Run(tc.name(), func(t *testing.T) {
			out := structured[map[string]any](t, callTool(t, fixtureClient(false), "", tc.tool, tc.args))
			for key, want := range tc.want {
				got, ok := out[key]
				if !ok {
					t.Errorf("%s is absent, want %v: %v", key, want, out)
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v, want %#v", key, got, want)
				}
			}
			for _, key := range outputKeys[tc.tool] {
				if _, wanted := tc.want[key]; wanted || slices.Contains(tc.also, key) {
					continue
				}
				if got, present := out[key]; present {
					t.Errorf("%s = %v, want absent: it belongs to another action", key, got)
				}
			}
		})
	}
}
