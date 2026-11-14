package tools

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// ToolAccess represents a 3-bit capability lattice per tool:
// AccessRead (read-only views), AccessCurate (non-admin library curation), and
// AccessWrite (full write or admin operations).
type ToolAccess uint8

const (
	AccessNone ToolAccess = 0
	AccessRead ToolAccess = 1 << iota
	AccessCurate
	AccessWrite
	AccessFull = AccessRead | AccessCurate | AccessWrite
)

// ToolsetMap maps canonical <verb>-<noun> toolset names to the tools they expose.
// Used with --toolsets and HTTP /mcp?toolsets= to enable groups of related tools.
// Groups prefixed with find- or monitor- register strictly read-only projections.
var ToolsetMap = map[string][]string{
	"find-media": {
		"jellyfin_libraries",
		"jellyfin_search",
		"jellyfin_browse",
		"jellyfin_get_item",
		"jellyfin_recommendations",
		"jellyfin_item_extras",
		"jellyfin_tv_shows",
		"jellyfin_music",
		"jellyfin_analytics",
		"jellyfin_user_data",
		"jellyfin_playlists",
		"jellyfin_metadata",
		"jellyfin_subtitles_lyrics",
		"jellyfin_images",
	},
	"monitor-media": {
		"jellyfin_playback_stats",
		"jellyfin_sessions",
	},
	"monitor-server": {
		"jellyfin_system_info",
		"jellyfin_sessions",
		"jellyfin_server",
		"jellyfin_users",
		"jellyfin_devices",
		"jellyfin_plugins",
		"jellyfin_library_manage",
	},
	"my-media": {
		"jellyfin_user_data",
		"jellyfin_playlists",
	},
	"control-playback": {
		"jellyfin_sessions",
		"jellyfin_playback",
	},
	"manage-media": {
		"jellyfin_collections",
		"jellyfin_metadata",
		"jellyfin_subtitles_lyrics",
		"jellyfin_images",
		"jellyfin_library_manage",
	},
	"admin-server": {
		"jellyfin_system_info",
		"jellyfin_server",
		"jellyfin_users",
		"jellyfin_library_manage",
		"jellyfin_plugins",
		"jellyfin_devices",
	},
	"live-tv": {
		"jellyfin_live_tv",
	},
}

// ToolsetAliases maps legacy single-noun toolset names to canonical <verb>-<noun>
// toolsets so existing --toolsets configurations continue working unchanged.
var ToolsetAliases = map[string]string{
	"discovery":     "find-media",
	"media":         "find-media",
	"user":          "my-media",
	"playback":      "control-playback",
	"content":       "manage-media",
	"analytics":     "monitor-media",
	"admin":         "admin-server",
	"manage-server": "admin-server",
	"livetv":        "live-tv",
}

var canonicalToolsetAccess = map[string]map[string]ToolAccess{
	"find-media": {
		"jellyfin_libraries":        AccessRead,
		"jellyfin_search":           AccessRead,
		"jellyfin_browse":           AccessRead,
		"jellyfin_get_item":         AccessRead,
		"jellyfin_recommendations":  AccessRead,
		"jellyfin_item_extras":      AccessRead,
		"jellyfin_tv_shows":         AccessRead,
		"jellyfin_music":            AccessRead,
		"jellyfin_analytics":        AccessRead,
		"jellyfin_user_data":        AccessRead,
		"jellyfin_playlists":        AccessRead,
		"jellyfin_metadata":         AccessRead,
		"jellyfin_subtitles_lyrics": AccessRead,
		"jellyfin_images":           AccessRead,
	},
	"monitor-media": {
		"jellyfin_playback_stats": AccessRead,
		"jellyfin_sessions":       AccessRead,
	},
	"monitor-server": {
		"jellyfin_system_info":    AccessRead,
		"jellyfin_sessions":       AccessRead,
		"jellyfin_server":         AccessRead,
		"jellyfin_users":          AccessRead,
		"jellyfin_devices":        AccessRead,
		"jellyfin_plugins":        AccessRead,
		"jellyfin_library_manage": AccessRead,
	},
	"my-media": {
		"jellyfin_user_data": AccessFull,
		"jellyfin_playlists": AccessFull,
	},
	"control-playback": {
		"jellyfin_sessions": AccessRead,
		"jellyfin_playback": AccessFull,
	},
	"manage-media": {
		"jellyfin_collections":      AccessFull,
		"jellyfin_metadata":         AccessFull,
		"jellyfin_subtitles_lyrics": AccessFull,
		"jellyfin_images":           AccessFull,
		"jellyfin_library_manage":   AccessCurate,
	},
	"admin-server": {
		"jellyfin_system_info":    AccessRead,
		"jellyfin_server":         AccessFull,
		"jellyfin_users":          AccessFull,
		"jellyfin_library_manage": AccessFull,
		"jellyfin_plugins":        AccessFull,
		"jellyfin_devices":        AccessFull,
	},
	"live-tv": {
		"jellyfin_live_tv": AccessFull,
	},
}

