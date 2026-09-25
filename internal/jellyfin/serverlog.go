package jellyfin

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// LogEntry is one entry of a Jellyfin server log: the line that carries its
// time and level, and the lines after it up to the next entry, which hold an
// exception's message and stack trace.
type LogEntry struct {
	Time   time.Time // zero without a header, or when the log template is not Jellyfin's default
	Level  string    // VRB, DBG, INF, WRN, ERR, or FTL; empty without a header
	Header string
	Detail []string
}

// An entry's first line carries its level as a tag such as [ERR] near the
// start. Jellyfin's default template writes
// [2026-09-24 13:37:36.636 -07:00] [ERR] [106] Source: message, and an
// administrator can change the template in logging.json, so the level is
// found wherever it sits in the first logLevelWithin bytes, and the time is
// read only from the default layout.
var (
	logLevel = regexp.MustCompile(`\[(VRB|DBG|INF|WRN|ERR|FTL)\]`)
	logTime  = regexp.MustCompile(`^\[(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:\.\d+)? [+-]\d\d:\d\d)\]`)
)

const (
	logLevelWithin = 64
	logTimeLayout  = "2006-01-02 15:04:05.999999999 -07:00"
)

// ParseServerLog splits a server log into its entries, oldest first. Lines
// before the first header form an entry of their own with no level.
func ParseServerLog(content string) []LogEntry {
	var entries []LogEntry
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if m := logLevel.FindStringSubmatchIndex(line); m != nil && m[0] < logLevelWithin {
			e := LogEntry{Level: line[m[2]:m[3]], Header: line}
			if tm := logTime.FindStringSubmatch(line); tm != nil {
				e.Time, _ = time.Parse(logTimeLayout, tm[1])
			}
			entries = append(entries, e)
			continue
		}
		if len(entries) == 0 {
			if strings.TrimSpace(line) == "" {
				continue
			}
			entries = append(entries, LogEntry{})
		}
		last := &entries[len(entries)-1]
		if last.Header == "" && len(last.Detail) == 0 {
			last.Header = line
			continue
		}
		last.Detail = append(last.Detail, line)
	}
	return entries
}

// IsError reports whether the entry is an error or a fatal error.
func (e LogEntry) IsError() bool { return e.Level == "ERR" || e.Level == "FTL" }

// IsWarning reports whether the entry is a warning.
func (e LogEntry) IsWarning() bool { return e.Level == "WRN" }

// Text renders the entry with its detail lines. Each run of stack frames, the
// detail lines that begin with "at ", keeps its first maxFrames frames, and a
// line then says how many were left out; the exception messages between runs
// are all kept.
func (e LogEntry) Text(maxFrames int) string {
	var b strings.Builder
	b.WriteString(e.Header)
	frames, skipped := 0, 0
	flush := func() {
		if skipped > 0 {
			fmt.Fprintf(&b, "\n   (%d more stack frames left out)", skipped)
		}
		frames, skipped = 0, 0
	}
	for _, line := range e.Detail {
		if strings.HasPrefix(strings.TrimSpace(line), "at ") {
			frames++
			if frames > maxFrames {
				skipped++
				continue
			}
		} else {
			flush()
		}
		b.WriteString("\n")
		b.WriteString(line)
	}
	flush()
	return b.String()
}

// Message is the entry's header followed by its first detail line, which for
// an error is the exception's type and message.
func (e LogEntry) Message() string {
	if len(e.Detail) == 0 || strings.TrimSpace(e.Detail[0]) == "" {
		return e.Header
	}
	return e.Header + " " + strings.TrimSpace(e.Detail[0])
}
