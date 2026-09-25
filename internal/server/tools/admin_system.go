package tools

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminSystemTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_system_info (read-only system queries) ---
	if enabled("jellyfin_system_info", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_system_info",
			Title: "System Info",
			InputSchema: jf.WithEnums[jf.SystemInfoInput](map[string][]any{
				"action": {"whoami", "info", "storage", "activity_log", "ping", "logs", "log_file", "playback_history", "health_check"},
			}),
			Description: "Get server information and status. Actions cover user identity (whoami), server info, storage, activity logs, ping, log files, playback history, and a one-call health_check." +
				"\n\nAction notes:" +
				"\n- activity_log: Shows activity events (logins and failed logins, playback start/stop, session activity, task and plugin results), newest first; this is not the server's log. Filter with type, severity, item_id, user_id, min_date, and max_date; sort with sort_by and sort_order. For the server's own errors and warnings, use the log_file action instead." +
				"\n- playback_history: Returns items with play counts, last-played dates, and an actual_playback flag (true = verified playback session in activity log, false = manually marked as watched or playback event expired from log). When the user asks 'what did I watch/last watch', use default verified_only=true. When they ask 'what's marked as watched', set verified_only=false to include manually marked items." +
				"\n- logs: Lists available log files. Tip: most server logs are named log_*.log, and FFmpeg transcode logs are named FFmpeg.*.log." +
				"\n- log_file: Reads a server log as entries, each with its exception message and the first frames of its stack trace, and returns the newest that match. Filter with severity, min_date and max_date, and contains. " +
				fmt.Sprintf("A result holds at most %d entries (limit, default %d) and about %d characters; the result says how to reach the earlier entries it leaves out.", jf.LogFileMaxEntries, jf.LogFileDefaultEntries, jf.LogFileMaxChars),
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SystemInfoInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "whoami":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var user map[string]any
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(userID))
				if err := client.Get(ctx, endpoint, nil, &user); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				result := map[string]any{
					"user_id":  userID,
					"username": jf.GetString(user, "Name"),
				}
				if policy := jf.ToMap(user["Policy"]); policy != nil {
					result["is_admin"] = jf.GetBool(policy, "IsAdministrator")
					result["enable_all_folders"] = jf.GetBool(policy, "EnableAllFolders")
					if ids := jf.ToStringSlice(policy["EnabledFolders"]); len(ids) > 0 {
						// Resolve folder IDs to names
						var libs []map[string]any
						if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err == nil {
							nameMap := make(map[string]string, len(libs))
							for _, lib := range libs {
								nameMap[jf.GetString(lib, "ItemId")] = jf.GetString(lib, "Name")
							}
							resolved := make([]map[string]any, 0, len(ids))
							for _, id := range ids {
								entry := map[string]any{"id": id}
								if name, ok := nameMap[id]; ok {
									entry["name"] = name
								}
								resolved = append(resolved, entry)
							}
							result["enabled_folders"] = resolved
						} else {
							result["enabled_folder_ids"] = ids
						}
					}
					result["max_parental_rating"] = jf.GetInt(policy, "MaxParentalRating")
				}
				if la := jf.GetString(user, "LastActivityDate"); la != "" {
					result["last_activity"] = jf.LocalDateTime(la)
				}
				if ll := jf.GetString(user, "LastLoginDate"); ll != "" {
					result["last_login"] = jf.LocalDateTime(ll)
				}
				return jf.TextResult(fmt.Sprintf("Current user:\n\n%s", jf.FormatJSON(result))), nil, nil

			case "info":
				var info map[string]any
				if err := client.Get(ctx, "/System/Info", nil, &info); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				// SystemInfo's OperatingSystem, OperatingSystemDisplayName,
				// CanSelfRestart, and HasUpdateAvailable are obsolete. The server
				// never sets them, so they carry only their defaults and are not
				// reported.
				result := map[string]any{
					"server_name":              jf.GetString(info, "ServerName"),
					"version":                  jf.GetString(info, "Version"),
					"id":                       jf.GetString(info, "Id"),
					"startup_wizard_completed": jf.GetBool(info, "StartupWizardCompleted"),
					"has_pending_restart":      jf.GetBool(info, "HasPendingRestart"),
				}
				if lo := jf.GetString(info, "LocalAddress"); lo != "" {
					result["local_address"] = lo
				}
				if pkg := jf.GetString(info, "PackageName"); pkg != "" {
					result["package_name"] = pkg
				}
				return jf.TextResult(jf.FormatJSON(result)), nil, nil

			case "storage":
				return handleStorage(ctx, client)

			case "activity_log":
				return handleActivityLog(ctx, client, args)

			case "ping":
				if err := client.PostNoContent(ctx, "/System/Ping", nil, nil); err != nil {
					return jf.ErrResult("Ping failed: %v", err), nil, nil
				}
				return jf.TextResult("Server is responsive."), nil, nil

			case "logs":
				var logs []map[string]any
				if err := client.Get(ctx, "/System/Logs", nil, &logs); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				// Categorize and filter log files
				logType := strings.ToLower(args.LogType)
				if logType == "" {
					logType = "main"
				}
				if !slices.Contains([]string{"main", "ffmpeg", "all"}, logType) {
					return jf.ErrResult("log_type %q is not valid. Use main, ffmpeg, or all.", args.LogType), nil, nil
				}
				var mainCount, ffmpegCount int
				entries := make([]map[string]any, 0, len(logs))
				for _, l := range logs {
					name := jf.GetString(l, "Name")
					isMain := strings.HasPrefix(name, "log_") && strings.HasSuffix(name, ".log")
					isFFmpeg := strings.HasPrefix(name, "FFmpeg")
					if isMain {
						mainCount++
					}
					if isFFmpeg {
						ffmpegCount++
					}
					// Apply type filter
					switch logType {
					case "main":
						if !isMain {
							continue
						}
					case "ffmpeg":
						if !isFFmpeg {
							continue
						}
					}
					entries = append(entries, map[string]any{
						"name":          name,
						"date_modified": jf.LocalDateTime(jf.GetString(l, "DateModified")),
						"size":          jf.GetInt64(l, "Size"),
					})
				}
				// Sort by date_modified descending
				sort.Slice(entries, func(i, j int) bool {
					return jf.NewerFirst(jf.GetString(entries[i], "date_modified"), jf.GetString(entries[j], "date_modified"))
				})
				limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
				if len(entries) > limit {
					entries = entries[:limit]
				}
				header := fmt.Sprintf("Log files (%d main, %d ffmpeg", mainCount, ffmpegCount)
				otherCount := len(logs) - mainCount - ffmpegCount
				if otherCount > 0 {
					header += fmt.Sprintf(", %d other", otherCount)
				}
				header += fmt.Sprintf(", showing %d with type=%s)", len(entries), logType)
				return jf.TextResult(fmt.Sprintf("%s:\n\n%s", header, jf.FormatJSON(entries))), nil, nil

			case "log_file":
				return handleLogFile(ctx, client, args)

			case "playback_history":
				return handlePlaybackHistory(ctx, client, args)

			case "health_check":
				return handleHealthCheck(ctx, client)

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: whoami, info, storage, activity_log, ping, logs, log_file, playback_history, health_check", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_system_control (destructive server operations) ---
	if enabled("jellyfin_system_control", AnnotDestructive) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_system_control",
			Title: "Server Control",
			InputSchema: jf.WithEnums[jf.SystemControlInput](map[string][]any{
				"action": {"restart", "shutdown"},
			}),
			Description: "Restart or shut down the Jellyfin server. These are destructive operations that cannot be undone. " +
				"Use 'restart' to restart the server (it is unavailable while it restarts). " +
				"Use 'shutdown' to stop the server completely (must be manually restarted).",
			Annotations: AnnotDestructive,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SystemControlInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "restart":
				if result := jf.DestructiveGate(ctx, req, args.Confirm, "Restart the Jellyfin server? It is unavailable while it restarts, and playback stops."); result != nil {
					return result, nil, nil
				}
				if err := client.PostNoContent(ctx, "/System/Restart", nil, nil); err != nil {
					return jf.ErrResult("Failed to restart: %v", err), nil, nil
				}
				return jf.TextResult("Server restart started. The server is unavailable until it comes back."), nil, nil

			case "shutdown":
				if result := jf.DestructiveGate(ctx, req, args.Confirm, "Shut down the Jellyfin server? It stays off until someone starts it again by hand."); result != nil {
					return result, nil, nil
				}
				if err := client.PostNoContent(ctx, "/System/Shutdown", nil, nil); err != nil {
					return jf.ErrResult("Failed to shutdown: %v", err), nil, nil
				}
				return jf.TextResult("Server shutdown started. It stays off until someone starts it again by hand."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: restart, shutdown", args.Action), nil, nil
			}
		})
	}
}

