package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
	"github.com/jaredtrent/jellyfin-mcp/internal/server/resources"
)

func TestSystemInfo_Ping(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/System/Ping" {
				// Ping just needs to not error
				return nil
			}
			return nil
		},
		getRawFunc: func(_ context.Context, endpoint string, _ url.Values) (string, error) {
			if endpoint == "/System/Ping" {
				return "Jellyfin Server", nil
			}
			return "", nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "ping",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "responsive") {
		t.Errorf("expected positive ping result, got: %s", text)
	}
}

// systemInfoPayload returns a /System/Info response shaped like a supported
// server's. The obsolete fields carry the defaults the server always sends,
// because it never sets them.
func systemInfoPayload(version string) map[string]any {
	return map[string]any{
		"ServerName":                 "Test Server",
		"Version":                    version,
		"Id":                         "server-1",
		"StartupWizardCompleted":     true,
		"HasPendingRestart":          false,
		"LocalAddress":               "http://localhost:8096",
		"PackageName":                "test-package",
		"OperatingSystem":            "",
		"OperatingSystemDisplayName": "",
		"CanSelfRestart":             true,
		"HasUpdateAvailable":         false,
	}
}

// systemInfoObsoleteKeys are output keys that could only mirror obsolete
// SystemInfo fields, so no system info surface reports them.
var systemInfoObsoleteKeys = []string{"os", "can_self_restart", "has_update_available"}

func systemInfoClient() *mockClient {
	return &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/System/Info" {
				return jsonInto(systemInfoPayload("12.1.0"), dest)
			}
			return nil
		},
	}
}

func TestSystemInfo_Info(t *testing.T) {
	result := callTool(t, systemInfoClient(), "", "jellyfin_system_info", map[string]any{
		"action": "info",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("decoding info: %v\n%s", err, text)
	}
	want := map[string]any{
		"server_name":              "Test Server",
		"version":                  "12.1.0",
		"id":                       "server-1",
		"startup_wizard_completed": true,
		"has_pending_restart":      false,
		"local_address":            "http://localhost:8096",
		"package_name":             "test-package",
	}
	for key, value := range want {
		if info[key] != value {
			t.Errorf("%s = %v, want %v", key, info[key], value)
		}
	}
	for _, key := range systemInfoObsoleteKeys {
		if got, ok := info[key]; ok {
			t.Errorf("info reports %s = %v, want it omitted", key, got)
		}
	}
}

// The resource shares /System/Info with the info action, so it is checked
// against the same payload here.
func TestServerInfoResource_OmitsObsoleteFields(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	resources.RegisterResources(srv, systemInfoClient())
	ct, st := newTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal("server connect:", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal("client connect:", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://server/info"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &info); err != nil {
		t.Fatalf("decoding resource: %v\n%s", err, res.Contents[0].Text)
	}
	if info["server_name"] != "Test Server" || info["version"] != "12.1.0" {
		t.Errorf("expected server name and version, got %v", info)
	}
	for _, key := range systemInfoObsoleteKeys {
		if got, ok := info[key]; ok {
			t.Errorf("resource reports %s = %v, want it omitted", key, got)
		}
	}
}

// /System/Info/Storage requires an administrator, while /System/Info does not.
// A refused storage request is reported as an error rather than answered with
// the unrelated /System/Info payload.
func TestSystemInfo_Storage_ReportsStorageError(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/System/Info/Storage":
				return errors.New("API error 403: forbidden")
			case "/System/Info":
				return jsonInto(systemInfoPayload("12.1.0"), dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "storage",
	})

	text := resultText(t, result)
	if !result.IsError {
		t.Errorf("expected an error result, got: %s", text)
	}
	if !strings.Contains(text, "API error 403") {
		t.Errorf("expected the storage API error, got: %s", text)
	}
	for _, field := range []string{"OperatingSystem", "CanSelfRestart", "HasUpdateAvailable", "Test Server"} {
		if strings.Contains(text, field) {
			t.Errorf("storage output contains /System/Info field %q: %s", field, text)
		}
	}
}

func TestSystemInfo_Whoami(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if strings.Contains(endpoint, "/Users/") {
				return jsonInto(map[string]any{
					"Name": "admin",
					"Id":   "test-user-id",
					"Policy": map[string]any{
						"IsAdministrator": true,
					},
				}, dest)
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "whoami",
	})

	text := resultText(t, result)
	if !strings.Contains(text, "admin") {
		t.Errorf("expected username in result, got: %s", text)
	}
}

