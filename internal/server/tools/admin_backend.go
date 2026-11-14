package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminBackendTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_plugins ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_plugins", AnnotDestructiveOpenWorld, AnnotReadOnlyOpenWorld); ok {
		roActions := []any{"list", "get_config", "list_packages", "list_repos"}
		schema := jf.WithEnums[jf.PluginsInput](map[string][]any{
			"action": {"list", "enable", "disable", "uninstall", "get_config", "update_config", "list_packages", "install", "list_repos", "set_repos"},
		})
		desc := "Inspect, install, enable/disable, configure, or uninstall Jellyfin plugins and manage plugin repositories. Plugin changes often require a server restart."
		if ro {
			schema = jf.WithEnumsFiltered[jf.PluginsInput](map[string][]any{"action": roActions}, []string{"action", "plugin_id"})
			desc = "List installed plugins, read plugin configuration, or browse available packages and repositories."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_plugins",
			Title:       "Plugins",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PluginsInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "list":
				var plugins []map[string]any
				if err := client.Get(ctx, "/Plugins", nil, &plugins); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(plugins))
				for _, p := range plugins {
					items = append(items, map[string]any{
						"id":      jf.GetString(p, "Id"),
						"name":    jf.GetString(p, "Name"),
						"version": jf.GetString(p, "Version"),
						"status":  jf.GetString(p, "Status"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Installed plugins (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "enable", "disable":
				if args.PluginID == "" || args.Version == "" {
					return jf.ErrResult("plugin_id and version are required. Use 'list' to find them."), nil, nil
				}
				if args.Action == "disable" {
					if res := jf.DestructiveDisabledError(ctx); res != nil {
						return res, nil, nil
					}
				}
				plugin, err := installedPlugin(ctx, client, args.PluginID)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				name := args.PluginID
				if plugin != nil {
					if n := jf.GetString(plugin, "Name"); n != "" {
						name = n
					}
					if !jf.GetBool(plugin, "CanUninstall") {
						return jf.ErrResult("'%s' is built into the server and cannot be %sd.", name, args.Action), nil, nil
					}
				}
				if args.Action == "disable" {
					if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Disable plugin '%s' (version %s)? Its features stop working after the server restarts.", name, args.Version)); result != nil {
						return result, nil, nil
					}
				}
				verb := "Enable"
				if args.Action == "disable" {
					verb = "Disable"
				}
				endpoint := fmt.Sprintf("/Plugins/%s/%s/%s", jf.SanitizeID(args.PluginID), jf.SanitizeID(args.Version), verb)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to %s plugin: %v", args.Action, err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Plugin '%s' %sd. The change takes effect after a server restart.", name, args.Action)), nil, nil

			case "uninstall":
				if args.PluginID == "" || args.Version == "" {
					return jf.ErrResult("plugin_id and version are required."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				plugin, err := installedPlugin(ctx, client, args.PluginID)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				name := args.PluginID
				if plugin != nil {
					if n := jf.GetString(plugin, "Name"); n != "" {
						name = n
					}
					if !jf.GetBool(plugin, "CanUninstall") {
						return jf.ErrResult("'%s' is built into the server and cannot be uninstalled.", name), nil, nil
					}
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Uninstall plugin '%s' (version %s)? The removal completes after a server restart.", name, args.Version)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Plugins/%s/%s", jf.SanitizeID(args.PluginID), jf.SanitizeID(args.Version))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to uninstall plugin: %v", err), nil, nil
				}
				return jf.TextResult("Plugin uninstalled. A server restart may be required."), nil, nil

			case "get_config":
				if args.PluginID == "" {
					return jf.ErrResult("plugin_id is required."), nil, nil
				}
				var config map[string]any
				endpoint := fmt.Sprintf("/Plugins/%s/Configuration", jf.SanitizeID(args.PluginID))
				if err := client.Get(ctx, endpoint, nil, &config); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(config)), nil, nil

			case "list_packages":
				var packages []map[string]any
				if err := client.Get(ctx, "/Packages", nil, &packages); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(packages))
				for _, p := range packages {
					items = append(items, map[string]any{
						"name":        jf.GetString(p, "name"),
						"description": jf.Truncate(jf.GetString(p, "description"), jf.SummaryMaxLen),
						"category":    jf.GetString(p, "category"),
						"owner":       jf.GetString(p, "owner"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Available packages (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "install":
				if args.PackageName == "" {
					return jf.ErrResult("package_name is required. Use 'list_packages' to find available packages."), nil, nil
				}
				var versionPart, repoPart string
				if args.Version != "" {
					versionPart = fmt.Sprintf(" (version %s)", args.Version)
				}
				if args.RepoURL != "" {
					repoPart = fmt.Sprintf(" from %s", args.RepoURL)
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Install plugin package '%s'%s%s? Plugins run third-party code on the Jellyfin server, and a server restart may be required to finish installation.", args.PackageName, versionPart, repoPart)); result != nil {
					return result, nil, nil
				}
				params := url.Values{}
				if args.Version != "" {
					params.Set("version", args.Version)
				}
				if args.RepoURL != "" {
					params.Set("repositoryUrl", args.RepoURL)
				}
				endpoint := fmt.Sprintf("/Packages/Installed/%s", jf.SanitizeID(args.PackageName))
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to install package: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Package '%s' installation started. A server restart may be required.", args.PackageName)), nil, nil

			case "update_config":
				if args.PluginID == "" {
					return jf.ErrResult("plugin_id is required for update_config. Use 'list' to find plugin IDs."), nil, nil
				}
				if args.Config == nil {
					return jf.ErrResult("config is required for update_config. Use 'get_config' first to see available fields."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				patch, ok := jf.AsConfigMap(args.Config)
				if !ok || len(patch) == 0 {
					return jf.ErrResult("config must be a non-empty JSON object of plugin configuration fields."), nil, nil
				}
				endpoint := fmt.Sprintf("/Plugins/%s/Configuration", jf.SanitizeID(args.PluginID))
				var current map[string]any
				if err := client.Get(ctx, endpoint, nil, &current); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				name := pluginName(ctx, client, args.PluginID)
				merged := jf.MergeConfig(current, patch)
				changes := jf.DiffConfig(current, merged)
				if len(current) > 0 && len(changes) == 0 {
					return jf.TextResult(fmt.Sprintf("Plugin '%s' already has that configuration; nothing changed.", name)), nil, nil
				}
				var warning string
				if len(current) > 0 {
					warning = fmt.Sprintf("Update configuration for plugin '%s'? Changes: %s. Other plugin settings stay unchanged.", name, jf.FormatConfigChanges(changes))
				} else {
					warning = fmt.Sprintf("Update configuration for plugin '%s'? Settings: %s.", name, jf.FormatConfigChanges(changes))
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				if err := client.PostNoContent(ctx, endpoint, nil, merged); err != nil {
					return jf.ErrResult("Failed to update plugin config: %v", err), nil, nil
				}
				return jf.TextResult("Plugin configuration updated."), nil, nil

			case "list_repos":
				var repos any
				if err := client.Get(ctx, "/Repositories", nil, &repos); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Plugin repositories:\n\n%s", jf.FormatJSON(repos))), nil, nil

			case "set_repos":
				if args.Repos == nil {
					return jf.ErrResult("repos is required for set_repos. Use 'list_repos' first, modify the array, then POST back. Format: [{\"Name\": \"...\", \"Url\": \"...\"}]."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				newRepos, ok := jf.AsSlice(args.Repos)
				if !ok {
					return jf.ErrResult("repos must be a JSON array of {\"Name\": \"...\", \"Url\": \"...\"} objects."), nil, nil
				}
				var currentRepos []map[string]any
				if err := client.Get(ctx, "/Repositories", nil, &currentRepos); err == nil && currentRepos != nil && reposEqual(currentRepos, newRepos) {
					return jf.TextResult("Plugin repositories already match the given list; nothing changed."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, formatRepoWarning(currentRepos, newRepos)); result != nil {
					return result, nil, nil
				}
				if err := client.PostNoContent(ctx, "/Repositories", nil, newRepos); err != nil {
					return jf.ErrResult("Failed to set repositories: %v", err), nil, nil
				}
				return jf.TextResult("Plugin repositories updated."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, enable, disable, uninstall, get_config, update_config, list_packages, install, list_repos, set_repos", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_server ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_server", AnnotDestructive, AnnotReadOnly); ok {
		roActions := []any{"get_config", "get_config_section", "list_backups", "backup_manifest", "list_tasks", "get_task"}
		schema := jf.WithEnums[jf.ServerManageInput](map[string][]any{
			"action": {"get_config", "get_config_section", "update_config_section", "list_backups", "backup_manifest", "create_backup", "restore_backup", "list_tasks", "get_task", "start_task", "stop_task", "set_task_triggers", "restart", "shutdown"},
		})
		desc := "Manage server configuration, backups, scheduled tasks, and server restart/shutdown."
		if ro {
			schema = jf.WithEnumsFiltered[jf.ServerManageInput](map[string][]any{"action": roActions}, []string{"action", "key", "file_name", "task_id"})
			desc = "Read server configuration sections, inspect backups/manifests, and view scheduled tasks."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_server",
			Title:       "Server Management",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.ServerManageInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "get_config":
				var config map[string]any
				if err := client.Get(ctx, "/System/Configuration", nil, &config); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Server configuration:\n\n%s", jf.FormatJSON(config))), nil, nil

			case "get_config_section":
				args.Key = canonicalConfigSectionKey(args.Key)
				if args.Key == "" {
					return jf.ErrResult("key is required for get_config_section. Common keys: encoding (transcoding), network, branding, metadata, livetv."), nil, nil
				}
				var config any
				endpoint := fmt.Sprintf("/System/Configuration/%s", jf.SanitizeID(args.Key))
				if err := client.Get(ctx, endpoint, nil, &config); err != nil {
					var apiErr *jf.APIError
					if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
						return jf.ErrResultWithHint("Valid named sections include encoding (transcoding), network, branding, metadata, and livetv; use get_config for general server settings.", "Jellyfin API error: %v", err), nil, nil
					}
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if strings.EqualFold(args.Key, "livetv") {
					redactLiveTVConfig(config)
				}
				return jf.TextResult(fmt.Sprintf("Config section '%s':\n\n%s", args.Key, jf.FormatJSON(config))), nil, nil

			case "update_config_section":
				args.Key = canonicalConfigSectionKey(args.Key)
				if args.Key == "" {
					return jf.ErrResult("key is required for update_config_section. Common keys: encoding (transcoding), network, branding, metadata, livetv."), nil, nil
				}
				if args.Config == nil {
					return jf.ErrResult("config is required for update_config_section: provide a JSON object of fields to update in the section."), nil, nil
				}
				if strings.EqualFold(args.Key, "livetv") && containsRedaction(args.Config) {
					return jf.ErrResult("Nothing was changed: the config carries redacted values from get_config_section (tuner URLs and listing-provider credentials are shown redacted), and writing it would replace the real ones. Edit the Live TV settings in the Jellyfin dashboard instead."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				patch, ok := jf.AsConfigMap(args.Config)
				if !ok || len(patch) == 0 {
					return jf.ErrResult("config must be a non-empty JSON object of configuration fields for section '%s'.", args.Key), nil, nil
				}
				endpoint := fmt.Sprintf("/System/Configuration/%s", jf.SanitizeID(args.Key))
				var current map[string]any
				if err := client.Get(ctx, endpoint, nil, &current); err != nil {
					var apiErr *jf.APIError
					if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
						return jf.ErrResultWithHint("Valid named sections include encoding (transcoding), network, branding, metadata, and livetv; use get_config for general server settings.", "Jellyfin API error: %v", err), nil, nil
					}
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				merged := jf.MergeConfig(current, patch)
				changes := jf.DiffConfig(current, merged)
				if len(current) > 0 && len(changes) == 0 {
					return jf.TextResult(fmt.Sprintf("Config section '%s' already has those values; nothing changed.", args.Key)), nil, nil
				}
				var warning string
				if len(current) > 0 {
					warning = fmt.Sprintf("Update server '%s' configuration section? Changes: %s. Other settings in '%s' stay unchanged.", args.Key, jf.FormatConfigChanges(changes), args.Key)
				} else {
					warning = fmt.Sprintf("Update server '%s' configuration section? Settings: %s.", args.Key, jf.FormatConfigChanges(changes))
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				if err := client.PostNoContent(ctx, endpoint, nil, merged); err != nil {
					return jf.ErrResult("Failed to update config section: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Config section '%s' updated.", args.Key)), nil, nil

			case "list_backups":
				var backups any
				if err := client.Get(ctx, "/Backup", nil, &backups); err != nil {
					return jf.ErrResult("Failed to list backups: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Backups:\n\n%s", jf.FormatJSON(jf.LocalizeInstants(backups, "DateCreated")))), nil, nil

			case "backup_manifest":
				if args.FileName == "" {
					return jf.ErrResult("file_name is required for backup_manifest. Use 'list_backups' to find backups."), nil, nil
				}
				var manifest map[string]any
				if err := client.Get(ctx, "/Backup/Manifest", url.Values{"path": {args.FileName}}, &manifest); err != nil {
					var apiErr *jf.APIError
					if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
						return jf.ErrResultWithHint("Use 'list_backups' to find backups.", "Jellyfin's backup folder has no file named %q.", args.FileName), nil, nil
					}
					return jf.ErrResultWithHint("Use 'list_backups' to find backups.", "Failed to read the backup %q: %v", args.FileName, err), nil, nil
				}
				// Jellyfin answers 204, with no manifest, for a file in the
				// backup folder that is not a Jellyfin backup.
				if manifest == nil {
					return jf.ErrResult("%q is not a Jellyfin backup archive.", args.FileName), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Backup %s:\n\n%s", args.FileName, jf.FormatJSON(backupManifest(manifest)))), nil, nil

			case "create_backup":
				body := map[string]any{
					"Metadata":  true,
					"Trickplay": true,
					"Subtitles": true,
					"Database":  true,
				}
				if err := client.PostNoContent(ctx, "/Backup/Create", nil, body); err != nil {
					return jf.ErrResult("Failed to create backup: %v", err), nil, nil
				}
				return jf.TextResult("Backup creation started. Use 'list_backups' to check status."), nil, nil

			case "restore_backup":
				if args.FileName == "" {
					return jf.ErrResult("file_name is required for restore_backup: the backup's file name, or the Path list_backups shows for it. Use 'backup_manifest' to check what it contains first."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				var manifest map[string]any
				if err := client.Get(ctx, "/Backup/Manifest", url.Values{"path": {args.FileName}}, &manifest); err != nil {
					var apiErr *jf.APIError
					if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
						return jf.ErrResultWithHint("Use 'list_backups' to find backups.", "Jellyfin's backup folder has no file named %q.", args.FileName), nil, nil
					}
					return jf.ErrResultWithHint("Use 'list_backups' to find backups.", "Failed to read the backup %q: %v", args.FileName, err), nil, nil
				}
				if manifest == nil {
					return jf.ErrResult("%q is not a Jellyfin backup archive.", args.FileName), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, formatRestoreBackupWarning(args.FileName, backupManifest(manifest))); result != nil {
					return result, nil, nil
				}
				body := map[string]any{"ArchiveFileName": args.FileName}
				if err := client.PostNoContent(ctx, "/Backup/Restore", nil, body); err != nil {
					return jf.ErrResult("Failed to restore backup: %v", err), nil, nil
				}
				return jf.TextResult("Backup restore started. The server restarts and is unavailable until it comes back."), nil, nil

			case "list_tasks":
				var tasks []map[string]any
				if err := client.Get(ctx, "/ScheduledTasks", nil, &tasks); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(tasks))
				for _, t := range tasks {
					task := map[string]any{
						"id":       jf.GetString(t, "Id"),
						"name":     jf.GetString(t, "Name"),
						"state":    jf.GetString(t, "State"),
						"category": jf.GetString(t, "Category"),
					}
					if le := jf.ToMap(t["LastExecutionResult"]); le != nil {
						task["last_status"] = jf.GetString(le, "Status")
						task["last_end"] = jf.LocalDateTime(jf.GetString(le, "EndTimeUtc"))
					}
					items = append(items, task)
				}
				return jf.TextResult(fmt.Sprintf("Scheduled tasks (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "get_task":
				if args.TaskID == "" {
					return jf.ErrResult("task_id is required. Use 'list_tasks' to find task IDs."), nil, nil
				}
				var task map[string]any
				endpoint := fmt.Sprintf("/ScheduledTasks/%s", jf.SanitizeID(args.TaskID))
				if err := client.Get(ctx, endpoint, nil, &task); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(task, "StartTimeUtc", "EndTimeUtc"))), nil, nil

			case "start_task":
				if args.TaskID == "" {
					return jf.ErrResult("task_id is required. Use 'list_tasks' to find task IDs."), nil, nil
				}
				endpoint := fmt.Sprintf("/ScheduledTasks/Running/%s", jf.SanitizeID(args.TaskID))
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to start task: %v", err), nil, nil
				}
				return jf.TextResult("Task started."), nil, nil

			case "stop_task":
				if args.TaskID == "" {
					return jf.ErrResult("task_id is required."), nil, nil
				}
				// Jellyfin answers 500 to a stop of a task that is not running,
				// so the state is read first.
				var task map[string]any
				if err := client.Get(ctx, "/ScheduledTasks/"+jf.SanitizeID(args.TaskID), nil, &task); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if state := jf.GetString(task, "State"); state != "Running" {
					return jf.TextResult(fmt.Sprintf("Task '%s' is not running (state: %s), so there is nothing to stop.", jf.GetString(task, "Name"), state)), nil, nil
				}
				endpoint := fmt.Sprintf("/ScheduledTasks/Running/%s", jf.SanitizeID(args.TaskID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to stop task: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Task '%s' stopped.", jf.GetString(task, "Name"))), nil, nil

			case "set_task_triggers":
				if args.TaskID == "" {
					return jf.ErrResult("task_id is required for set_task_triggers."), nil, nil
				}
				if args.Triggers == nil {
					return jf.ErrResult("triggers is required for set_task_triggers. Use 'get_task' first to see current trigger format. Types: DailyTrigger, WeeklyTrigger, IntervalTrigger, StartupTrigger."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				triggers, ok := jf.AsSlice(args.Triggers)
				if !ok {
					return jf.ErrResult("triggers must be a JSON array of trigger objects for set_task_triggers. Use 'get_task' first to see current trigger format."), nil, nil
				}
				name := taskName(ctx, client, args.TaskID)
				var warning string
				if len(triggers) == 0 {
					warning = fmt.Sprintf("Remove all scheduled triggers from task '%s'? It will only run when started manually.", name)
				} else {
					warning = fmt.Sprintf("Replace scheduled triggers for task '%s' with: %s?", name, formatTriggerSummary(triggers))
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/ScheduledTasks/%s/Triggers", jf.SanitizeID(args.TaskID))
				if err := client.PostNoContent(ctx, endpoint, nil, triggers); err != nil {
					return jf.ErrResult("Failed to set triggers: %v", err), nil, nil
				}
				return jf.TextResult("Task triggers updated."), nil, nil

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
				return jf.ErrResult("Invalid action '%s'. Valid actions: get_config, get_config_section, update_config_section, list_backups, backup_manifest, create_backup, restore_backup, list_tasks, get_task, start_task, stop_task, set_task_triggers, restart, shutdown", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_devices ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_devices", AnnotDestructive, AnnotReadOnly); ok {
		roActions := []any{"list", "get", "api_keys"}
		schema := jf.WithEnums[jf.DevicesInput](map[string][]any{
			"action": {"list", "get", "delete", "api_keys", "create_api_key", "revoke_api_key"},
		})
		desc := "Inspect or revoke connected devices, and list, create, or revoke administrator API keys."
		if ro {
			schema = jf.WithEnumsFiltered[jf.DevicesInput](map[string][]any{"action": roActions}, []string{"action", "device_id", "limit", "days"})
			desc = "List connected devices, inspect device details, or list API keys (tokens masked)."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_devices",
			Title:       "Devices & API Keys",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.DevicesInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "list":
				var result map[string]any
				if err := client.Get(ctx, "/Devices", nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				rawItems := jf.ToSlice(result["Items"])
				totalDevices := len(rawItems)
				// Apply recency filter
				days := args.Days
				if days <= 0 {
					days = 30
				}
				cutoff := time.Now().AddDate(0, 0, -days)
				items := make([]map[string]any, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					lastActivity := jf.GetString(m, "DateLastActivity")
					if seen, ok := jf.ParseTime(lastActivity); !ok || seen.Before(cutoff) {
						continue
					}
					items = append(items, map[string]any{
						"id":            jf.GetString(m, "Id"),
						"name":          jf.GetString(m, "Name"),
						"app_name":      jf.GetString(m, "AppName"),
						"app_version":   jf.GetString(m, "AppVersion"),
						"last_user":     jf.GetString(m, "LastUserName"),
						"last_activity": jf.LocalDateTime(lastActivity),
					})
				}
				// Sort by last_activity descending
				sort.Slice(items, func(i, j int) bool {
					return jf.NewerFirst(jf.GetString(items[i], "last_activity"), jf.GetString(items[j], "last_activity"))
				})
				active := len(items)
				limit := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
				if len(items) > limit {
					items = items[:limit]
				}
				return jf.TextResult(fmt.Sprintf("Devices (%d total, %d active in last %d days, showing %d):\n\n%s", totalDevices, active, days, len(items), jf.FormatJSON(items))), nil, nil

			case "get":
				if args.DeviceID == "" {
					return jf.ErrResult("device_id is required. Use 'list' to find device IDs."), nil, nil
				}
				params := url.Values{"id": {args.DeviceID}}
				var device map[string]any
				if err := client.Get(ctx, "/Devices/Info", params, &device); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.LocalizeInstants(device, "DateLastActivity"))), nil, nil

			case "delete":
				if args.DeviceID == "" {
					return jf.ErrResult("device_id is required."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Remove device '%s'? It has to sign in again before it can connect.", deviceName(ctx, client, args.DeviceID))); result != nil {
					return result, nil, nil
				}
				params := url.Values{"id": {args.DeviceID}}
				if err := client.Del(ctx, "/Devices", params); err != nil {
					return jf.ErrResult("Failed to delete device: %v", err), nil, nil
				}
				return jf.TextResult("Device removed. It has to sign in again before it can connect."), nil, nil

			case "api_keys":
				var result map[string]any
				if err := client.Get(ctx, "/Auth/Keys", nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				rawItems := jf.ToSlice(result["Items"])
				items := make([]map[string]any, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					items = append(items, map[string]any{
						"access_token": jf.MaskToken(jf.GetString(m, "AccessToken")),
						"app_name":     jf.GetString(m, "AppName"),
						"date_created": jf.LocalDateTime(jf.GetString(m, "DateCreated")),
					})
				}
				return jf.TextResult(fmt.Sprintf("API keys (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "create_api_key":
				if args.AppName == "" {
					return jf.ErrResult("app_name is required for create_api_key."), nil, nil
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Create a new API key for application '%s'? API keys grant full administrator access to the Jellyfin server.", args.AppName)); result != nil {
					return result, nil, nil
				}
				params := url.Values{"app": {args.AppName}}
				if err := client.PostNoContent(ctx, "/Auth/Keys", params, nil); err != nil {
					return jf.ErrResult("Failed to create API key: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("API key created for '%s'. Open Dashboard -> Administration -> API Keys in Jellyfin to copy the key ('api_keys' masks tokens).", args.AppName)), nil, nil

			case "revoke_api_key":
				if args.Key == "" && args.AppName == "" {
					return jf.ErrResult("app_name (the key's application name from 'api_keys') or key (the full token) is required."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				// The delete route takes the full token, which api_keys shows
				// masked, so a key is usually named by its application.
				token, appName := args.Key, args.AppName
				if token == "" {
					var result map[string]any
					if err := client.Get(ctx, "/Auth/Keys", nil, &result); err != nil {
						return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
					}
					var matches []string
					for _, raw := range jf.ToSlice(result["Items"]) {
						if m := jf.ToMap(raw); jf.GetString(m, "AppName") == appName {
							matches = append(matches, jf.GetString(m, "AccessToken"))
						}
					}
					switch len(matches) {
					case 0:
						return jf.ErrResult("No API key is named '%s'. Use 'api_keys' to see the names.", appName), nil, nil
					case 1:
						token = matches[0]
					default:
						return jf.ErrResult("%d API keys are named '%s'. Ask the user for the full token of the one to revoke and pass it as key.", len(matches), appName), nil, nil
					}
				}
				if appName == "" {
					appName = jf.MaskToken(token)
				}
				warning := fmt.Sprintf("Revoke the API key '%s'? Any client using it loses access immediately.", appName)
				if selfKey := strings.TrimSpace(os.Getenv("JELLYFIN_API_KEY")); selfKey != "" && token == selfKey {
					warning = fmt.Sprintf("Revoke the API key '%s'? Warning: this is the JELLYFIN_API_KEY currently used by this MCP server, so revoking it immediately locks out this MCP connection as well as any other client using it.", appName)
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Auth/Keys/%s", jf.SanitizeID(token))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to revoke API key: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("API key '%s' revoked.", appName)), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, get, delete, api_keys, create_api_key, revoke_api_key", args.Action), nil, nil
			}
		})
	}
}

// backupManifest is the readable form of a backup's manifest: the Jellyfin
// version that made it, when, and which kinds of data it holds.
func backupManifest(m map[string]any) map[string]any {
	out := map[string]any{
		"server_version":        jf.GetString(m, "ServerVersion"),
		"backup_engine_version": jf.GetString(m, "BackupEngineVersion"),
		"date_created":          jf.LocalDateTime(jf.GetString(m, "DateCreated")),
		"path":                  jf.GetString(m, "Path"),
	}
	options := jf.ToMap(m["Options"])
	var contents []string
	for _, kind := range []string{"Database", "Metadata", "Subtitles", "Trickplay"} {
		if jf.GetBool(options, kind) {
			contents = append(contents, strings.ToLower(kind))
		}
	}
	out["contents"] = contents
	return out
}

// taskName returns a scheduled task's name for a message, or its ID when it cannot be read.
func taskName(ctx context.Context, client jf.Client, taskID string) string {
	var task map[string]any
	if err := client.Get(ctx, "/ScheduledTasks/"+jf.SanitizeID(taskID), nil, &task); err == nil {
		if name := jf.GetString(task, "Name"); name != "" {
			return name
		}
	}
	return taskID
}

// installedPlugin returns the installed plugin with the given ID, or nil when
// none has it.
func installedPlugin(ctx context.Context, client jf.Client, pluginID string) (map[string]any, error) {
	var plugins []map[string]any
	if err := client.Get(ctx, "/Plugins", nil, &plugins); err != nil {
		return nil, err
	}
	for _, p := range plugins {
		if jf.NormalizeID(jf.GetString(p, "Id")) == jf.NormalizeID(pluginID) {
			return p, nil
		}
	}
	return nil, nil
}

// pluginName returns an installed plugin's name for a message, or its ID when it cannot be read.
func pluginName(ctx context.Context, client jf.Client, pluginID string) string {
	if p, err := installedPlugin(ctx, client, pluginID); err == nil && p != nil {
		if name := jf.GetString(p, "Name"); name != "" {
			return name
		}
	}
	return pluginID
}

// deviceName returns a device's name and app for a message, or its ID when it cannot be read.
func deviceName(ctx context.Context, client jf.Client, deviceID string) string {
	var device map[string]any
	if err := client.Get(ctx, "/Devices/Info", url.Values{"id": {deviceID}}, &device); err == nil {
		if name := jf.GetString(device, "Name"); name != "" {
			if app := jf.GetString(device, "AppName"); app != "" && app != name {
				return name + " (" + app + ")"
			}
			return name
		}
	}
	return deviceID
}

const ticksPerSecond int64 = 10_000_000

func formatTriggerSummary(triggers []any) string {
	parts := make([]string, 0, len(triggers))
	for _, raw := range triggers {
		m := jf.ToMap(raw)
		if m == nil {
			parts = append(parts, "custom trigger")
			continue
		}
		trigType := triggerFieldString(m, "Type")
		switch trigType {
		case "DailyTrigger":
			if ticks, ok := triggerFieldTicks(m, "TimeOfDayTicks"); ok {
				parts = append(parts, "daily at "+formatTimeOfDayTicks(ticks))
			} else {
				parts = append(parts, "daily")
			}
		case "WeeklyTrigger":
			day := triggerFieldString(m, "DayOfWeek")
			if ticks, ok := triggerFieldTicks(m, "TimeOfDayTicks"); ok {
				if day != "" {
					parts = append(parts, fmt.Sprintf("weekly on %s at %s", day, formatTimeOfDayTicks(ticks)))
				} else {
					parts = append(parts, "weekly at "+formatTimeOfDayTicks(ticks))
				}
			} else if day != "" {
				parts = append(parts, "weekly on "+day)
			} else {
				parts = append(parts, "weekly")
			}
		case "IntervalTrigger":
			if ticks, ok := triggerFieldTicks(m, "IntervalTicks"); ok && ticks > 0 {
				parts = append(parts, "every "+formatIntervalTicks(ticks))
			} else {
				parts = append(parts, "on an interval")
			}
		case "StartupTrigger":
			parts = append(parts, "at server startup")
		default:
			if trigType != "" {
				parts = append(parts, trigType)
			} else {
				parts = append(parts, "custom trigger")
			}
		}
	}
	return strings.Join(parts, ", ")
}

func triggerFieldString(m map[string]any, key string) string {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func triggerFieldTicks(m map[string]any, key string) (int64, bool) {
	for k, v := range m {
		if !strings.EqualFold(k, key) {
			continue
		}
		switch n := v.(type) {
		case float64:
			return int64(n), true
		case int64:
			return n, true
		case int:
			return int64(n), true
		}
	}
	return 0, false
}

func formatTimeOfDayTicks(ticks int64) string {
	totalMinutes := (ticks / ticksPerSecond) / 60
	hours := (totalMinutes / 60) % 24
	minutes := totalMinutes % 60
	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

func formatIntervalTicks(ticks int64) string {
	seconds := ticks / ticksPerSecond
	if seconds >= 3600 && seconds%3600 == 0 {
		hours := seconds / 3600
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	if seconds >= 60 && seconds%60 == 0 {
		mins := seconds / 60
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	if seconds == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", seconds)
}

// repoEnabled returns m's Enabled flag, defaulting to true when omitted to
// match Jellyfin's RepositoryInfo.Enabled default.
func repoEnabled(m map[string]any) bool {
	for k, v := range m {
		if strings.EqualFold(k, "Enabled") {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return true
}

func reposEqual(current []map[string]any, next []any) bool {
	if len(current) != len(next) {
		return false
	}
	for i, cur := range current {
		m := jf.ToMap(next[i])
		if m == nil {
			return false
		}
		if triggerFieldString(cur, "Name") != triggerFieldString(m, "Name") ||
			triggerFieldString(cur, "Url") != triggerFieldString(m, "Url") ||
			repoEnabled(cur) != repoEnabled(m) {
			return false
		}
	}
	return true
}

func formatRepoEntry(m map[string]any) string {
	name := triggerFieldString(m, "Name")
	repoURL := triggerFieldString(m, "Url")
	switch {
	case name != "" && repoURL != "":
		return fmt.Sprintf("'%s' (%s)", name, repoURL)
	case repoURL != "":
		return fmt.Sprintf("'%s'", repoURL)
	case name != "":
		return fmt.Sprintf("'%s'", name)
	default:
		return "(unnamed repository)"
	}
}

func formatRepoWarning(current []map[string]any, next []any) string {
	if len(next) == 0 {
		return "Remove all configured plugin repositories? You will not be able to browse or install plugin packages until a repository is added."
	}
	nextMaps := make([]map[string]any, 0, len(next))
	nextLabels := make([]string, 0, len(next))
	nextByURL := make(map[string]string, len(next))
	for _, raw := range next {
		m := jf.ToMap(raw)
		if m == nil {
			continue
		}
		nextMaps = append(nextMaps, m)
		label := formatRepoEntry(m)
		nextLabels = append(nextLabels, label)
		if u := strings.ToLower(triggerFieldString(m, "Url")); u != "" {
			nextByURL[u] = label
		}
	}
	if len(current) == 0 {
		return fmt.Sprintf("Set plugin repositories to %s? Only add repositories you trust, because plugins run third-party code on the Jellyfin server.", strings.Join(nextLabels, ", "))
	}
	curByURL := make(map[string]string, len(current))
	for _, m := range current {
		if u := strings.ToLower(triggerFieldString(m, "Url")); u != "" {
			curByURL[u] = formatRepoEntry(m)
		}
	}
	var added, removed []string
	for _, m := range nextMaps {
		u := strings.ToLower(triggerFieldString(m, "Url"))
		if u != "" && curByURL[u] == "" {
			added = append(added, formatRepoEntry(m))
		}
	}
	for _, m := range current {
		u := strings.ToLower(triggerFieldString(m, "Url"))
		if u != "" && nextByURL[u] == "" {
			removed = append(removed, formatRepoEntry(m))
		}
	}
	var details []string
	if len(added) > 0 {
		details = append(details, "add "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		details = append(details, "remove "+strings.Join(removed, ", "))
	}
	if len(details) == 0 {
		return fmt.Sprintf("Update configured plugin repositories (%s)?", strings.Join(nextLabels, ", "))
	}
	note := ""
	if len(added) > 0 {
		note = " Only add repositories you trust, because plugins run third-party code on the Jellyfin server."
	}
	return fmt.Sprintf("Update plugin repositories (%s)?%s", strings.Join(details, "; "), note)
}

func formatRestoreBackupWarning(fileName string, manifest map[string]any) string {
	var meta []string
	if created := jf.GetString(manifest, "date_created"); created != "" {
		meta = append(meta, "created "+created)
	}
	if ver := jf.GetString(manifest, "server_version"); ver != "" {
		meta = append(meta, "Jellyfin "+ver)
	}
	var replaces string
	if contents, ok := manifest["contents"].([]string); ok && len(contents) > 0 {
		replaces = " and replaces current " + strings.Join(contents, ", ")
	} else {
		replaces = " and replaces the current server data"
	}
	if len(meta) > 0 {
		return fmt.Sprintf("Restore the server from backup '%s' (%s)? The server restarts%s.", fileName, strings.Join(meta, ", "), replaces)
	}
	return fmt.Sprintf("Restore the server from backup '%s'? The server restarts%s.", fileName, replaces)
}

// canonicalConfigSectionKey maps unambiguous transcoding aliases such as
// "MediaEncoding" (Jellyfin's XML config filename) or "transcoding" to the
// REST section key "encoding", preserving all other keys as given.
func canonicalConfigSectionKey(key string) string {
	trimmed := strings.TrimSpace(key)
	switch strings.ToLower(trimmed) {
	case "mediaencoding", "transcoding", "encodingoptions":
		return "encoding"
	default:
		return trimmed
	}
}