func handleStorage(ctx context.Context, client jf.Client) (*mcp.CallToolResult, any, error) {
	var info map[string]any
	if err := client.Get(ctx, "/System/Info/Storage", nil, &info); err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	type mountEntry struct {
		FreeSpace int64
		Paths     []string
		Libraries []string
	}
	mounts := make(map[string]*mountEntry)
	addMount := func(freeSpace int64, path, label string) {
		m, ok := mounts[path]
		if !ok {
			m = &mountEntry{FreeSpace: freeSpace}
			mounts[path] = m
			m.Paths = append(m.Paths, path)
		}
		if label != "" {
			m.Libraries = append(m.Libraries, label)
		}
	}
	for _, key := range []string{"ProgramDataFolder", "CacheFolder", "LogFolder", "TranscodingTempFolder"} {
		if folder := jf.ToMap(info[key]); folder != nil {
			addMount(jf.GetInt64(folder, "FreeSpace"), jf.GetString(folder, "Path"), "")
		}
	}
	if libs := jf.ToSlice(info["Libraries"]); len(libs) > 0 {
		for _, lib := range libs {
			lm := jf.ToMap(lib)
			libName := jf.GetString(lm, "Name")
			if folders := jf.ToSlice(lm["Folders"]); len(folders) > 0 {
				for _, f := range folders {
					fm := jf.ToMap(f)
					addMount(jf.GetInt64(fm, "FreeSpace"), jf.GetString(fm, "Path"), libName)
				}
			}
		}
	}
	mountList := make([]map[string]any, 0, len(mounts))
	for _, m := range mounts {
		entry := map[string]any{
			"free":       jf.FormatGB(m.FreeSpace),
			"free_bytes": m.FreeSpace,
			"paths":      m.Paths,
		}
		if m.FreeSpace < jf.LowStorageThreshold {
			entry["low_space"] = true
		}
		if len(m.Libraries) > 0 {
			entry["libraries"] = m.Libraries
		}
		mountList = append(mountList, entry)
	}
	if len(mountList) == 0 {
		return jf.TextResult(jf.FormatJSON(info)), nil, nil
	}
	return jf.TextResult(fmt.Sprintf("Storage (%d mounts):\n\n%s", len(mountList), jf.FormatJSON(mountList))), nil, nil
}

