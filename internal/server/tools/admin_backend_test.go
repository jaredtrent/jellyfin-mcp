package tools_test

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// The recency window compares instants, so a device just inside it is listed
// whatever the server's time zone, and its last activity is shown in that
// zone.
func TestDevices_ListRecencyWindow(t *testing.T) {
	inZone(t, time.FixedZone("UTC+9", 9*60*60))
	now := time.Now().UTC()
	inside := now.Add(-23 * time.Hour)
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			return jsonInto(map[string]any{"Items": []map[string]any{
				{"Id": "dev-inside", "Name": "Test Device 1", "DateLastActivity": inside.Format("2006-01-02T15:04:05.0000000Z")},
				{"Id": "dev-outside", "Name": "Test Device 2", "DateLastActivity": now.Add(-25 * time.Hour).Format("2006-01-02T15:04:05.0000000Z")},
				{"Id": "dev-unknown", "Name": "Test Device 3", "DateLastActivity": ""},
			}}, dest)
		},
	}
	text := resultText(t, callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "list", "days": 1}))
	if !strings.Contains(text, "dev-inside") || strings.Contains(text, "dev-outside") || strings.Contains(text, "dev-unknown") {
		t.Errorf("want only the device active within the last day: %s", text)
	}
	if want := inside.In(time.Local).Format(time.RFC3339); !strings.Contains(text, want) {
		t.Errorf("want last_activity %s in the server's zone: %s", want, text)
	}
}

// backup_manifest shows what a backup holds, in the server's time zone, and
// explains a file that is missing or not a Jellyfin backup.
func TestServer_BackupManifest(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	var sent url.Values
	answer := func(dest any) error {
		return jsonInto(map[string]any{
			"ServerVersion": "12.1.0", "BackupEngineVersion": "0.2.0",
			"DateCreated": "2026-03-05T03:30:00.0000000Z", "Path": "/config/data/backups/jellyfin-backup-1.zip",
			"Options": map[string]any{"Metadata": true, "Trickplay": false, "Subtitles": false, "Database": true},
		}, dest)
	}
	mc := &mockClient{getFunc: func(_ context.Context, endpoint string, params url.Values, dest any) error {
		if endpoint != "/Backup/Manifest" {
			t.Errorf("unexpected endpoint %s", endpoint)
		}
		sent = params
		return answer(dest)
	}}
	result := callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "backup_manifest", "file_name": "jellyfin-backup-1.zip"})
	text := resultText(t, result)
	if result.IsError || sent.Get("path") != "jellyfin-backup-1.zip" {
		t.Fatalf("sent %v: %s", sent, text)
	}
	for _, want := range []string{`"server_version": "12.1.0"`, `"date_created": "2026-03-04T20:30:00-07:00"`, `"database"`, `"metadata"`} {
		if !strings.Contains(text, want) {
			t.Errorf("want %s in: %s", want, text)
		}
	}
	if strings.Contains(text, `"trickplay"`) || strings.Contains(text, `"subtitles"`) {
		t.Errorf("lists contents the backup does not hold: %s", text)
	}

	for _, tt := range []struct {
		name   string
		answer func(dest any) error
		want   string
	}{
		{"not a backup", func(any) error { return nil }, `"jellyfin-backup-1.zip" is not a Jellyfin backup archive.`},
		{"missing", func(any) error { return &jf.APIError{StatusCode: 404} }, `Jellyfin's backup folder has no file named "jellyfin-backup-1.zip".`},
		{"unreadable", func(any) error { return errors.New("API error 500: failed") }, `Failed to read the backup "jellyfin-backup-1.zip": API error 500: failed`},
	} {
		answer = tt.answer
		result := callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "backup_manifest", "file_name": "jellyfin-backup-1.zip"})
		text := resultText(t, result)
		if !result.IsError || !strings.Contains(text, tt.want) {
			t.Errorf("%s: %s", tt.name, text)
		}
		if tt.name != "not a backup" && !strings.Contains(text, "Use 'list_backups' to find backups.") {
			t.Errorf("%s: no pointer to list_backups: %s", tt.name, text)
		}
	}
	result = callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "backup_manifest"})
	if !result.IsError || !strings.Contains(resultText(t, result), "file_name is required") {
		t.Errorf("no file name: %s", resultText(t, result))
	}
}

// create_backup names the backup's contents with the DTO's own keys.
func TestServer_CreateBackupBody(t *testing.T) {
	var body any
	mc := &mockClient{postNoContentFunc: func(_ context.Context, endpoint string, _ url.Values, reqBody any) error {
		body = reqBody
		return nil
	}}
	callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "create_backup"})
	want := map[string]any{"Metadata": true, "Trickplay": true, "Subtitles": true, "Database": true}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body %v, want %v", body, want)
	}
}

