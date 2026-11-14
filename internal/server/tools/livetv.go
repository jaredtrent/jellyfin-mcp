package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterLiveTVTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_live_tv ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_live_tv", AnnotDestructive, AnnotReadOnly); ok {
		roActions := []any{"channels", "channel", "programs", "recommended", "guide_info", "tuners", "recordings", "timers", "series_timers"}
		schema := jf.WithEnums[jf.LiveTVInput](map[string][]any{
			"action": {"channels", "channel", "programs", "recommended", "guide_info", "tuners", "recordings", "delete_recording", "timers", "create_timer", "cancel_timer", "series_timers", "create_series_timer", "cancel_series_timer"},
		})
		desc := "Browse Live TV channels, program guide, and tuners, or manage DVR recordings and one-time/series timers."
		if ro {
			schema = jf.WithEnumsFiltered[jf.LiveTVInput](map[string][]any{"action": roActions}, []string{"action", "channel_id", "limit"})
			desc = "Browse Live TV channels, program guide, tuners, DVR recordings, and scheduled timers."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_live_tv",
			Title:       "Live TV & DVR",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.LiveTVInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}

			switch args.Action {
			case "channels":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
				params := url.Values{"UserId": {userID}}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/LiveTv/Channels", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return jf.TextResult(fmt.Sprintf("Live TV channels (%d):%s\n\n%s", len(items), noteText(moreNote(len(items), total, maxItems, "")), jf.FormatJSON(items))), nil, nil

			case "channel":
				if args.ChannelID == "" {
					return jf.ErrResult("channel_id is required. Use 'channels' to find IDs."), nil, nil
				}
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var channel map[string]any
				endpoint := fmt.Sprintf("/LiveTv/Channels/%s", jf.SanitizeID(args.ChannelID))
				params := url.Values{"UserId": {userID}}
				if err := client.Get(ctx, endpoint, params, &channel); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(channel, "StartDate", "EndDate"))), nil, nil

			case "programs":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				maxItems := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
				params := url.Values{"UserId": {userID}}
				if args.ChannelID != "" {
					params.Set("ChannelIds", args.ChannelID)
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/LiveTv/Programs", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return jf.TextResult(fmt.Sprintf("Programs (%d):%s\n\n%s", len(items), noteText(moreNote(len(items), total, maxItems, "pass a channel_id")), jf.FormatJSON(items))), nil, nil

			case "recommended":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
				params := url.Values{"UserId": {userID}}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/LiveTv/Programs/Recommended", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return jf.TextResult(fmt.Sprintf("Recommended programs (%d):%s\n\n%s", len(items), noteText(moreNote(len(items), total, maxItems, "")), jf.FormatJSON(items))), nil, nil

			case "guide_info":
				var info map[string]any
				if err := client.Get(ctx, "/LiveTv/GuideInfo", nil, &info); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(info, "StartDate", "EndDate"))), nil, nil

			case "tuners":
				// Configured tuner hosts are stored in the "livetv" named
				// configuration, which is where the web dashboard reads them.
				// /LiveTv/TunerHosts/Types lists the kinds of tuner the server
				// supports, not the tuners that are set up.
				var config struct {
					TunerHosts []map[string]any `json:"TunerHosts"`
				}
				if err := client.Get(ctx, "/System/Configuration/livetv", nil, &config); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if len(config.TunerHosts) == 0 {
					return jf.TextResult("No tuner hosts are configured."), nil, nil
				}
				for _, host := range config.TunerHosts {
					if u := jf.GetString(host, "Url"); u != "" {
						host["Url"] = redactURL(u)
					}
				}
				return jf.TextResult(fmt.Sprintf("Configured tuner hosts (%d):\n\n%s", len(config.TunerHosts), jf.FormatJSON(config.TunerHosts))), nil, nil

			case "recordings":
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
				var result map[string]any
				if err := client.Get(ctx, "/LiveTv/Recordings", nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ItemsFrom(result)
				total := max(jf.GetInt(result, "TotalRecordCount"), len(items))
				if len(items) > maxItems {
					items = items[:maxItems]
				}
				return jf.TextResult(fmt.Sprintf("Recordings (%d):%s\n\n%s", len(items), noteText(moreNote(len(items), total, maxItems, "")), jf.FormatJSON(items))), nil, nil

			case "delete_recording":
				if args.RecordingID == "" {
					return jf.ErrResult("recording_id is required."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				recLabel := recordingLabel(ctx, client, args.RecordingID)
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete recording '%s' permanently? Its recording file is removed from the server.", recLabel)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/LiveTv/Recordings/%s", jf.SanitizeID(args.RecordingID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete recording: %v", err), nil, nil
				}
				return jf.TextResult("Recording deleted."), nil, nil

			case "timers":
				var result map[string]any
				if err := client.Get(ctx, "/LiveTv/Timers", nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(result, "StartDate", "EndDate"))), nil, nil

			case "create_timer":
				if args.ProgramID == "" {
					return jf.ErrResult("program_id is required. Use 'programs' to find program IDs."), nil, nil
				}
				params := url.Values{"programId": {args.ProgramID}}
				var defaults map[string]any
				if err := client.Get(ctx, "/LiveTv/Timers/Defaults", params, &defaults); err != nil {
					return jf.ErrResult("Failed to get timer defaults: %v", err), nil, nil
				}
				if err := client.PostNoContent(ctx, "/LiveTv/Timers", nil, defaults); err != nil {
					return jf.ErrResult("Failed to create timer: %v", err), nil, nil
				}
				return jf.TextResult("Recording timer created."), nil, nil

			case "cancel_timer":
				if args.TimerID == "" {
					return jf.ErrResult("timer_id is required. Use 'timers' to find timer IDs."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				tLabel := timerLabel(ctx, client, "/LiveTv/Timers/"+jf.SanitizeID(args.TimerID), args.TimerID)
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Cancel scheduled recording timer %s? The program will not be recorded.", tLabel)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/LiveTv/Timers/%s", jf.SanitizeID(args.TimerID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to cancel timer: %v", err), nil, nil
				}
				return jf.TextResult("Timer cancelled."), nil, nil

			case "series_timers":
				var result map[string]any
				if err := client.Get(ctx, "/LiveTv/SeriesTimers", nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(result, "StartDate", "EndDate"))), nil, nil

			case "create_series_timer":
				if args.ProgramID == "" {
					return jf.ErrResult("program_id is required."), nil, nil
				}
				// The timer defaults for a program are a SeriesTimerInfoDto that
				// names the program and its channel and carries the default schedule
				// and padding. That is the body the series timer endpoint takes, so
				// the defaults are posted unchanged, and the server resolves the
				// series from the program.
				params := url.Values{"programId": {args.ProgramID}}
				var defaults map[string]any
				if err := client.Get(ctx, "/LiveTv/Timers/Defaults", params, &defaults); err != nil {
					return jf.ErrResult("Failed to get timer defaults: %v", err), nil, nil
				}
				if err := client.PostNoContent(ctx, "/LiveTv/SeriesTimers", nil, defaults); err != nil {
					return jf.ErrResult("Failed to create series timer: %v", err), nil, nil
				}
				return jf.TextResult("Series recording rule created."), nil, nil

			case "cancel_series_timer":
				if args.TimerID == "" {
					return jf.ErrResult("timer_id is required. Use 'series_timers' to find IDs."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				stLabel := timerLabel(ctx, client, "/LiveTv/SeriesTimers/"+jf.SanitizeID(args.TimerID), args.TimerID)
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Cancel series recording rule %s? Future episodes will not be recorded.", stLabel)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/LiveTv/SeriesTimers/%s", jf.SanitizeID(args.TimerID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to cancel series timer: %v", err), nil, nil
				}
				return jf.TextResult("Series recording rule cancelled."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: channels, channel, programs, recommended, guide_info, tuners, recordings, delete_recording, timers, create_timer, cancel_timer, series_timers, create_series_timer, cancel_series_timer", args.Action), nil, nil
			}
		})
	}
}

// redactURL reduces a tuner URL to its scheme and host. An M3U playlist URL
// from an IPTV provider often carries the account's credentials in the
// user-info, the query, or the path itself, so everything past the host is
// removed, and the removal is noted. Scheme and host are enough to tell tuners
// apart. A value without a scheme, such as a device address or a local file
// path, is returned unchanged, and an absolute URL that does not parse is
// withheld rather than shown.
func redactURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(URL withheld: it could not be parsed for redaction)"
	}
	origin := u.Scheme + "://" + u.Host
	if u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") {
		return origin
	}
	return origin + " (path, credentials, and query removed)"
}