// activitySorts maps the activity_log sort_by values to Jellyfin's fields.
var activitySorts = map[string]string{"date": "DateCreated", "name": "Name", "type": "Type", "severity": "LogSeverity"}

// isJellyfinID reports whether s is a Jellyfin ID: a GUID of 32 hexadecimal
// digits, with or without dashes.
func isJellyfinID(s string) bool {
	id := jf.NormalizeID(s)
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}

func handleActivityLog(ctx context.Context, client jf.Client, args jf.SystemInfoInput) (*mcp.CallToolResult, any, error) {
	q := jf.ActivityQuery{
		Type:   args.Type,
		ItemID: args.ItemID,
		UserID: args.UserID,
		Limit:  jf.ClampInt(args.Limit, 200, jf.MaxLimitCap),
	}
	var filters []string
	if args.MinDate != "" {
		t, _, err := parseDateArg("min_date", args.MinDate)
		if err != nil {
			return jf.ErrResult("%v", err), nil, nil
		}
		q.MinDate = t
		filters = append(filters, "since "+args.MinDate)
	}
	if args.MaxDate != "" {
		t, err := parseMaxDateArg("max_date", args.MaxDate)
		if err != nil {
			return jf.ErrResult("%v", err), nil, nil
		}
		q.MaxDate = t
		filters = append(filters, "until "+args.MaxDate)
	}
	if !q.MinDate.IsZero() && !q.MaxDate.IsZero() && q.MinDate.After(q.MaxDate) {
		return jf.ErrResult("min_date (%s) is later than max_date (%s)", args.MinDate, args.MaxDate), nil, nil
	}
	for level := range strings.SplitSeq(args.Severity, ",") {
		level = strings.TrimSpace(level)
		if level == "" {
			continue
		}
		i := slices.IndexFunc(jf.ActivitySeverities, func(s string) bool { return strings.EqualFold(s, level) })
		if i < 0 {
			return jf.ErrResult("severity for activity_log must be one or more of %s, separated by commas, not %q", strings.Join(jf.ActivitySeverities, ", "), level), nil, nil
		}
		if !slices.Contains(q.Severities, jf.ActivitySeverities[i]) {
			q.Severities = append(q.Severities, jf.ActivitySeverities[i])
		}
	}
	if len(q.Severities) > 0 {
		filters = append(filters, "severity="+strings.Join(q.Severities, ","))
	}
	sortBy := strings.ToLower(cmp.Or(args.SortBy, "date"))
	q.SortBy = activitySorts[sortBy]
	if q.SortBy == "" {
		return jf.ErrResult("sort_by for activity_log must be date, name, type, or severity, not %q", args.SortBy), nil, nil
	}
	switch strings.ToLower(args.SortOrder) {
	case "":
		// Names and types read best A to Z; dates and severities, the most
		// recent or most severe first.
		q.Ascending = sortBy == "name" || sortBy == "type"
	case "ascending":
		q.Ascending = true
	case "descending":
	default:
		return jf.ErrResult("sort_order must be ascending or descending, not %q", args.SortOrder), nil, nil
	}
	if args.ItemID != "" && !isJellyfinID(args.ItemID) {
		return jf.ErrResult("item_id must be a Jellyfin item ID of 32 hexadecimal digits, with or without dashes, not %q", args.ItemID), nil, nil
	}
	for _, f := range []struct{ name, value string }{{"type", args.Type}, {"item_id", args.ItemID}, {"user_id", args.UserID}} {
		if f.value != "" {
			filters = append(filters, f.name+"="+f.value)
		}
	}

	found, err := jf.QueryActivity(ctx, client, q)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	entries := make([]map[string]any, 0, len(found.Entries))
	for _, m := range found.Entries {
		entry := map[string]any{
			"date":     jf.LocalDateTime(jf.GetString(m, "Date")),
			"name":     jf.GetString(m, "Name"),
			"type":     jf.GetString(m, "Type"),
			"severity": jf.GetString(m, "Severity"),
		}
		if overview := jf.GetString(m, "Overview"); overview != "" {
			entry["overview"] = jf.Truncate(overview, jf.OverviewMaxLen)
		}
		if uid := jf.GetString(m, "UserId"); uid != "" {
			entry["user_id"] = uid
		}
		if itemID := jf.GetString(m, "ItemId"); itemID != "" {
			entry["item_id"] = itemID
		}
		entries = append(entries, entry)
	}
	header := fmt.Sprintf("Activity log (%d entries", len(entries))
	if len(filters) > 0 {
		header += ", " + strings.Join(filters, ", ")
	}
	if sortBy != "date" || q.Ascending {
		order := "descending"
		if q.Ascending {
			order = "ascending"
		}
		header += fmt.Sprintf(", sorted by %s %s", sortBy, order)
	}
	header += ")"
	msg := fmt.Sprintf("%s:\n\n%s", header, jf.FormatJSON(entries))
	if found.Capped {
		msg += fmt.Sprintf("\n\nOnly the %d most recent entries were examined, so older matches may be missing. Narrow the dates with min_date and max_date to reach them.", jf.ActivityLogLookback)
	}
	return jf.TextResult(msg), nil, nil
}

