package tools_test

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// liveTVGetCall records one Get request.
type liveTVGetCall struct {
	endpoint string
	params   url.Values
}

// liveTVTimerDefaults is a SeriesTimerInfoDto as GET /LiveTv/Timers/Defaults
// returns it for a program.
var liveTVTimerDefaults = map[string]any{
	"ProgramId":         "program-1",
	"ExternalProgramId": "external-program-1",
	"ChannelId":         "channel-1",
	"ChannelName":       "Channel A",
	"Name":              "Test Show",
	"RecordAnyTime":     true,
	"RecordAnyChannel":  false,
	"RecordNewOnly":     true,
	"Days":              []any{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	"DayPattern":        "Daily",
	"PrePaddingSeconds": float64(60),
}

func TestRecordings_CreateSeriesTimer_PostsTimerDefaults(t *testing.T) {
	var gets []liveTVGetCall
	var posts []postCall
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gets = append(gets, liveTVGetCall{endpoint: endpoint, params: maps.Clone(params)})
			return jsonInto(liveTVTimerDefaults, dest)
		},
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, reqBody any) error {
			posts = append(posts, postCall{endpoint: endpoint, params: maps.Clone(params), body: reqBody})
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_recordings", map[string]any{
		"action":     "create_series_timer",
		"program_id": "program-1",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(gets) != 1 {
		t.Fatalf("got %d GET requests, want 1", len(gets))
	}
	if gets[0].endpoint != "/LiveTv/Timers/Defaults" {
		t.Errorf("defaults endpoint = %q, want /LiveTv/Timers/Defaults", gets[0].endpoint)
	}
	if want := (url.Values{"programId": {"program-1"}}); !reflect.DeepEqual(gets[0].params, want) {
		t.Errorf("defaults params = %v, want %v", gets[0].params, want)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d POST requests, want 1", len(posts))
	}
	if posts[0].endpoint != "/LiveTv/SeriesTimers" {
		t.Errorf("create endpoint = %q, want /LiveTv/SeriesTimers", posts[0].endpoint)
	}
	if len(posts[0].params) != 0 {
		t.Errorf("create params = %v, want none", posts[0].params)
	}
	if got, want := bodyShape(t, posts[0].body), bodyShape(t, liveTVTimerDefaults); !reflect.DeepEqual(got, want) {
		t.Errorf("create body = %v, want the defaults unchanged: %v", got, want)
	}
}

func TestRecordings_CreateSeriesTimer_DefaultsErrorStopsCreation(t *testing.T) {
	var posts []postCall
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, _ any) error {
			return errors.New("API error 404: program not found")
		},
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, reqBody any) error {
			posts = append(posts, postCall{endpoint: endpoint, params: params, body: reqBody})
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_recordings", map[string]any{
		"action":     "create_series_timer",
		"program_id": "program-1",
	})

	text := resultText(t, result)
	if !result.IsError {
		t.Fatalf("expected an error when defaults cannot be fetched, got: %s", text)
	}
	if !strings.Contains(text, "program not found") {
		t.Errorf("expected the server error in the result, got: %s", text)
	}
	if len(posts) != 0 {
		t.Errorf("got %d POST requests, want none: %+v", len(posts), posts)
	}
}

func TestLiveTV_Tuners_ListsConfiguredTunerHosts(t *testing.T) {
	var gets []liveTVGetCall
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			gets = append(gets, liveTVGetCall{endpoint: endpoint, params: maps.Clone(params)})
			return jsonInto(map[string]any{
				"TunerHosts": []map[string]any{
					{"Id": "tuner-1", "Type": "hdhomerun", "FriendlyName": "Tuner A", "Url": "http://tuner-a.example", "TunerCount": 2},
					{"Id": "tuner-2", "Type": "m3u", "FriendlyName": "Tuner B", "Url": "/data/tuner-b.m3u", "TunerCount": 1},
				},
				"ListingProviders": []map[string]any{
					{"Id": "provider-1", "Type": "schedulesdirect", "Password": "placeholder-password"},
				},
				"RecordingPath": "/data/recordings",
			}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_live_tv", map[string]any{
		"action": "tuners",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(gets) != 1 {
		t.Fatalf("got %d GET requests, want 1", len(gets))
	}
	if gets[0].endpoint != "/System/Configuration/livetv" {
		t.Errorf("endpoint = %q, want /System/Configuration/livetv", gets[0].endpoint)
	}
	if len(gets[0].params) != 0 {
		t.Errorf("params = %v, want none", gets[0].params)
	}
	for _, want := range []string{"Configured tuner hosts (2)", "Tuner A", "tuner-2"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in result, got: %s", want, text)
		}
	}
	for _, unwanted := range []string{"placeholder-password", "/data/recordings"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("result includes %q from outside TunerHosts: %s", unwanted, text)
		}
	}
}