// healthCheckClient returns a mock whose /System/Info reports version and
// whose /Backup lists one fresh backup, so the only possible source of a
// non-healthy status is the server version itself.
func healthCheckClient(version string) *mockClient {
	return &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/System/Info":
				return jsonInto(systemInfoPayload(version), dest)
			case "/Backup":
				return jsonInto([]map[string]any{{
					"DateCreated":   time.Now().UTC().Format(time.RFC3339),
					"ServerVersion": version,
				}}, dest)
			}
			return nil
		},
	}
}

// healthCheckReport decodes the JSON report that follows the health_check
// header line.
func healthCheckReport(t *testing.T, text string) map[string]any {
	t.Helper()
	_, body, ok := strings.Cut(text, "\n\n")
	if !ok {
		t.Fatalf("health_check output has no report body: %s", text)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("decoding health_check report: %v\n%s", err, body)
	}
	return report
}

func healthCheckServerSection(t *testing.T, report map[string]any) map[string]any {
	t.Helper()
	server, ok := report["server"].(map[string]any)
	if !ok {
		t.Fatalf("report has no server section: %v", report)
	}
	return server
}

func TestSystemInfo_HealthCheck_SupportedVersion(t *testing.T) {
	result := callTool(t, healthCheckClient("12.1.0"), "", "jellyfin_system_info", map[string]any{
		"action": "health_check",
	})

	report := healthCheckReport(t, resultText(t, result))
	server := healthCheckServerSection(t, report)
	if server["supported"] != true {
		t.Errorf("supported = %v, want true", server["supported"])
	}
	if server["minimum_supported_version"] != "10.11.0" {
		t.Errorf("minimum_supported_version = %v, want 10.11.0", server["minimum_supported_version"])
	}
	if _, ok := server["note"]; ok {
		t.Errorf("unexpected note for a supported version: %v", server["note"])
	}
	if report["status"] != "ok" {
		t.Errorf("status = %v, want ok", report["status"])
	}
}

func TestSystemInfo_HealthCheck_OmitsObsoleteFields(t *testing.T) {
	result := callTool(t, healthCheckClient("12.1.0"), "", "jellyfin_system_info", map[string]any{
		"action": "health_check",
	})

	server := healthCheckServerSection(t, healthCheckReport(t, resultText(t, result)))
	if server["server_name"] != "Test Server" || server["version"] != "12.1.0" {
		t.Errorf("expected server name and version, got %v", server)
	}
	for _, key := range systemInfoObsoleteKeys {
		if got, ok := server[key]; ok {
			t.Errorf("server section reports %s = %v, want it omitted", key, got)
		}
	}
}

func TestSystemInfo_HealthCheck_UnsupportedVersion(t *testing.T) {
	result := callTool(t, healthCheckClient("10.10.7"), "", "jellyfin_system_info", map[string]any{
		"action": "health_check",
	})

	report := healthCheckReport(t, resultText(t, result))
	server := healthCheckServerSection(t, report)
	if server["supported"] != false {
		t.Errorf("supported = %v, want false", server["supported"])
	}
	if server["minimum_supported_version"] != "10.11.0" {
		t.Errorf("minimum_supported_version = %v, want 10.11.0", server["minimum_supported_version"])
	}
	note, _ := server["note"].(string)
	if !strings.Contains(note, "10.10.7") || !strings.Contains(note, "below the minimum supported version") {
		t.Errorf("note = %q, want it to name the version and the supported minimum", note)
	}
	if report["status"] != "warnings" {
		t.Errorf("status = %v, want warnings", report["status"])
	}
}

func TestSystemInfo_HealthCheck_UnparsableVersion(t *testing.T) {
	result := callTool(t, healthCheckClient("unknown"), "", "jellyfin_system_info", map[string]any{
		"action": "health_check",
	})

	report := healthCheckReport(t, resultText(t, result))
	server := healthCheckServerSection(t, report)
	if _, ok := server["supported"]; ok {
		t.Errorf("supported should be omitted for an unparsable version, got %v", server["supported"])
	}
	if report["status"] != "ok" {
		t.Errorf("status = %v, want ok", report["status"])
	}
}

// The last backup's date is its day in the server's time zone, and its age
// counts from when it was made.
func TestSystemInfo_HealthCheck_BackupDateInTheServerZone(t *testing.T) {
	inZone(t, time.FixedZone("UTC+9", 9*60*60))
	// 20:00 UTC is 05:00 the next day in UTC+9, between one and two days ago.
	now := time.Now().UTC()
	made := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	if now.Sub(made) < 24*time.Hour {
		made = made.AddDate(0, 0, -1)
	}
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/System/Info":
				return jsonInto(systemInfoPayload("12.1.0"), dest)
			case "/Backup":
				return jsonInto([]map[string]any{{"DateCreated": made.Format("2006-01-02T15:04:05.0000000Z")}}, dest)
			}
			return nil
		},
	}
	report := healthCheckReport(t, resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "health_check"})))
	backups, ok := report["backups"].(map[string]any)
	if !ok {
		t.Fatalf("report has no backups section: %v", report)
	}
	if want := made.In(time.Local).Format("2006-01-02"); backups["last_backup"] != want {
		t.Errorf("last_backup = %v, want %s", backups["last_backup"], want)
	}
	if backups["last_backup_age_days"] != float64(1) {
		t.Errorf("last_backup_age_days = %v, want 1", backups["last_backup_age_days"])
	}
}