// playbackEntry is a played item with what the play state adds: the play
// count, and whether the activity log verifies a playback.
type playbackEntry struct {
	jf.MediaItem
	PlayCount      int   `json:"play_count,omitempty"`
	ActualPlayback *bool `json:"actual_playback,omitempty"`
}

func handlePlaybackHistory(ctx context.Context, client jf.Client, args jf.SystemInfoInput) (*mcp.CallToolResult, any, error) {
	userID, err := client.GetUserID(ctx)
	if err != nil {
		return jf.ErrResult("Jellyfin error: %v", err), nil, nil
	}
	maxItems := jf.ClampInt(args.Limit, 500, jf.MaxLimitCap)
	targetUser := userID
	if args.UserID != "" {
		targetUser = args.UserID
	}
	params := url.Values{
		"UserId":    {targetUser},
		"Recursive": {"true"},
		"Filters":   {"IsPlayed"},
		"SortBy":    {"DatePlayed"},
		"SortOrder": {"Descending"},
		"Fields":    {"Overview"},
	}
	rawItems, total, err := jf.FetchAllPages(ctx, client, "/Items", params, maxItems)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}

	verifiedOnly := args.VerifiedOnly == nil || *args.VerifiedOnly
	// Without the activity log, playback can be listed but not verified.
	playedViaPlayback, verifyErr := jf.PlayedItemIDs(ctx, client, targetUser)
	if verifyErr != nil && verifiedOnly {
		return jf.ErrResult("Jellyfin API error: %v. Set verified_only=false to list items marked played without verifying them.", verifyErr), nil, nil
	}

	entries := make([]playbackEntry, 0, len(rawItems))
	for _, raw := range rawItems {
		m := jf.ToMap(raw)
		entry := playbackEntry{MediaItem: jf.MediaItemFrom(m)}
		if ud := jf.ToMap(m["UserData"]); ud != nil {
			if lp := jf.GetString(ud, "LastPlayedDate"); lp != "" {
				entry.LastPlayed = jf.LocalDateTime(lp)
			}
			if pc := jf.GetInt(ud, "PlayCount"); pc > 0 {
				entry.PlayCount = pc
			}
		}
		actual := playedViaPlayback[jf.NormalizeID(entry.ID)]
		if verifyErr == nil {
			entry.ActualPlayback = &actual
		}
		if verifiedOnly && !actual {
			continue
		}
		entries = append(entries, entry)
	}
	header := fmt.Sprintf("Playback history (%d items", len(entries))
	if verifiedOnly {
		header += fmt.Sprintf(", verified_only=true, %d total marked played", total)
		if total > len(rawItems) {
			header += fmt.Sprintf(", the %d most recently played checked", len(rawItems))
		}
	} else {
		header += fmt.Sprintf(" of %d total", total)
	}
	header += "). played means finished at least once; progress is the position saved by the latest play"
	msg := fmt.Sprintf("%s:\n\n%s", header, jf.FormatJSON(entries))
	if verifyErr != nil {
		msg += fmt.Sprintf("\n\nPlayback could not be verified, so actual_playback is left out: %v", verifyErr)
	}
	return jf.TextResult(msg), nil, nil
}