var pureWriteTools = map[string]bool{
	"jellyfin_playback":    true,
	"jellyfin_collections": true,
}

// Shared tool annotation presets used by all register*Tools functions.
// DestructiveHint and OpenWorldHint are set explicitly on every preset so clients
// never have to guess from omitted pointer defaults.
var (
	AnnotReadOnly             = &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: jf.BoolPtr(false), IdempotentHint: true, OpenWorldHint: jf.BoolPtr(false)}
	AnnotReadOnlyOpenWorld    = &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: jf.BoolPtr(false), IdempotentHint: true, OpenWorldHint: jf.BoolPtr(true)}
	AnnotWriteOp              = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(false), IdempotentHint: true, OpenWorldHint: jf.BoolPtr(false)}
	AnnotWriteCreate          = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(false), IdempotentHint: false, OpenWorldHint: jf.BoolPtr(false)}
	AnnotWriteOpenWorld       = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(false), IdempotentHint: true, OpenWorldHint: jf.BoolPtr(true)}
	AnnotDestructive          = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(true), OpenWorldHint: jf.BoolPtr(false)}
	AnnotDestructiveOpenWorld = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(true), OpenWorldHint: jf.BoolPtr(true)}

	// annotCurateProbe is an internal probe passed to enabled() by
	// RegisterAdminLibraryTools to detect AccessCurate without changing
	// Register*Tools callback signatures.
	annotCurateProbe = &mcp.ToolAnnotations{Title: "curate"}
)

// ResolveToolAccess resolves comma-separated toolset names (canonical or legacy
// alias) into a per-tool ToolAccess map and returns any unrecognized toolset
// names in unknown. An empty toolsets string grants every tool its full access.
// When readOnly is true, every entry is masked to AccessRead.
func ResolveToolAccess(toolsets string, readOnly bool) (map[string]ToolAccess, []string) {
	access := make(map[string]ToolAccess)
	var unknown []string

	if strings.TrimSpace(toolsets) == "" {
		for _, grants := range canonicalToolsetAccess {
			for tool, acc := range grants {
				access[tool] |= acc
			}
		}
	} else {
		for _, raw := range strings.Split(toolsets, ",") {
			ts := strings.TrimSpace(raw)
			if ts == "" {
				continue
			}
			canonical := strings.ToLower(ts)
			if aliasTarget, isAlias := ToolsetAliases[canonical]; isAlias {
				if canonical == "analytics" {
					access["jellyfin_analytics"] |= AccessRead
				}
				canonical = aliasTarget
			}
			grants, ok := canonicalToolsetAccess[canonical]
			if !ok {
				unknown = append(unknown, ts)
				continue
			}
			for tool, acc := range grants {
				access[tool] |= acc
			}
		}
	}

	if readOnly {
		for tool, acc := range access {
			masked := acc & AccessRead
			if masked == AccessNone || pureWriteTools[tool] {
				delete(access, tool)
			} else {
				access[tool] = masked
			}
		}
	}
	return access, unknown
}

// NarrowToolAccess intersects two ToolAccess maps with bitwise AND per tool so
// per-request query parameters can only narrow, never widen, base server access.
func NarrowToolAccess(base, narrow map[string]ToolAccess) map[string]ToolAccess {
	out := make(map[string]ToolAccess, len(base))
	for tool, baseAcc := range base {
		acc := baseAcc & narrow[tool]
		if pureWriteTools[tool] && acc&AccessWrite == 0 {
			continue
		}
		if acc != AccessNone {
			out[tool] = acc
		}
	}
	return out
}

// HasWriteAccess reports whether any tool in access retains curate or write capability.
func HasWriteAccess(access map[string]ToolAccess) bool {
	for tool, acc := range access {
		if pureWriteTools[tool] {
			if acc&AccessWrite != 0 {
				return true
			}
			continue
		}
		if acc&(AccessCurate|AccessWrite) != 0 {
			return true
		}
	}
	return false
}

// FilterFromAccess builds the enabled(name, annotations) callback from a
// resolved ToolAccess map.
func FilterFromAccess(access map[string]ToolAccess) func(string, *mcp.ToolAnnotations) bool {
	return func(name string, annotations *mcp.ToolAnnotations) bool {
		acc := access[name]
		if acc == AccessNone {
			return false
		}
		if annotations == annotCurateProbe {
			return acc&AccessCurate != 0
		}
		if annotations == nil {
			return acc&AccessWrite != 0
		}
		if annotations.ReadOnlyHint {
			return acc&AccessRead != 0
		}
		return acc&AccessWrite != 0
	}
}

// LogUnknownToolsets logs a warning for each unrecognized toolset name.
func LogUnknownToolsets(unknown []string) {
	if len(unknown) == 0 {
		return
	}
	valid := make([]string, 0, len(ToolsetMap))
	for name := range ToolsetMap {
		valid = append(valid, name)
	}
	slices.Sort(valid)
	validList := strings.Join(valid, ", ")
	for _, ts := range unknown {
		log.Printf("WARNING: unknown toolset %q (valid: %s)", ts, validList)
	}
}