func TestSystemInfo_HealthCheck_BackupFailureReportsError(t *testing.T) {
	mc := healthCheckClient("12.1.0")
	getInfo := mc.getFunc
	mc.getFunc = func(ctx context.Context, endpoint string, params url.Values, dest any) error {
		if endpoint == "/Backup" {
			return errors.New("API error 403: forbidden")
		}
		return getInfo(ctx, endpoint, params, dest)
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "health_check",
	})

	report := healthCheckReport(t, resultText(t, result))
	backups, ok := report["backups"].(map[string]any)
	if !ok {
		t.Fatalf("report has no backups section: %v", report)
	}
	errText, _ := backups["error"].(string)
	if !strings.Contains(errText, "API error 403") {
		t.Errorf("backups error = %q, want the underlying API error", errText)
	}
}

func TestSystemInfo_PlaybackHistory_QueriesItemsWithUserID(t *testing.T) {
	// The ID contains a character that path escaping would alter, so the
	// assertions fail if it is escaped before it reaches the query.
	const targetUser = "user/a"
	var itemsParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			switch endpoint {
			case "/Items":
				itemsParams = params
				return jsonInto(map[string]any{
					"Items": []map[string]any{{
						"Id":   "item-1",
						"Name": "Test Movie",
						"Type": "Movie",
						"UserData": map[string]any{
							"Played":    true,
							"PlayCount": 1,
						},
					}},
					"TotalRecordCount": 1,
				}, dest)
			case "/System/ActivityLog/Entries":
				return jsonInto(map[string]any{
					"Items": []map[string]any{{
						"Type":   "VideoPlaybackStopped",
						"UserId": targetUser,
						"ItemId": "item-1",
					}},
					"TotalRecordCount": 1,
				}, dest)
			}
			t.Errorf("unexpected GET %s", endpoint)
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action":  "playback_history",
		"user_id": targetUser,
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if itemsParams == nil {
		t.Fatal("expected a GET /Items request")
	}
	if got := itemsParams.Get("UserId"); got != targetUser {
		t.Errorf("UserId query = %q, want %q", got, targetUser)
	}
	if itemsParams.Get("Filters") != "IsPlayed" || itemsParams.Get("Recursive") != "true" {
		t.Errorf("expected played-item filters to be kept, got %v", itemsParams)
	}
	if !strings.Contains(text, "Test Movie") || !strings.Contains(text, `"actual_playback": true`) {
		t.Errorf("expected the item to be matched to its playback event, got: %s", text)
	}
}

// last_played is an instant, shown in the server's time zone with its offset.
func TestSystemInfo_PlaybackHistory_LastPlayedInTheServerZone(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint == "/Items" {
				return jsonInto(map[string]any{"Items": []map[string]any{{
					"Id": "item-1", "Name": "Test Movie", "Type": "Movie",
					"UserData": map[string]any{"Played": true, "LastPlayedDate": "2026-03-05T03:30:00.1234567Z"},
				}}, "TotalRecordCount": 1}, dest)
			}
			return jsonInto(map[string]any{"Items": []map[string]any{}}, dest)
		},
	}
	text := resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "playback_history", "verified_only": false}))
	if !strings.Contains(text, `"last_played": "2026-03-04T20:30:00-07:00"`) {
		t.Errorf("want last_played in the server's zone: %s", text)
	}
}

func TestSystemInfo_PlaybackHistory_DefaultsToCurrentUser(t *testing.T) {
	var itemsParams url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, _ any) error {
			if endpoint == "/Items" {
				itemsParams = params
			}
			return nil
		},
	}

	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "playback_history",
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if got := itemsParams.Get("UserId"); got != "test-user-id" {
		t.Errorf("UserId query = %q, want test-user-id", got)
	}
}

// Two user IDs in the form Jellyfin writes them: 32 lower-case hex digits
// without dashes.
const (
	systemInfoUserID      = "0123456789abcdef0123456789abcdef"
	systemInfoOtherUserID = "fedcba9876543210fedcba9876543210"
)