func handleHealthCheck(ctx context.Context, client jf.Client) (*mcp.CallToolResult, any, error) {
	var (
		sysInfo     map[string]any
		tasks       []map[string]any
		plugins     []map[string]any
		logs        []map[string]any
		backups     any
		storageInfo map[string]any
		sysInfoErr  error
		tasksErr    error
		pluginsErr  error
		logsErr     error
		backupsErr  error
		storageErr  error
	)
	var wg sync.WaitGroup
	wg.Add(6)
	go func() { defer wg.Done(); sysInfoErr = client.Get(ctx, "/System/Info", nil, &sysInfo) }()
	go func() { defer wg.Done(); tasksErr = client.Get(ctx, "/ScheduledTasks", nil, &tasks) }()
	go func() { defer wg.Done(); pluginsErr = client.Get(ctx, "/Plugins", nil, &plugins) }()
	go func() { defer wg.Done(); logsErr = client.Get(ctx, "/System/Logs", nil, &logs) }()
	go func() { defer wg.Done(); backupsErr = client.Get(ctx, "/Backup", nil, &backups) }()
	go func() { defer wg.Done(); storageErr = client.Get(ctx, "/System/Info/Storage", nil, &storageInfo) }()
	wg.Wait()

	overallStatus := "ok"
	report := map[string]any{}

	if sysInfoErr == nil {
		serverSection := map[string]any{
			"version":             jf.GetString(sysInfo, "Version"),
			"server_name":         jf.GetString(sysInfo, "ServerName"),
			"has_pending_restart": jf.GetBool(sysInfo, "HasPendingRestart"),
		}
		if jf.GetBool(sysInfo, "HasPendingRestart") {
			overallStatus = "warnings"
		}
		serverSection["minimum_supported_version"] = jf.MinSupportedVersion.String()
		if v, err := jf.ParseServerVersion(jf.GetString(sysInfo, "Version")); err == nil {
			serverSection["supported"] = v.Supported()
			if !v.Supported() {
				serverSection["note"] = fmt.Sprintf("Jellyfin %s is below the minimum supported version %s, so some tools may fail. Upgrade Jellyfin to %s or later.", v, jf.MinSupportedVersion, jf.MinSupportedVersion)
				if overallStatus == "ok" {
					overallStatus = "warnings"
				}
			}
		}
		report["server"] = serverSection
	}

	if storageErr == nil {
		storageHealthy := true
		mountSummary := make([]map[string]any, 0)
		seen := make(map[string]bool)
		checkMount := func(freeSpace int64, path string) {
			if seen[path] {
				return
			}
			seen[path] = true
			entry := map[string]any{
				"path": path,
				"free": jf.FormatGB(freeSpace),
			}
			if freeSpace < jf.LowStorageThreshold {
				entry["low_space"] = true
				storageHealthy = false
			}
			mountSummary = append(mountSummary, entry)
		}
		for _, key := range []string{"ProgramDataFolder", "CacheFolder", "TranscodingTempFolder"} {
			if folder := jf.ToMap(storageInfo[key]); folder != nil {
				checkMount(jf.GetInt64(folder, "FreeSpace"), jf.GetString(folder, "Path"))
			}
		}
		if libs := jf.ToSlice(storageInfo["Libraries"]); len(libs) > 0 {
			for _, lib := range libs {
				lm := jf.ToMap(lib)
				if folders := jf.ToSlice(lm["Folders"]); len(folders) > 0 {
					for _, f := range folders {
						fm := jf.ToMap(f)
						checkMount(jf.GetInt64(fm, "FreeSpace"), jf.GetString(fm, "Path"))
					}
				}
			}
		}
		if !storageHealthy && overallStatus == "ok" {
			overallStatus = "warnings"
		}
		report["storage"] = map[string]any{
			"ok":     storageHealthy,
			"mounts": mountSummary,
		}
	}

	if tasksErr == nil {
		failedTasks := make([]string, 0)
		runningTasks := make([]string, 0)
		for _, t := range tasks {
			name := jf.GetString(t, "Name")
			state := jf.GetString(t, "State")
			if state == "Running" {
				runningTasks = append(runningTasks, name)
			}
			if le := jf.ToMap(t["LastExecutionResult"]); le != nil {
				status := jf.GetString(le, "Status")
				if status != "" && status != "Completed" && status != "Aborted" {
					failedTasks = append(failedTasks, fmt.Sprintf("%s (%s)", name, status))
					if overallStatus == "ok" {
						overallStatus = "warnings"
					}
				}
			}
		}
		report["tasks"] = map[string]any{
			"failed":  failedTasks,
			"running": runningTasks,
		}
	}

	if pluginsErr == nil {
		needsRestart := make([]string, 0)
		failed := make([]string, 0)
		disabled := make([]string, 0)
		for _, p := range plugins {
			name := jf.GetString(p, "Name")
			version := jf.GetString(p, "Version")
			// Jellyfin's PluginStatus enum carries both spellings of
			// Superseded, and Malfunctioned and NotSupported are the plugins
			// that failed to load.
			switch jf.GetString(p, "Status") {
			case "Restart", "Superseded", "Superceded":
				needsRestart = append(needsRestart, fmt.Sprintf("%s (%s)", name, version))
			case "Malfunctioned", "NotSupported":
				failed = append(failed, fmt.Sprintf("%s (%s)", name, version))
			case "Disabled":
				disabled = append(disabled, name)
			}
		}
		if (len(needsRestart) > 0 || len(failed) > 0) && overallStatus == "ok" {
			overallStatus = "warnings"
		}
		report["plugins"] = map[string]any{
			"needs_restart":  needsRestart,
			"failed_to_load": failed,
			"disabled":       disabled,
		}
	}

	if logsErr == nil {
		var latestLog string
		var latestDate string
		for _, l := range logs {
			name := jf.GetString(l, "Name")
			if strings.HasPrefix(name, "log_") && strings.HasSuffix(name, ".log") {
				date := jf.GetString(l, "DateModified")
				if date > latestDate {
					latestDate = date
					latestLog = name
				}
			}
		}
		if latestLog != "" {
			params := url.Values{"name": {latestLog}}
			if logContent, err := client.GetRaw(ctx, "/System/Logs/Log", params); err == nil {
				// Errors and warnings are sampled separately, so frequent
				// warnings cannot push the errors out of the sample. An
				// error's sample line carries its exception message.
				var errs, warns []string
				for _, e := range jf.ParseServerLog(logContent) {
					switch {
					case e.IsError():
						errs = append(errs, truncateAtWord(e.Message(), logIssueMaxLen))
					case e.IsWarning():
						warns = append(warns, truncateAtWord(e.Message(), logIssueMaxLen))
					}
				}
				if len(errs) > 0 {
					overallStatus = "errors"
				}
				report["recent_log_issues"] = map[string]any{
					"log_file":      latestLog,
					"warnings":      len(warns),
					"errors":        len(errs),
					"last_errors":   lastN(errs, jf.HealthCheckMaxIssues),
					"last_warnings": lastN(warns, jf.HealthCheckMaxIssues),
				}
			}
		}
	}

	if backupsErr == nil {
		backupList := jf.ToSlice(backups)
		if len(backupList) > 0 {
			var latestBackup map[string]any
			var latestBackupDate string
			for _, b := range backupList {
				bm := jf.ToMap(b)
				date := jf.GetString(bm, "DateCreated")
				if date > latestBackupDate {
					latestBackupDate = date
					latestBackup = bm
				}
			}
			if latestBackup != nil {
				backupHealthy := true
				if created, ok := jf.ParseTime(latestBackupDate); ok {
					ageDays := int(time.Since(created).Hours() / 24)
					backupSection := map[string]any{
						"last_backup":          jf.LocalDate(latestBackupDate),
						"last_backup_age_days": ageDays,
					}
					if version := jf.GetString(latestBackup, "ServerVersion"); version != "" {
						backupSection["backup_server_version"] = version
					}
					if ageDays > jf.BackupStaleDays {
						backupHealthy = false
						if overallStatus == "ok" {
							overallStatus = "warnings"
						}
					}
					backupSection["ok"] = backupHealthy
					report["backups"] = backupSection
				}
			}
		} else {
			report["backups"] = map[string]any{"ok": false, "note": "no backups found"}
			if overallStatus == "ok" {
				overallStatus = "warnings"
			}
		}
	} else {
		report["backups"] = map[string]any{"error": fmt.Sprintf("listing backups failed: %v", backupsErr)}
	}

	// A section whose request failed is reported as such, and the status is
	// then unknown rather than ok, because what failed was not checked.
	for _, section := range []struct {
		name string
		err  error
	}{{"server", sysInfoErr}, {"storage", storageErr}, {"tasks", tasksErr}, {"plugins", pluginsErr}, {"logs", logsErr}, {"backups", backupsErr}} {
		if section.err == nil {
			continue
		}
		if _, ok := report[section.name]; !ok {
			report[section.name] = map[string]any{"error": section.err.Error()}
		}
		if overallStatus == "ok" {
			overallStatus = "unknown"
		}
	}
	report["status"] = overallStatus
	return jf.TextResult(fmt.Sprintf("Health check, status %s:\n\n%s", overallStatus, jf.FormatJSON(report))), nil, nil
}

