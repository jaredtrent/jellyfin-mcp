package jellyfin

import (
	"strings"
	"testing"
	"time"
)

const sampleServerLog = `[2026-01-02 00:55:00.067 -07:00] [INF] [12] Main: Startup complete
[2026-01-02 00:55:01.100 -07:00] [ERR] [76] Api.ExceptionMiddleware: Error processing request. URL "GET" "/Items".
Sample.DatabaseException (0x80004005): Database Error 5: 'database is locked'.
   at Sample.Db.Throw(Int32 rc)
   at Sample.Db.Step()
   at Sample.Db.Read()
   at Sample.Db.Query()
 ---> Sample.InnerException: inner reason
   at Sample.Inner.Run()
[2026-01-02 00:56:00.000 -07:00] [WRN] [40] Net.WebSocket: connection closed early
[2026-01-02 00:57:00.000 -07:00] [FTL] [1] Main: Host terminated
`

func TestParseServerLog(t *testing.T) {
	entries := ParseServerLog(sampleServerLog)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}
	if e := entries[1]; e.Level != "ERR" || len(e.Detail) != 7 || !e.IsError() {
		t.Errorf("error entry = %+v, want ERR with 7 detail lines", e)
	}
	if !entries[3].IsError() || !entries[2].IsWarning() || entries[0].IsError() {
		t.Error("FTL must count as an error, WRN as a warning, and INF as neither")
	}
	want := time.Date(2026, 1, 2, 7, 55, 1, 100_000_000, time.UTC)
	if !entries[1].Time.Equal(want) {
		t.Errorf("time = %v, want %v", entries[1].Time, want)
	}
	if got := entries[1].Message(); !strings.HasSuffix(got, "Database Error 5: 'database is locked'.") {
		t.Errorf("Message = %q, want the header followed by the exception", got)
	}
}

// Text keeps every exception message and the first frames of each stack
// trace, and says how many frames it left out.
func TestLogEntryText(t *testing.T) {
	text := ParseServerLog(sampleServerLog)[1].Text(2)
	for _, want := range []string{"database is locked", "at Sample.Db.Step()", "(2 more stack frames left out)", "inner reason", "at Sample.Inner.Run()"} {
		if !strings.Contains(text, want) {
			t.Errorf("text is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "at Sample.Db.Read()") {
		t.Errorf("text kept a frame past the limit:\n%s", text)
	}
}

// Text before the first header, as in a log cut at a size limit, is kept as
// an entry with no level.
func TestParseServerLog_TextBeforeFirstHeader(t *testing.T) {
	entries := ParseServerLog("   at Leftover.Frame()\n" + sampleServerLog)
	if len(entries) != 5 || entries[0].Level != "" || entries[0].Header != "   at Leftover.Frame()" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

// A log written with a changed template still splits into entries by level,
// with no time.
func TestParseServerLog_CustomTemplate(t *testing.T) {
	entries := ParseServerLog("12:00:01 [WRN] slow response\n12:00:02 [ERR] failed\nSome.Exception: reason\n")
	if len(entries) != 2 || !entries[0].IsWarning() || !entries[1].IsError() || len(entries[1].Detail) != 1 || !entries[1].Time.IsZero() {
		t.Errorf("entries = %+v", entries)
	}
}
