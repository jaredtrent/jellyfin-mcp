package jellyfin

// Jellyfin uses .NET ticks (100-nanosecond intervals) for durations.
const (
	TicksPerSecond int64 = 10_000_000
	TicksPerMinute int64 = 600_000_000
)

// Size and rate unit divisors.
const (
	UnitsPerKilo int64 = 1_000     // base-10 kilo divisor (bytes→KB, bps→kbps)
	BytesPerMB   int64 = 1_000_000 // base-10, so a size_mb field is megabytes as labeled
	BytesPerGB   int64 = 1_073_741_824
)

// LowStorageThreshold is the free-space threshold (100 GB) below which
// storage is flagged as low in health checks.
const LowStorageThreshold int64 = 107_374_182_400

// Truncation lengths for Jellyfin datetime strings.
const (
	DateOnlyLen = 10 // "2006-01-02"
	DateTimeLen = 19 // "2006-01-02T15:04:05"
)

// Go reference-time formats for time.Parse / time.Format.
const (
	DateOnlyFormat = "2006-01-02"
	DateTimeFormat = "2006-01-02T15:04:05"
)

// Content truncation limits.
const (
	OverviewMaxLen  = 200 // standard item summaries (search, browse, activity log)
	SummaryMaxLen   = 150 // compact list entries (episodes, plugin descriptions)
	ErrorBodyMaxLen = 500
)

// MaxPeopleInDetail caps the number of cast/crew entries in detail views.
const MaxPeopleInDetail = 15

// Pagination defaults for fetchAllPages.
const (
	DefaultPageSize = 200
	DefaultMaxItems = 2000
)

// MaxLimitCap is the upper bound applied to user-supplied limit parameters
// to prevent excessive memory allocation or API abuse.
const MaxLimitCap = 500

// ActivityLogLookback is the max activity log entries to scan for playback verification.
const ActivityLogLookback = 2000

// Token masking thresholds.
const (
	TokenMaskMinLen  = 12 // tokens this short or shorter are shown in full
	TokenRevealChars = 4  // number of chars to show at start/end of masked tokens
)

// BackupStaleDays is how old a backup can be before health_check warns.
const BackupStaleDays = 7

// HealthCheckMaxIssues caps how many recent errors, and separately how many
// recent warnings, health_check shows.
const HealthCheckMaxIssues = 5

// log_file limits. A result holds at most LogFileMaxEntries entries and about
// LogFileMaxChars characters, which an MCP client can show in full; each run
// of stack frames in an entry keeps its first LogFileStackFrames frames.
const (
	LogFileDefaultEntries = 100
	LogFileMaxEntries     = 500
	LogFileMaxChars       = 48_000
	LogFileStackFrames    = 3
)

// MaxResponseBodyBytes caps the amount of data read from a single Jellyfin API
// response to prevent unbounded memory allocation (50 MB).
const MaxResponseBodyBytes int64 = 50 * 1024 * 1024