// systemInfoSuppliedUserIDs holds systemInfoUserID in each form a caller may
// supply it.
var systemInfoSuppliedUserIDs = []string{
	systemInfoUserID,
	"01234567-89ab-cdef-0123-456789abcdef",
	"01234567-89AB-CDEF-0123-456789ABCDEF",
}

// systemInfoEntries decodes the JSON array that follows a list action's
// header line.
func systemInfoEntries(t *testing.T, text string) []map[string]any {
	t.Helper()
	_, body, ok := strings.Cut(text, "\n\n")
	if !ok {
		t.Fatalf("output has no JSON body: %s", text)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("decoding entries: %v\n%s", err, body)
	}
	return entries
}

func TestSystemInfo_PlaybackHistory_MatchesUserIDInAnyForm(t *testing.T) {
	for _, supplied := range systemInfoSuppliedUserIDs {
		t.Run(supplied, func(t *testing.T) {
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
					switch endpoint {
					case "/Items":
						return jsonInto(map[string]any{
							"Items": []map[string]any{
								{"Id": "item-1", "Name": "Test Movie", "Type": "Movie", "UserData": map[string]any{"Played": true}},
								{"Id": "item-2", "Name": "Test Movie 2", "Type": "Movie", "UserData": map[string]any{"Played": true}},
							},
							"TotalRecordCount": 2,
						}, dest)
					case "/System/ActivityLog/Entries":
						return jsonInto(map[string]any{
							"Items": []map[string]any{
								{"Type": "VideoPlaybackStopped", "UserId": systemInfoUserID, "ItemId": "item-1"},
								{"Type": "VideoPlaybackStopped", "UserId": systemInfoOtherUserID, "ItemId": "item-2"},
							},
							"TotalRecordCount": 2,
						}, dest)
					}
					return nil
				},
			}

			result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
				"action":        "playback_history",
				"user_id":       supplied,
				"verified_only": false,
			})

			text := resultText(t, result)
			if result.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			actual := map[string]any{}
			for _, e := range systemInfoEntries(t, text) {
				id, _ := e["id"].(string)
				actual[id] = e["actual_playback"]
			}
			if actual["item-1"] != true {
				t.Errorf("item-1 actual_playback = %v, want true for the user's own playback", actual["item-1"])
			}
			if actual["item-2"] != false {
				t.Errorf("item-2 actual_playback = %v, want false for another user's playback", actual["item-2"])
			}
		})
	}
}

func TestSystemInfo_ActivityLog_FiltersUserIDInAnyForm(t *testing.T) {
	for _, supplied := range systemInfoSuppliedUserIDs {
		t.Run(supplied, func(t *testing.T) {
			mc := &mockClient{
				getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
					if endpoint == "/System/ActivityLog/Entries" {
						return jsonInto(map[string]any{
							"Items": []map[string]any{
								{"Name": "Event A", "Type": "SessionStarted", "UserId": systemInfoUserID},
								{"Name": "Event B", "Type": "SessionStarted", "UserId": systemInfoOtherUserID},
							},
							"TotalRecordCount": 2,
						}, dest)
					}
					return nil
				},
			}

			result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
				"action":  "activity_log",
				"user_id": supplied,
			})

			text := resultText(t, result)
			if result.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			entries := systemInfoEntries(t, text)
			if len(entries) != 1 || entries[0]["name"] != "Event A" {
				t.Errorf("entries = %v, want only the user's Event A", entries)
			}
		})
	}
}

// A min_date given as a date means midnight in the server's time zone, and
// Jellyfin receives it as that exact instant.
// Activity log dates are shown in the server's time zone, with the offset.
func TestSystemInfo_ActivityLog_DatesInTheServerZone(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{"Items": []map[string]any{
				{"Date": "2026-03-05T07:05:00.1234567Z", "Name": "Test event", "Type": "SessionStarted", "Severity": "Information"},
			}, "TotalRecordCount": 1}, dest)
		},
	}
	text := resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "activity_log"}))
	if !strings.Contains(text, `"2026-03-05T00:05:00-07:00"`) {
		t.Errorf("want the entry's date in the server's zone: %s", text)
	}
}

func TestSystemInfo_ActivityLog_MinDate(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	for _, tt := range []struct {
		minDate, want string
	}{
		{"2026-03-05", "2026-03-05T07:00:00.0000000Z"},
		{"2026-03-05T18:00:00+02:00", "2026-03-05T16:00:00.0000000Z"},
	} {
		var sent string
		mc := &mockClient{
			getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
				sent = params.Get("MinDate")
				return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
			},
		}
		result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "activity_log", "min_date": tt.minDate})
		if result.IsError || sent != tt.want {
			t.Errorf("min_date %s: sent MinDate %q, want %q: %s", tt.minDate, sent, tt.want, resultText(t, result))
		}
	}
	result := callTool(t, &mockClient{}, "", "jellyfin_system_info", map[string]any{"action": "activity_log", "min_date": "last week"})
	if !result.IsError || !strings.Contains(resultText(t, result), "min_date must be a date") {
		t.Errorf("malformed min_date: %s", resultText(t, result))
	}
}