// logIssueMaxLen bounds a log line in a health check, long enough to keep
// the exception message that follows the logger name.
const logIssueMaxLen = 400

// truncateAtWord shortens s to at most max characters, ending at a word
// boundary when one falls in the last quarter, and marks the cut.
func truncateAtWord(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	if i := strings.LastIndex(s[:max], " "); i > max*3/4 {
		cut = i
	}
	return s[:cut] + "…"
}

// handleLogFile reads one server log as entries and returns the newest that
// match the filters, as many as the limit and the size budget allow, oldest
// first. When entries are left out, the result gives the max_date that reaches
// them.
func handleLogFile(ctx context.Context, client jf.Client, args jf.SystemInfoInput) (*mcp.CallToolResult, any, error) {
	if args.Name == "" {
		return jf.ErrResult("name is required for log_file. Use 'logs' action to list available log files."), nil, nil
	}
	severity := strings.ToLower(args.Severity)
	if severity == "" {
		severity = "all"
	}
	if !slices.Contains([]string{"all", "warn", "error", "warn+error"}, severity) {
		return jf.ErrResult("severity %q is not valid for log_file. Use all, warn, error, or warn+error.", args.Severity), nil, nil
	}
	var filters []string
	if severity != "all" {
		filters = append(filters, "severity "+severity)
	}
	var minT, maxT time.Time
	if args.MinDate != "" {
		t, _, err := parseDateArg("min_date", args.MinDate)
		if err != nil {
			return jf.ErrResult("%v", err), nil, nil
		}
		minT = t
		filters = append(filters, "since "+args.MinDate)
	}
	if args.MaxDate != "" {
		t, err := parseMaxDateArg("max_date", args.MaxDate)
		if err != nil {
			return jf.ErrResult("%v", err), nil, nil
		}
		maxT = t
		filters = append(filters, "until "+args.MaxDate)
	}
	if !minT.IsZero() && !maxT.IsZero() && minT.After(maxT) {
		return jf.ErrResult("min_date (%s) is later than max_date (%s)", args.MinDate, args.MaxDate), nil, nil
	}
	contains := strings.TrimSpace(args.Contains)
	if contains != "" {
		filters = append(filters, fmt.Sprintf("containing %q", contains))
	}

	content, err := client.GetRaw(ctx, "/System/Logs/Log", url.Values{"name": {args.Name}})
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	needle := strings.ToLower(contains)
	var matches []jf.LogEntry
	untimed := 0
	for _, e := range jf.ParseServerLog(content) {
		switch {
		case severity == "warn" && !e.IsWarning(),
			severity == "error" && !e.IsError(),
			severity == "warn+error" && !e.IsWarning() && !e.IsError():
			continue
		}
		if !minT.IsZero() || !maxT.IsZero() {
			if e.Time.IsZero() {
				untimed++
				continue
			}
			if (!minT.IsZero() && e.Time.Before(minT)) || (!maxT.IsZero() && e.Time.After(maxT)) {
				continue
			}
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Header+"\n"+strings.Join(e.Detail, "\n")), needle) {
			continue
		}
		matches = append(matches, e)
	}

	scope := ""
	if len(filters) > 0 {
		scope = " (" + strings.Join(filters, ", ") + ")"
	}
	untimedNote := ""
	if untimed > 0 {
		untimedNote = fmt.Sprintf(" %d %s no time in the layout Jellyfin writes by default, so the date filters left %s out.", untimed, plural(untimed, "entry has", "entries have"), plural(untimed, "it", "them"))
	}
	if len(matches) == 0 {
		return jf.TextResult(fmt.Sprintf("Log file '%s'%s: no entries match.%s", args.Name, scope, untimedNote)), nil, nil
	}
	limit := jf.ClampInt(args.Limit, jf.LogFileDefaultEntries, jf.LogFileMaxEntries)
	var shown []string
	size, oldest, full := 0, len(matches), false
	for i := len(matches) - 1; i >= 0 && len(shown) < limit; i-- {
		text := jf.Truncate(matches[i].Text(jf.LogFileStackFrames), jf.LogFileMaxChars)
		if len(shown) > 0 && size+len(text) > jf.LogFileMaxChars {
			full = true
			break
		}
		shown = append(shown, text)
		size += len(text) + 1
		oldest = i
	}
	slices.Reverse(shown)
	msg := fmt.Sprintf("Log file '%s'%s: %d %s; showing the newest %d, oldest first.", args.Name, scope, len(matches), plural(len(matches), "entry matches", "entries match"), len(shown))
	if left := oldest; left > 0 {
		why := fmt.Sprintf("the limit is %d", limit)
		if full {
			why = "more would not fit in one result"
		}
		msg += fmt.Sprintf(" The %d earlier %s left out because %s.", left, plural(left, "entry is", "entries are"), why)
		if t := matches[oldest].Time; !t.IsZero() {
			msg += fmt.Sprintf(" To read them, call again with max_date=%s, one millisecond before the oldest entry shown.", t.Add(-time.Millisecond).In(time.Local).Format("2006-01-02T15:04:05.000Z07:00"))
		}
	}
	return jf.TextResult(msg + untimedNote + "\n\n" + strings.Join(shown, "\n")), nil, nil
}

// lastN returns the last n entries of s, never nil.
func lastN(s []string, n int) []string {
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return append([]string{}, s...)
}