// redactionMarkers are the strings redaction writes in place of a value. A
// configuration that carries one was built from redacted output.
var redactionMarkers = []string{"(redacted)", "(path, credentials, and query removed)", "(URL withheld: it could not be parsed for redaction)"}

// containsRedaction reports whether any string in v, at any depth, carries a
// redaction marker.
func containsRedaction(v any) bool {
	switch val := v.(type) {
	case string:
		for _, m := range redactionMarkers {
			if strings.Contains(val, m) {
				return true
			}
		}
	case map[string]any:
		for _, item := range val {
			if containsRedaction(item) {
				return true
			}
		}
	case []any:
		for _, item := range val {
			if containsRedaction(item) {
				return true
			}
		}
	}
	return false
}

// redactLiveTVConfig redacts the credentials in a livetv configuration
// section: tuner URLs through redactURL, and listing providers' user names and
// passwords.
func redactLiveTVConfig(config any) {
	m := jf.ToMap(config)
	for _, h := range jf.ToSlice(m["TunerHosts"]) {
		if hm := jf.ToMap(h); hm != nil {
			if u := jf.GetString(hm, "Url"); u != "" {
				hm["Url"] = redactURL(u)
			}
		}
	}
	for _, p := range jf.ToSlice(m["ListingProviders"]) {
		if pm := jf.ToMap(p); pm != nil {
			for _, k := range []string{"Username", "Password"} {
				if jf.GetString(pm, k) != "" {
					pm[k] = "(redacted)"
				}
			}
		}
	}
}

func recordingLabel(ctx context.Context, client jf.Client, recordingID string) string {
	var rec map[string]any
	if err := client.Get(ctx, "/LiveTv/Recordings/"+jf.SanitizeID(recordingID), nil, &rec); err == nil {
		if name := jf.GetString(rec, "Name"); name != "" {
			if ep := jf.GetString(rec, "EpisodeTitle"); ep != "" && ep != name {
				return name + " - " + ep
			}
			return name
		}
	}
	return recordingID
}

func timerLabel(ctx context.Context, client jf.Client, endpoint, fallbackID string) string {
	var timer map[string]any
	if err := client.Get(ctx, endpoint, nil, &timer); err == nil {
		if name := jf.GetString(timer, "Name"); name != "" {
			var details []string
			if ch := jf.GetString(timer, "ChannelName"); ch != "" {
				details = append(details, ch)
			}
			if start := jf.LocalDateTime(jf.GetString(timer, "StartDate")); start != "" {
				details = append(details, start)
			}
			if len(details) > 0 {
				return fmt.Sprintf("'%s' (%s)", name, strings.Join(details, ", "))
			}
			return fmt.Sprintf("'%s'", name)
		}
	}
	return fmt.Sprintf("'%s'", fallbackID)
}