// On Jellyfin 12, activity_log sends its filters and sort to the server,
// reading dates in the server's zone and severity in any case.
func TestSystemInfo_ActivityLog_Filters(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	var sent url.Values
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			sent, _ = url.ParseQuery(params.Encode())
			return jsonInto(map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0}, dest)
		},
	}
	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{
		"action": "activity_log", "type": "PlaybackStopped", "item_id": "0123456789ABCDEF0123456789abcdef", "severity": "error",
		"min_date": "2026-03-01", "max_date": "2026-03-05", "sort_by": "severity",
	})
	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	for key, want := range map[string]string{
		"MinDate": "2026-03-01T07:00:00.0000000Z", "MaxDate": "2026-03-06T06:59:59.9999999Z",
		"Type": "PlaybackStopped", "ItemId": "0123456789ABCDEF0123456789abcdef", "Severity": "Error",
	} {
		if got := sent.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if !slices.Equal(sent["SortBy"], []string{"LogSeverity", "DateCreated"}) || !slices.Equal(sent["SortOrder"], []string{"Descending", "Descending"}) {
		t.Errorf("SortBy %v SortOrder %v, want the most severe first, then newest first", sent["SortBy"], sent["SortOrder"])
	}
	if !strings.Contains(text, "severity=Error") || !strings.Contains(text, "sorted by severity descending") {
		t.Errorf("header does not describe the filters: %s", text)
	}

	// Names sort A to Z unless told otherwise.
	text = resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "activity_log", "sort_by": "name"}))
	if !slices.Equal(sent["SortBy"], []string{"Name", "DateCreated"}) || !slices.Equal(sent["SortOrder"], []string{"Ascending", "Descending"}) {
		t.Errorf("SortBy %v SortOrder %v, want names ascending, then newest first", sent["SortBy"], sent["SortOrder"])
	}
	if !strings.Contains(text, "sorted by name ascending") {
		t.Errorf("header does not describe the sort: %s", text)
	}
}

func TestSystemInfo_ActivityLog_RejectsBadFilters(t *testing.T) {
	for _, tt := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"severity": "loud"}, "severity for activity_log must be one or more of Trace, Debug, Information, Warning, Error, Critical, None"},
		{map[string]any{"sort_by": "user"}, "sort_by for activity_log must be date, name, type, or severity"},
		{map[string]any{"sort_order": "up"}, "sort_order must be ascending or descending"},
		{map[string]any{"min_date": "2026-03-05", "max_date": "2026-03-04"}, "min_date (2026-03-05) is later than max_date (2026-03-04)"},
		{map[string]any{"max_date": "soon"}, "max_date must be a date"},
		{map[string]any{"item_id": "item-1"}, `item_id must be a Jellyfin item ID of 32 hexadecimal digits, with or without dashes, not "item-1"`},
	} {
		tt.args["action"] = "activity_log"
		result := callTool(t, &mockClient{}, "", "jellyfin_system_info", tt.args)
		if !result.IsError || !strings.Contains(resultText(t, result), tt.want) {
			t.Errorf("%v: %s, want %q", tt.args, resultText(t, result), tt.want)
		}
	}
}

// On Jellyfin 10.11 the filters are applied to the most recent entries, and
// the result says when older matches may be missing.
func TestSystemInfo_ActivityLog_Jellyfin10SaysWhenCapped(t *testing.T) {
	mc := &mockClient{
		serverVersion: "10.11.6",
		getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
			if params.Has("Type") || params.Has("Severity") {
				t.Errorf("sent filters Jellyfin 10.11 does not take: %v", params)
			}
			limit, _ := strconv.Atoi(params.Get("Limit"))
			items := make([]map[string]any, limit)
			for i := range items {
				items[i] = map[string]any{"Date": "2026-03-01T00:00:00Z", "Type": "SessionStarted", "Severity": "Information"}
			}
			return jsonInto(map[string]any{"Items": items, "TotalRecordCount": 5000}, dest)
		},
	}
	text := resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "activity_log", "severity": "Error"}))
	if !strings.Contains(text, "Activity log (0 entries, severity=Error)") || !strings.Contains(text, "Only the 2000 most recent entries were examined") {
		t.Errorf("want no entries and the scan's limit: %s", text)
	}
}

