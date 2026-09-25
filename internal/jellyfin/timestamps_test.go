package jellyfin

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // zone rules on machines without a zone database
)

// TestMain runs the package's tests in UTC, so results that depend on the
// server's time zone do not depend on the machine running them. Tests about
// the zone set their own with inZone.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// inZone sets the server's time zone for the rest of the test.
func inZone(t *testing.T, loc *time.Location) {
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2024-01-31T18:00:00.1234567Z", time.Date(2024, 1, 31, 18, 0, 0, 123456700, time.UTC), true},
		{"2024-01-31T18:00:00Z", time.Date(2024, 1, 31, 18, 0, 0, 0, time.UTC), true},
		{"2024-01-31T20:00:00+02:00", time.Date(2024, 1, 31, 18, 0, 0, 0, time.UTC), true},
		{"2024-01-31T18:00:00", time.Date(2024, 1, 31, 18, 0, 0, 0, time.UTC), true},
		{"2024-01-31", time.Time{}, false},
		{"", time.Time{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseTime(tt.in)
		if ok != tt.ok || !got.Equal(tt.want) {
			t.Errorf("ParseTime(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// Instants are shown in the server's time zone: date-times with their
// offset, dates as the local calendar day.
func TestLocalTimestamps(t *testing.T) {
	tests := []struct {
		name     string
		zone     *time.Location
		in       string
		dateTime string
		date     string
	}{
		{"west of UTC, the local day is earlier", time.FixedZone("UTC-7", -7*60*60), "2026-03-05T03:30:00.1234567Z", "2026-03-04T20:30:00-07:00", "2026-03-04"},
		{"east of UTC, the local day is later", time.FixedZone("UTC+9", 9*60*60), "2026-03-04T18:00:00Z", "2026-03-05T03:00:00+09:00", "2026-03-05"},
		{"UTC keeps its Z", time.UTC, "2026-03-04T18:00:00Z", "2026-03-04T18:00:00Z", "2026-03-04"},
		{"an offset-free value is UTC", time.FixedZone("UTC-7", -7*60*60), "2026-03-05T03:30:00", "2026-03-04T20:30:00-07:00", "2026-03-04"},
		{"the unset date is left as written", time.FixedZone("UTC-7", -7*60*60), "0001-01-01T00:00:00.0000000Z", "0001-01-01T00:00:00", "0001-01-01"},
		{"an unreadable value is cut", time.FixedZone("UTC-7", -7*60*60), "2026-03-05 03:30:00.000", "2026-03-05 03:30:00", "2026-03-05"},
		{"empty stays empty", time.FixedZone("UTC-7", -7*60*60), "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inZone(t, tt.zone)
			if got := LocalDateTime(tt.in); got != tt.dateTime {
				t.Errorf("LocalDateTime(%q) = %q, want %q", tt.in, got, tt.dateTime)
			}
			if got := LocalDate(tt.in); got != tt.date {
				t.Errorf("LocalDate(%q) = %q, want %q", tt.in, got, tt.date)
			}
		})
	}
}

// PremiereDate and EndDate are calendar dates stored as midnight UTC, so they
// keep their day in every zone, while last_played is an instant and moves to
// the local day.
func TestDetailedItemFrom_DatesInTheServerZone(t *testing.T) {
	inZone(t, time.FixedZone("UTC-7", -7*60*60))
	item := DetailedItemFrom(map[string]any{
		"Id":           "z1",
		"Name":         "Zone Test",
		"Type":         "Series",
		"PremiereDate": "2024-06-15T00:00:00.0000000Z",
		"EndDate":      "2024-09-01T00:00:00.0000000Z",
		"UserData":     map[string]any{"Played": true, "LastPlayedDate": "2024-06-16T02:00:00.0000000Z"},
	})
	if got := item.PremiereDate; got != "2024-06-15" {
		t.Errorf("premiere_date = %v, want 2024-06-15", got)
	}
	if got := item.EndDate; got != "2024-09-01" {
		t.Errorf("end_date = %v, want 2024-09-01", got)
	}
	if item.UserData == nil {
		t.Fatal("user_data missing")
	}
	if got := item.UserData.LastPlayed; got != "2024-06-15" {
		t.Errorf("last_played = %v, want 2024-06-15", got)
	}
}

// Local times across a daylight saving change sort by the instant they name.
// In Los Angeles on 2026-11-01, 01:10 PST is later than 01:30 PDT.
func TestNewerFirst(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	inZone(t, loc)
	pdt := LocalDateTime("2026-11-01T08:30:00Z")
	pst := LocalDateTime("2026-11-01T09:10:00Z")
	if pdt != "2026-11-01T01:30:00-07:00" || pst != "2026-11-01T01:10:00-08:00" {
		t.Fatalf("fixtures %s and %s", pdt, pst)
	}
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{pst, pdt, true},
		{pdt, pst, false},
		{pdt, "unreadable", true},
		{"unreadable", pdt, false},
		{"b", "a", true},
	} {
		if got := NewerFirst(tt.a, tt.b); got != tt.want {
			t.Errorf("NewerFirst(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// Only the named instants move to the server's zone, nested or in lists, and
// a Utc key loses its suffix; a calendar date beside them keeps its day.
func TestLocalizeInstants(t *testing.T) {
	inZone(t, time.FixedZone("UTC-5", -5*60*60))
	v := map[string]any{
		"LastExecutionResult": map[string]any{"StartTimeUtc": "2026-01-02T03:04:05.1234567Z", "Status": "Completed"},
		"Items": []any{
			map[string]any{"StartDate": "2026-01-02T03:00:00Z", "PremiereDate": "2026-01-02T00:00:00Z"},
			map[string]any{"StartDate": "0001-01-01T00:00:00Z"},
		},
	}
	LocalizeInstants(v, "StartTimeUtc", "StartDate")
	le := v["LastExecutionResult"].(map[string]any)
	if _, ok := le["StartTimeUtc"]; ok || le["StartTime"] != "2026-01-01T22:04:05-05:00" {
		t.Errorf("task result = %v, want StartTime in UTC-5 and no StartTimeUtc", le)
	}
	items := v["Items"].([]any)
	first := items[0].(map[string]any)
	if first["StartDate"] != "2026-01-01T22:00:00-05:00" || first["PremiereDate"] != "2026-01-02T00:00:00Z" {
		t.Errorf("program = %v, want StartDate converted and PremiereDate untouched", first)
	}
	if unset := items[1].(map[string]any)["StartDate"]; unset != "0001-01-01T00:00:00Z" {
		t.Errorf("unset date = %v, want it left as written", unset)
	}
}