func TestLiveTV_Tuners_NoneConfigured(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{"TunerHosts": []any{}}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_live_tv", map[string]any{
		"action": "tuners",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "No tuner hosts are configured") {
		t.Errorf("expected the empty-configuration message, got: %s", text)
	}
}

func TestLiveTV_Tuners_RedactsURLCredentials(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{"TunerHosts": []map[string]any{
				{"Id": "tuner-1", "Type": "m3u", "Url": "http://account:secret-a@iptv.example.com/get.php?username=account&password=secret-b"},
				{"Id": "tuner-2", "Type": "hdhomerun", "Url": "http://tuner-b.example.com:5004"},
				{"Id": "tuner-3", "Type": "m3u", "Url": "/data/tuner-c.m3u"},
				{"Id": "tuner-4", "Type": "m3u", "Url": "http://xtream.example.com/live/account/secret-c/playlist.m3u"},
				{"Id": "tuner-5", "Type": "m3u", "Url": "http://account:secret-d%zz@broken.example.com/"},
			}}, dest)
		},
	}

	text := resultText(t, callTool(t, mc, "", "jellyfin_live_tv", map[string]any{"action": "tuners"}))
	for _, secret := range []string{"secret-a", "secret-b", "secret-c", "secret-d", "account"} {
		if strings.Contains(text, secret) {
			t.Errorf("result leaks %q: %s", secret, text)
		}
	}
	for _, want := range []string{
		`"http://iptv.example.com (path, credentials, and query removed)"`,
		`"http://tuner-b.example.com:5004"`,
		`"/data/tuner-c.m3u"`,
		`"http://xtream.example.com (path, credentials, and query removed)"`,
		`"(URL withheld: it could not be parsed for redaction)"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in result, got: %s", want, text)
		}
	}
}

// Program listings give each program's channel and its start and end in the
// server's time zone, and timers state their times in that zone as well.
func TestLiveTV_ProgramAndTimerTimesInServerZone(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	inZone(t, loc)
	mc := &mockClient{getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
		switch endpoint {
		case "/LiveTv/Programs":
			return jsonInto(map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{{
				"Id": "p1", "Name": "Evening News", "Type": "Program", "ChannelName": "Channel 4",
				"StartDate": "2026-01-02T23:00:00.0000000Z", "EndDate": "2026-01-02T23:30:00.0000000Z",
			}}}, dest)
		case "/LiveTv/Timers":
			return jsonInto(map[string]any{"Items": []map[string]any{{
				"Id": "t1", "Name": "Evening News", "StartDate": "2026-01-02T23:00:00.0000000Z", "EndDate": "2026-01-02T23:30:00.0000000Z",
			}}}, dest)
		}
		return nil
	}}
	programs := resultText(t, callTool(t, mc, "", "jellyfin_live_tv", map[string]any{"action": "programs"}))
	for _, want := range []string{`"start_time": "2026-01-02T18:00:00-05:00"`, `"end_time": "2026-01-02T18:30:00-05:00"`, `"channel_name": "Channel 4"`} {
		if !strings.Contains(programs, want) {
			t.Errorf("programs missing %s:\n%s", want, programs)
		}
	}
	timers := resultText(t, callTool(t, mc, "", "jellyfin_recordings", map[string]any{"action": "timers"}))
	if !strings.Contains(timers, `"StartDate": "2026-01-02T18:00:00-05:00"`) || strings.Contains(timers, "23:00:00") {
		t.Errorf("timers not in the server zone:\n%s", timers)
	}
}