// Audio playback verifies an item too, and playback history without a
// readable activity log fails when it must verify, and otherwise says why
// actual_playback is missing.
func TestSystemInfo_PlaybackHistory_Verification(t *testing.T) {
	logErr := error(nil)
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Items":
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Id": "track-1", "Name": "Test Track", "Type": "Audio", "UserData": map[string]any{"Played": true}},
				}, "TotalRecordCount": 1}, dest)
			case "/System/ActivityLog/Entries":
				if logErr != nil {
					return logErr
				}
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Type": "AudioPlaybackStopped", "UserId": "test-user-id", "ItemId": "TRACK-1"},
				}, "TotalRecordCount": 1}, dest)
			}
			return nil
		},
	}
	text := resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "playback_history"}))
	if !strings.Contains(text, "Test Track") || !strings.Contains(text, `"actual_playback": true`) {
		t.Errorf("want the track verified by its audio playback: %s", text)
	}

	logErr = errors.New("API error 500: failed")
	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "playback_history"})
	if !result.IsError || !strings.Contains(resultText(t, result), "Set verified_only=false") {
		t.Errorf("verified history without the activity log: %s", resultText(t, result))
	}
	result = callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "playback_history", "verified_only": false})
	text = resultText(t, result)
	if result.IsError || !strings.Contains(text, "Test Track") || strings.Contains(text, `"actual_playback":`) || !strings.Contains(text, "Playback could not be verified") {
		t.Errorf("unverified history without the activity log: %s", text)
	}
}

// jellyfin://recently-played counts audio playback, and fails rather than
// listing nothing when the activity log cannot be read.
func TestRecentlyPlayedResource_Verification(t *testing.T) {
	logErr := error(nil)
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			switch endpoint {
			case "/Items":
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Id": "track-1", "Name": "Test Track", "Type": "Audio"},
					{"Id": "track-2", "Name": "Marked Track", "Type": "Audio"},
				}, "TotalRecordCount": 2}, dest)
			case "/System/ActivityLog/Entries":
				if logErr != nil {
					return logErr
				}
				return jsonInto(map[string]any{"Items": []map[string]any{
					{"Type": "AudioPlaybackStopped", "UserId": "test-user-id", "ItemId": "track-1"},
				}, "TotalRecordCount": 1}, dest)
			}
			return nil
		},
	}
	cs := newFullSession(t, mc)
	read, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://recently-played"})
	if err != nil {
		t.Fatal(err)
	}
	if text := read.Contents[0].Text; !strings.Contains(text, "Test Track") || strings.Contains(text, "Marked Track") {
		t.Errorf("want only the played track: %s", text)
	}
	logErr = errors.New("API error 500: failed")
	if _, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "jellyfin://recently-played"}); err == nil {
		t.Error("reading recently-played without the activity log succeeded")
	}
}

// The troubleshoot prompt looks for recent failures by severity.
func TestTroubleshootPrompt_UsesActivitySeverity(t *testing.T) {
	cs := newFullSession(t, &mockClient{})
	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "troubleshoot"})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, `action="activity_log" severity="Error,Warning"`) {
		t.Errorf("prompt does not ask for errors and warnings: %s", text)
	}
	assertNamesRegisteredTools(t, cs, "the troubleshoot prompt", text)
}

// Log files are listed newest first by the instant they were modified, also
// across a daylight saving change.
func TestSystemInfo_LogsNewestFirstAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	inZone(t, loc)
	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto([]map[string]any{
			{"Name": "log_20261101a.log", "DateModified": "2026-11-01T08:30:00.0000000Z", "Size": 1},
			{"Name": "log_20261101b.log", "DateModified": "2026-11-01T09:10:00.0000000Z", "Size": 1},
		}, dest)
	}}
	text := resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "logs"}))
	if i, j := strings.Index(text, "log_20261101b.log"), strings.Index(text, "log_20261101a.log"); i < 0 || j < 0 || i > j {
		t.Errorf("want the later log first: %s", text)
	}
}

// A filter value the code does not know is an error, never zero matches.
func TestSystemInfo_LogFiltersRejectUnknownValues(t *testing.T) {
	mc := &mockClient{
		getRawFunc: func(context.Context, string, url.Values) (string, error) { return "[WRN] a\n[ERR] b\n", nil },
		getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
			return jsonInto([]map[string]any{{"Name": "log_1.log"}}, dest)
		},
	}
	result := callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "log_file", "name": "log_1.log", "severity": "Warning"})
	if text := resultText(t, result); !result.IsError || !strings.Contains(text, "not valid for log_file") {
		t.Errorf("log_file: got %s", text)
	}
	result = callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "logs", "log_type": "server"})
	if text := resultText(t, result); !result.IsError || !strings.Contains(text, "log_type") {
		t.Errorf("logs: got %s", text)
	}
	result = callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "log_file", "name": "log_1.log", "severity": "warn"})
	if text := resultText(t, result); result.IsError || !strings.Contains(text, "1 entry matches") {
		t.Errorf("warn: got %s", text)
	}
}

