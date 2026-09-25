package jellyfin

import (
	"strings"
	"time"
)

// ParseTime parses a date-time as Jellyfin writes it: ISO 8601 in UTC with up
// to seven fractional digits and a Z suffix, such as
// 2024-01-31T18:00:00.1234567Z. A value without an offset is read as UTC.
func ParseTime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(DateTimeFormat, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// LocalDateTime formats a Jellyfin instant in the server's time zone, the one
// the server instructions state, as an RFC 3339 timestamp with its offset,
// such as 2024-01-31T00:05:00-05:00. The result is also a valid date input to
// the tools. A value that does not parse is cut to its date and time.
func LocalDateTime(s string) string {
	t, ok := parseInstant(s)
	if !ok {
		return Truncate(s, DateTimeLen)
	}
	return t.In(time.Local).Format(time.RFC3339)
}

// LocalDate formats a Jellyfin instant as its calendar date in the server's
// time zone, the zone date inputs such as min_date_created are read in. A
// value that does not parse is cut to its date.
//
// Calendar dates that Jellyfin stores as midnight UTC, such as PremiereDate
// and EndDate, are not instants and must not go through LocalDate: west of UTC
// they would move to the previous day.
func LocalDate(s string) string {
	t, ok := parseInstant(s)
	if !ok {
		return Truncate(s, DateOnlyLen)
	}
	return t.In(time.Local).Format(DateOnlyFormat)
}

// NewerFirst reports whether timestamp a is later than b, for sorting
// timestamps newest first. They are compared as instants, because the local
// times LocalDateTime writes change their offset with daylight saving time
// and then no longer sort as strings. A value that does not parse sorts after
// every value that does.
func NewerFirst(a, b string) bool {
	ta, okA := ParseTime(a)
	tb, okB := ParseTime(b)
	if okA != okB {
		return okA
	}
	if !okA {
		return a > b
	}
	return ta.After(tb)
}

// parseInstant is ParseTime for values to convert to local time. Jellyfin
// writes an unset date as 0001-01-01T00:00:00Z, which is left as written
// rather than moved to a time before year 1.
func parseInstant(s string) (time.Time, bool) {
	t, ok := ParseTime(s)
	if !ok || t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}

// LocalizeInstants rewrites, anywhere in a decoded Jellyfin response, the
// values of the named keys from UTC instants to the server's time zone, as
// LocalDateTime does, and returns v. A key ending in Utc, such as a task
// result's StartTimeUtc, is renamed without the suffix, because its value is
// no longer UTC.
//
// Only the named keys change. Calendar dates that Jellyfin stores as midnight
// UTC, such as an item's PremiereDate, sit beside instants in the same
// objects and must keep their day, so a caller names the instants it returns.
func LocalizeInstants(v any, keys ...string) any {
	named := make(map[string]bool, len(keys))
	for _, k := range keys {
		named[k] = true
	}
	localizeInstants(v, named)
	return v
}

func localizeInstants(v any, named map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		converted := map[string]string{}
		for k, val := range v {
			if s, ok := val.(string); ok && named[k] {
				if _, ok := parseInstant(s); ok {
					converted[k] = LocalDateTime(s)
				}
				continue
			}
			localizeInstants(val, named)
		}
		for k, local := range converted {
			delete(v, k)
			v[strings.TrimSuffix(k, "Utc")] = local
		}
	case []any:
		for _, e := range v {
			localizeInstants(e, named)
		}
	case []map[string]any:
		for _, e := range v {
			localizeInstants(e, named)
		}
	}
}
