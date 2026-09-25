package tools

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// ToolsetMap maps toolset names to the tool names they contain.
// Used with --toolsets flag to enable groups of related tools.
var ToolsetMap = map[string][]string{
	"discovery": {
		"jellyfin_libraries",
		"jellyfin_search",
		"jellyfin_browse",
		"jellyfin_get_item",
		"jellyfin_recommendations",
		"jellyfin_item_extras",
		"jellyfin_download_link",
	},
	"media": {
		"jellyfin_tv_shows",
		"jellyfin_music",
		"jellyfin_people",
	},
	"user": {
		"jellyfin_user_data",
		"jellyfin_playlists",
		"jellyfin_collections",
	},
	"playback": {
		"jellyfin_sessions",
		"jellyfin_playback_control",
		"jellyfin_play",
	},
	"admin": {
		"jellyfin_system_info",
		"jellyfin_system_control",
		"jellyfin_users",
		"jellyfin_library_manage",
		"jellyfin_tasks",
		"jellyfin_plugins",
		"jellyfin_devices",
		"jellyfin_server",
	},
	"content": {
		"jellyfin_metadata",
		"jellyfin_subtitles_lyrics",
		"jellyfin_images",
		"jellyfin_videos",
	},
	"livetv": {
		"jellyfin_live_tv",
		"jellyfin_recordings",
	},
	"analytics": {
		"jellyfin_analytics",
	},
}

// Shared tool annotation presets used by all register*Tools functions.
var (
	AnnotReadOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: jf.BoolPtr(false)}
	AnnotWriteOp     = &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, OpenWorldHint: jf.BoolPtr(false)}
	AnnotWriteCreate = &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: jf.BoolPtr(false)}
	AnnotDestructive = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: jf.BoolPtr(true), OpenWorldHint: jf.BoolPtr(false)}
)

// BuildToolFilter creates the enabled() callback used by all register*Tools functions.
// It supports two filtering dimensions:
//   - toolsets: comma-separated toolset names (e.g. "discovery,media,playback")
//   - readOnly: when true, only tools with ReadOnlyHint=true are registered
//
// Destructive actions are not filtered here: every tool stays registered, and
// DestructivePolicy makes each destructive action refuse instead.
func BuildToolFilter(toolsets string, readOnly bool) func(string, *mcp.ToolAnnotations) bool {
	// Build the set of allowed tool names from toolsets
	var allowed map[string]bool
	if toolsets != "" {
		allowed = make(map[string]bool)
		for _, ts := range strings.Split(toolsets, ",") {
			ts = strings.TrimSpace(ts)
			if ts == "" {
				continue
			}
			tools, ok := ToolsetMap[ts]
			if !ok {
				valid := make([]string, 0, len(ToolsetMap))
				for name := range ToolsetMap {
					valid = append(valid, name)
				}
				log.Printf("WARNING: unknown toolset %q (valid: %s)", ts, strings.Join(valid, ", "))
				continue
			}
			for _, t := range tools {
				allowed[t] = true
			}
		}
	}

	return func(name string, annotations *mcp.ToolAnnotations) bool {
		// Toolset filter: if toolsets specified, tool must be in an enabled set
		if allowed != nil && !allowed[name] {
			return false
		}

		// Annotation-based filters
		if annotations == nil {
			// Tools with no annotations are not read-only and not destructive
			return !readOnly
		}
		if readOnly && !annotations.ReadOnlyHint {
			return false
		}

		return true
	}
}

// DestructivePolicy is the receiving middleware for --disable-destructive.
// When disabled is true, every tool call runs in a context in which
// jf.DestructiveGate refuses the action, so a tool's destructive actions are
// refused while its other actions keep working.
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

// ConfirmationNote is receiving middleware that prefixes the result of a call
// the user confirmed through the confirmation form with "Confirmed by the
// user.", so the model can report the confirmation instead of guessing
// whether the form was shown.
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
}

// moreNote says that a list stops short of everything that matches: how many
// it shows of the total, and how to see the rest. It returns nil when the list
// is complete. narrow names the inputs that shrink the match when the limit is
// already at its maximum; it is empty when no input narrows the list.
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

// noteText joins notes for a text result's header line, each after a space.
func noteText(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return " " + strings.Join(notes, " ")
}