// A section whose request failed is reported with its error, and the status
// is then unknown rather than healthy.
func TestSystemInfo_HealthCheck_FailedSectionMakesStatusUnknown(t *testing.T) {
	mc := healthCheckClient("12.1.0")
	getInfo := mc.getFunc
	mc.getFunc = func(ctx context.Context, endpoint string, params url.Values, dest any) error {
		if endpoint == "/System/Info/Storage" {
			return errors.New("API error 500: storage")
		}
		return getInfo(ctx, endpoint, params, dest)
	}
	report := healthCheckReport(t, resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "health_check"})))
	if report["status"] != "unknown" {
		t.Errorf("status = %v, want unknown", report["status"])
	}
	storage, _ := report["storage"].(map[string]any)
	if errText, _ := storage["error"].(string); !strings.Contains(errText, "API error 500: storage") {
		t.Errorf("storage section = %v", report["storage"])
	}
}

// A plugin that failed to load is named in the report and makes the status
// warnings, under either spelling Jellyfin uses for a superseded plugin.
func TestSystemInfo_HealthCheck_ReportsPluginsThatFailedToLoad(t *testing.T) {
	mc := healthCheckClient("12.1.0")
	getInfo := mc.getFunc
	mc.getFunc = func(ctx context.Context, endpoint string, params url.Values, dest any) error {
		if endpoint == "/Plugins" {
			return jsonInto([]map[string]any{
				{"Name": "Broken", "Version": "1.0.0.0", "Status": "Malfunctioned"},
				{"Name": "Too New", "Version": "2.0.0.0", "Status": "NotSupported"},
				{"Name": "Old", "Version": "0.9.0.0", "Status": "Superceded"},
				{"Name": "Fine", "Version": "3.0.0.0", "Status": "Active"},
			}, dest)
		}
		return getInfo(ctx, endpoint, params, dest)
	}
	report := healthCheckReport(t, resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "health_check"})))
	if report["status"] != "warnings" {
		t.Errorf("status = %v, want warnings", report["status"])
	}
	plugins, _ := report["plugins"].(map[string]any)
	failedJSON, _ := json.Marshal(plugins["failed_to_load"])
	failed := string(failedJSON)
	if !strings.Contains(failed, "Broken (1.0.0.0)") || !strings.Contains(failed, "Too New (2.0.0.0)") || strings.Contains(failed, "Fine") {
		t.Errorf("failed_to_load = %v", plugins["failed_to_load"])
	}
	if restart, _ := json.Marshal(plugins["needs_restart"]); !strings.Contains(string(restart), "Old (0.9.0.0)") {
		t.Errorf("needs_restart = %v", plugins["needs_restart"])
	}
}

// With an issue, the troubleshoot prompt offers every group of checks, so an
// issue phrased without a subsystem's name still reaches its checks.
func TestTroubleshootPrompt_OffersEveryCheckGroup(t *testing.T) {
	cs := newFullSession(t, &mockClient{})
	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "troubleshoot", Arguments: map[string]string{"issue": "I can't watch Harbor Lights on the TV"}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{`"I can't watch Harbor Lights on the TV"`, `action="playback_info"`, `key="network"`, "jellyfin://guides/library-setup", "jellyfin://guides/plugins", `action="storage"`, "Malfunctioned"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt is missing %q:\n%s", want, text)
		}
	}
	assertNamesRegisteredTools(t, cs, "the troubleshoot prompt", text)
}

// serverLog is a log in Jellyfin's default template: an error with its
// exception and stack trace, a fatal error, and warnings after both.
const serverLog = `[2026-01-02 00:55:00.000 -05:00] [INF] [1] Main: Startup complete
[2026-01-02 00:55:01.000 -05:00] [ERR] [76] Api.ExceptionMiddleware: Error processing request.
Sample.DatabaseException: Database Error 5: 'database is locked'.
   at Sample.Db.Throw()
   at Sample.Db.Step()
   at Sample.Db.Read()
   at Sample.Db.Query()
   at Sample.Db.Open()
[2026-01-02 00:56:00.000 -05:00] [FTL] [1] Main: Host terminated
[2026-01-02 00:57:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 1
[2026-01-02 00:58:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 2
[2026-01-02 00:59:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 3
[2026-01-02 01:00:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 4
[2026-01-02 01:01:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 5
[2026-01-02 01:02:00.000 -05:00] [WRN] [40] Net.WebSocket: closed early 6
`