// BuildToolFilter creates the enabled() callback used by all register*Tools functions.
//   - toolsets: comma-separated toolset names (such as "find-media,monitor-media")
//   - readOnly: when true, only tools or mixed-tool read-only projections with ReadOnlyHint=true are registered
func BuildToolFilter(toolsets string, readOnly bool) func(string, *mcp.ToolAnnotations) bool {
	if strings.TrimSpace(toolsets) == "" && !readOnly {
		return func(string, *mcp.ToolAnnotations) bool {
			return true
		}
	}
	access, unknown := ResolveToolAccess(toolsets, readOnly)
	LogUnknownToolsets(unknown)
	return FilterFromAccess(access)
}

// toolMode determines whether a mixed read/write tool is enabled in full mode
// or in read-only mode (exposing only its read-only actions and inputs).
func toolMode(enabled func(string, *mcp.ToolAnnotations) bool, name string, fullAnnot, roAnnot *mcp.ToolAnnotations) (ok bool, readOnly bool, annot *mcp.ToolAnnotations) {
	if enabled(name, fullAnnot) {
		return true, false, fullAnnot
	}
	if roAnnot != nil && enabled(name, roAnnot) {
		return true, true, roAnnot
	}
	return false, false, nil
}

// requireReadOnlyAction guards mixed tool handlers when registered in read-only mode.
func requireReadOnlyAction(action string, allowed []any) *mcp.CallToolResult {
	return requireAllowedAction(action, allowed, "read-only mode (allowed read-only actions")
}

func requireAllowedAction(action string, allowed []any, modeLabel string) *mcp.CallToolResult {
	for _, a := range allowed {
		if s, ok := a.(string); ok && s == action {
			return nil
		}
	}
	names := make([]string, 0, len(allowed))
	for _, a := range allowed {
		if s, ok := a.(string); ok {
			names = append(names, s)
		}
	}
	slices.Sort(names)
	return jf.ErrResult("Action '%s' is disabled in %s: %s).", action, modeLabel, strings.Join(names, ", "))
}

// DestructivePolicy is the receiving middleware for --disable-destructive.
func DestructivePolicy(disabled bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if disabled && method == "tools/call" {
				ctx = jf.WithDestructiveDisabled(ctx)
			}
			return next(ctx, method, req)
		}
	}
}

// ConfirmationLedgerMiddleware attaches l to every tools/call context so
// non-elicitation clients require a prior CONFIRMATION REQUIRED preview before
// accepting confirm=true.
func ConfirmationLedgerMiddleware(l *jf.ConfirmationLedger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if l != nil && method == "tools/call" {
				ctx = jf.WithConfirmationLedger(ctx, l)
			}
			return next(ctx, method, req)
		}
	}
}

// ConfirmationNote prefixes the result of a form-confirmed call with "Confirmed by the user."
func ConfirmationNote() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			ctx = jf.WithConfirmationTracking(ctx)
			res, err := next(ctx, method, req)
			if err != nil || !jf.ConfirmedInCall(ctx) {
				return res, err
			}
			if result, ok := res.(*mcp.CallToolResult); ok && !result.IsError && len(result.Content) > 0 {
				if text, ok := result.Content[0].(*mcp.TextContent); ok && text.Text != jf.DeclinedText {
					text.Text = "Confirmed by the user. " + text.Text
				}
			}
			return res, err
		}
	}
}

func RegisterTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {
	RegisterDiscoveryTools(server, client, enabled)
	RegisterMediaTools(server, client, enabled)
	RegisterUserTools(server, client, enabled)
	RegisterPlaybackTools(server, client, enabled)
	RegisterAdminSystemTools(server, client, enabled)
	RegisterAdminUserTools(server, client, enabled)
	RegisterAdminLibraryTools(server, client, enabled)
	RegisterAdminBackendTools(server, client, enabled)
	RegisterLiveTVTools(server, client, enabled)
	RegisterContentTools(server, client, enabled)
	RegisterAnalyticsTools(server, client, enabled)
	RegisterPlaybackStatsTools(server, client, enabled)
}

func moreNote(shown, total, limit int, narrow string) []string {
	if total <= shown {
		return nil
	}
	if limit < jf.MaxLimitCap {
		return []string{fmt.Sprintf("Showing %d of %d. Increase limit (currently %d, at most %d) to see more.", shown, total, limit, jf.MaxLimitCap)}
	}
	if narrow == "" {
		return []string{fmt.Sprintf("Showing %d of %d; the limit is at its maximum of %d.", shown, total, jf.MaxLimitCap)}
	}
	return []string{fmt.Sprintf("Showing %d of %d; the limit is at its maximum of %d, so %s to see the rest.", shown, total, jf.MaxLimitCap, narrow)}
}

func noteText(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return " " + strings.Join(notes, " ")
}