// Devices are listed newest first by the instant of their last activity, also
// across a daylight saving change: in Los Angeles on 2026-11-01, 01:10 PST is
// later than 01:30 PDT.
func TestDevices_ListNewestFirstAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	inZone(t, loc)
	mc := &mockClient{getFunc: func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto(map[string]any{"Items": []map[string]any{
			{"Id": "dev-pdt", "Name": "Test Device 1", "DateLastActivity": "2026-11-01T08:30:00.0000000Z"},
			{"Id": "dev-pst", "Name": "Test Device 2", "DateLastActivity": "2026-11-01T09:10:00.0000000Z"},
		}}, dest)
	}}
	text := resultText(t, callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "list"}))
	if i, j := strings.Index(text, "dev-pst"), strings.Index(text, "dev-pdt"); i < 0 || j < 0 || i > j {
		t.Errorf("want dev-pst (01:10 PST) before dev-pdt (01:30 PDT): %s", text)
	}
}

// The livetv configuration section carries tuner URLs and listing-provider
// accounts, which are redacted as the tuners action redacts them.
func TestServer_GetConfigSection_RedactsLiveTVCredentials(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if !strings.EqualFold(endpoint, "/System/Configuration/livetv") {
				t.Errorf("endpoint = %q", endpoint)
			}
			return jsonInto(map[string]any{
				"TunerHosts":       []map[string]any{{"Id": "tuner-1", "Url": "http://account:secret-a@iptv.example.com/get.php?password=secret-b"}},
				"ListingProviders": []map[string]any{{"Id": "provider-1", "Username": "account", "Password": "secret-c", "ListingsId": "lineup-1"}},
				"RecordingPath":    "/data/recordings",
			}, dest)
		},
	}
	result := callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "get_config_section", "key": "LiveTV"})
	text := resultText(t, result)
	if result.IsError {
		t.Fatal(text)
	}
	for _, secret := range []string{"secret-a", "secret-b", "secret-c", "account"} {
		if strings.Contains(text, secret) {
			t.Errorf("result leaks %q: %s", secret, text)
		}
	}
	for _, want := range []string{`"http://iptv.example.com (path, credentials, and query removed)"`, `"Username": "(redacted)"`, `"Password": "(redacted)"`, `"lineup-1"`, `"/data/recordings"`} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in result, got: %s", want, text)
		}
	}
}

// revoke_api_key names the key by its application, because api_keys shows
// tokens masked, and sends the full token to the delete route.
func TestDevices_RevokeAPIKeyByAppName(t *testing.T) {
	keys := []map[string]any{
		{"AccessToken": "tokentokentokentokentokentokenaaaa", "AppName": "App A"},
		{"AccessToken": "tokentokentokentokentokentokenbbbb", "AppName": "App B"},
		{"AccessToken": "tokentokentokentokentokentokencccc", "AppName": "App B"},
	}
	var deleted []string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			if endpoint != "/Auth/Keys" {
				t.Errorf("GET %s", endpoint)
			}
			return jsonInto(map[string]any{"Items": keys}, dest)
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error {
			deleted = append(deleted, endpoint)
			return nil
		},
	}
	result := callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "revoke_api_key", "app_name": "App A", "confirm": true})
	if text := resultText(t, result); result.IsError || !strings.Contains(text, "'App A' revoked") {
		t.Errorf("got %s", text)
	}
	if !slices.Equal(deleted, []string{"/Auth/Keys/tokentokentokentokentokentokenaaaa"}) {
		t.Errorf("deleted %v", deleted)
	}
	for name, want := range map[string]string{"App B": "2 API keys are named", "App Z": "No API key is named"} {
		result := callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "revoke_api_key", "app_name": name, "confirm": true})
		if text := resultText(t, result); !result.IsError || !strings.Contains(text, want) {
			t.Errorf("%s: got %s", name, text)
		}
	}
	if len(deleted) != 1 {
		t.Errorf("ambiguous or unknown names caused deletes: %v", deleted)
	}
	result = callTool(t, mc, "", "jellyfin_devices", map[string]any{"action": "revoke_api_key", "key": "tokentokentokentokentokentokencccc", "confirm": true})
	if text := resultText(t, result); result.IsError || strings.Contains(text, "tokentokentokentokentokentokencccc") {
		t.Errorf("full key: got %s", text)
	}
	if deleted[len(deleted)-1] != "/Auth/Keys/tokentokentokentokentokentokencccc" {
		t.Errorf("deleted %v", deleted)
	}
}

// A livetv section built from get_config_section output carries redaction
// markers, and writing it back would replace the real values.
func TestServer_UpdateConfigSection_RefusesRedactedLiveTV(t *testing.T) {
	posts := 0
	mc := &mockClient{postNoContentFunc: func(context.Context, string, url.Values, any) error { posts++; return nil }}
	redacted := map[string]any{"TunerHosts": []any{map[string]any{"Url": "http://iptv.example.com (path, credentials, and query removed)"}}}
	result := callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "update_config_section", "key": "livetv", "config": redacted, "confirm": true})
	if text := resultText(t, result); !result.IsError || !strings.Contains(text, "redacted values") {
		t.Errorf("got %s", text)
	}
	clean := map[string]any{"TunerHosts": []any{map[string]any{"Url": "http://tuner.example.com:5004"}}}
	result = callTool(t, mc, "", "jellyfin_server", map[string]any{"action": "update_config_section", "key": "livetv", "config": clean, "confirm": true})
	if text := resultText(t, result); result.IsError {
		t.Errorf("clean config refused: %s", text)
	}
	if posts != 1 {
		t.Errorf("%d POSTs, want 1", posts)
	}
}