func logFileClient(content string) *mockClient {
	return &mockClient{getRawFunc: func(context.Context, string, url.Values) (string, error) { return content, nil }}
}

// log_file returns whole entries: an error keeps its exception and the first
// frames of its trace, and error covers fatal errors.
func TestLogFile_ErrorsAreWholeEntries(t *testing.T) {
	inZone(t, time.FixedZone("UTC-5", -5*60*60))
	text := resultText(t, callTool(t, logFileClient(serverLog), "", "jellyfin_system_info", map[string]any{"action": "log_file", "name": "log_1.log", "severity": "error"}))
	for _, want := range []string{"2 entries match", "database is locked", "at Sample.Db.Read()", "(2 more stack frames left out)", "[FTL]"} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[WRN]") || strings.Contains(text, "at Sample.Db.Query()") {
		t.Errorf("result holds a warning or a frame past the limit:\n%s", text)
	}
}

// contains searches an entry's exception text, and a time window with a
// limit says which max_date reaches the entries it left out.
func TestLogFile_ContainsAndWindow(t *testing.T) {
	inZone(t, time.FixedZone("UTC-5", -5*60*60))
	text := resultText(t, callTool(t, logFileClient(serverLog), "", "jellyfin_system_info", map[string]any{"action": "log_file", "name": "log_1.log", "contains": "DATABASE ERROR 5"}))
	if !strings.Contains(text, "1 entry matches") || !strings.Contains(text, "Error processing request") {
		t.Errorf("contains did not find the entry by its exception:\n%s", text)
	}
	text = resultText(t, callTool(t, logFileClient(serverLog), "", "jellyfin_system_info", map[string]any{
		"action": "log_file", "name": "log_1.log", "min_date": "2026-01-02T00:56:00-05:00", "max_date": "2026-01-02T01:00:00-05:00", "limit": 2,
	}))
	for _, want := range []string{"5 entries match", "showing the newest 2", "The 3 earlier entries are left out because the limit is 2", "max_date=2026-01-02T00:58:59.999-05:00", "closed early 3", "closed early 4"} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "closed early 5") {
		t.Errorf("an entry after max_date was returned:\n%s", text)
	}
}

// A result stays within the size budget and says how to reach the rest.
func TestLogFile_SizeBudget(t *testing.T) {
	inZone(t, time.FixedZone("UTC-5", -5*60*60))
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "[2026-01-02 01:%02d:%02d.000 -05:00] [INF] [1] Main: %s\n", i/60, i%60, strings.Repeat("x", 300))
	}
	text := resultText(t, callTool(t, logFileClient(b.String()), "", "jellyfin_system_info", map[string]any{"action": "log_file", "name": "log_1.log", "limit": 500}))
	if len(text) > jf.LogFileMaxChars+2000 {
		t.Errorf("result is %d characters, over the budget of %d", len(text), jf.LogFileMaxChars)
	}
	if !strings.Contains(text, "more would not fit in one result") || !strings.Contains(text, "max_date=") {
		t.Errorf("result does not say how to reach the rest:\n%.400s", text)
	}
}

// The health check samples errors and warnings separately, so later warnings
// do not push the errors out, and an error's sample carries its exception.
func TestSystemInfo_HealthCheck_SamplesErrorsSeparately(t *testing.T) {
	mc := healthCheckClient("12.1.0")
	getInfo := mc.getFunc
	mc.getFunc = func(ctx context.Context, endpoint string, params url.Values, dest any) error {
		if endpoint == "/System/Logs" {
			return jsonInto([]map[string]any{{"Name": "log_1.log", "DateModified": "2026-01-02T06:00:00Z"}}, dest)
		}
		return getInfo(ctx, endpoint, params, dest)
	}
	mc.getRawFunc = func(context.Context, string, url.Values) (string, error) { return serverLog, nil }
	report := healthCheckReport(t, resultText(t, callTool(t, mc, "", "jellyfin_system_info", map[string]any{"action": "health_check"})))
	logs, _ := report["recent_log_issues"].(map[string]any)
	if logs["errors"] != float64(2) || logs["warnings"] != float64(6) || report["status"] != "errors" {
		t.Errorf("counts = %v errors, %v warnings, status %v; want 2, 6, errors", logs["errors"], logs["warnings"], report["status"])
	}
	errs, _ := json.Marshal(logs["last_errors"])
	if !strings.Contains(string(errs), "database is locked") || !strings.Contains(string(errs), "Host terminated") {
		t.Errorf("last_errors = %s, want both errors with the exception message", errs)
	}
	if warns, _ := logs["last_warnings"].([]any); len(warns) != jf.HealthCheckMaxIssues {
		t.Errorf("last_warnings = %v, want the last %d", warns, jf.HealthCheckMaxIssues)
	}
}
