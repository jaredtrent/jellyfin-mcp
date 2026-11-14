package tools

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminUserTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_users ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_users", AnnotDestructive, AnnotReadOnly); ok {
		roActions := []any{"list", "get", "qc_status"}
		schema := jf.WithEnums[jf.UsersInput](map[string][]any{
			"action": {"list", "get", "create", "delete", "update_policy", "update_password", "update_config", "qc_status", "qc_initiate", "qc_authorize"},
		})
		desc := "Manage Jellyfin user accounts (permissions, library access, passwords, language/audio preferences) and Quick Connect device sign-in."
		if ro {
			schema = jf.WithEnumsFiltered[jf.UsersInput](map[string][]any{"action": roActions}, []string{"action", "user_id"})
			desc = "List Jellyfin user accounts, inspect user details/permissions, or check Quick Connect status."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_users",
			Title:       "User Management",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.UsersInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "list":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is not supported for list (use action=get with user_id to inspect one user)."), nil, nil
				}
				var users []map[string]any
				if err := client.Get(ctx, "/Users", nil, &users); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(users))
				for _, u := range users {
					items = append(items, jf.ExtractUserSummary(u))
				}
				return jf.TextResult(fmt.Sprintf("Users (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "get":
				if args.UserID == "" {
					return jf.ErrResult("user_id is required. Use 'list' to find user IDs."), nil, nil
				}
				_, _, user, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				detailed := jf.ExtractDetailedUser(user)
				if ids, ok := detailed["enabled_folders"].([]string); ok && len(ids) > 0 {
					var libs []map[string]any
					if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err == nil && len(libs) > 0 {
						nameByID := make(map[string]string, len(libs)*2)
						for _, lib := range libs {
							id := jf.GetString(lib, "ItemId")
							name := jf.GetString(lib, "Name")
							if id != "" && name != "" {
								nameByID[id] = name
								nameByID[jf.NormalizeID(id)] = name
							}
						}
						names := make([]string, 0, len(ids))
						for _, id := range ids {
							if name := nameByID[id]; name != "" {
								names = append(names, name)
							} else if name := nameByID[jf.NormalizeID(id)]; name != "" {
								names = append(names, name)
							}
						}
						if len(names) > 0 {
							detailed["enabled_folder_names"] = names
						}
					}
				}
				return jf.TextResult(jf.FormatJSON(detailed)), nil, nil

			case "create":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is not supported for create (pass username to create a new account)."), nil, nil
				}
				if args.Username == "" {
					return jf.ErrResult("username is required to create a user."), nil, nil
				}
				body := map[string]any{"Name": args.Username}
				if args.Password != "" {
					body["Password"] = args.Password
				}
				var result map[string]any
				if err := client.Post(ctx, "/Users/New", nil, body, &result); err != nil {
					return jf.ErrResult("Failed to create user: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("User '%s' created (ID: %s).", args.Username, jf.GetString(result, "Id"))), nil, nil

			case "delete":
				if args.UserID == "" {
					return jf.ErrResult("user_id is required. Use 'list' to find user IDs."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				uid, name, _, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				if name == "" {
					name = uid
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete user '%s' permanently? Their watch history, settings, and other data are lost.", name)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(uid))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete user: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("User '%s' deleted.", name)), nil, nil

			case "update_policy":
				args.EnabledFolderIDs = jf.SplitIDs(args.EnabledFolderIDs)
				if args.UserID == "" {
					return jf.ErrResult("user_id is required. Use 'list' to find user IDs."), nil, nil
				}
				if args.IsAdmin == nil && args.IsDisabled == nil && args.EnableAllFolders == nil && len(args.EnabledFolderIDs) == 0 {
					return jf.ErrResult("Provide at least one of is_admin, is_disabled, enable_all_folders, or enabled_folder_ids for update_policy."), nil, nil
				}
				if args.EnableAllFolders != nil && *args.EnableAllFolders && len(args.EnabledFolderIDs) > 0 {
					return jf.ErrResult("enable_all_folders=true cannot be combined with enabled_folder_ids; omit enable_all_folders or set it to false to restrict library access."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}

				jf.ReportProgress(ctx, req, 0, 2, "Fetching current policy...")

				uid, name, user, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				if name == "" {
					name = uid
				}
				var folderNames map[string]string
				if len(args.EnabledFolderIDs) > 0 {
					allGUIDs := !slices.ContainsFunc(args.EnabledFolderIDs, func(s string) bool { return !jf.IsJellyfinID(s) })
					if !allGUIDs || jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
						resolvedFolders, fNames, err := jf.ResolveLibraryIDs(ctx, client, args.EnabledFolderIDs)
						if err != nil {
							return jf.ResolveErrResult(err), nil, nil
						}
						args.EnabledFolderIDs = resolvedFolders
						folderNames = fNames
					}
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Update permissions for user '%s'? Changes: %s.", name, policyChanges(args, folderNames))); result != nil {
					return result, nil, nil
				}

				policy := jf.ToMap(user["Policy"])
				if policy == nil {
					policy = make(map[string]any)
				}

				jf.ReportProgress(ctx, req, 1, 2, "Applying policy changes...")

				if args.IsAdmin != nil {
					policy["IsAdministrator"] = *args.IsAdmin
				}
				if args.IsDisabled != nil {
					policy["IsDisabled"] = *args.IsDisabled
				}
				if len(args.EnabledFolderIDs) > 0 {
					policy["EnableAllFolders"] = false
					policy["EnabledFolders"] = args.EnabledFolderIDs
				} else if args.EnableAllFolders != nil {
					policy["EnableAllFolders"] = *args.EnableAllFolders
				}
				policyEndpoint := fmt.Sprintf("/Users/%s/Policy", jf.SanitizeID(uid))
				if err := client.PostNoContent(ctx, policyEndpoint, nil, policy); err != nil {
					return jf.ErrResult("Failed to update policy: %v", err), nil, nil
				}
				return jf.TextResult("User policy updated."), nil, nil

			case "update_password":
				if args.UserID == "" || args.Password == "" {
					return jf.ErrResult("user_id and password are required for update_password."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				uid, name, _, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				if name == "" {
					name = uid
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Change the password for user '%s'? Their old password stops working immediately, and their signed-in devices must sign in again.", name)); result != nil {
					return result, nil, nil
				}
				body := map[string]any{
					"NewPw": args.Password,
				}
				params := url.Values{"userId": {uid}}
				if err := client.PostNoContent(ctx, "/Users/Password", params, body); err != nil {
					return jf.ErrResult("Failed to update password: %v", err), nil, nil
				}
				return jf.TextResult("Password updated."), nil, nil

			case "update_config":
				if args.UserID == "" {
					return jf.ErrResult("user_id is required. Use 'list' to find user IDs."), nil, nil
				}
				hasParam := args.SubtitleLanguage != "" || args.AudioLanguage != "" || args.PlayDefaultAudio != nil
				if !hasParam && args.Config == nil {
					return jf.ErrResult("Provide subtitle_language, audio_language, or play_default_audio to update preferences, or config to merge raw configuration fields."), nil, nil
				}
				if args.Config != nil {
					if res := jf.DestructiveDisabledError(ctx); res != nil {
						return res, nil, nil
					}
					rawPatch, ok := jf.AsConfigMap(args.Config)
					if !ok || len(rawPatch) == 0 {
						return jf.ErrResult("config must be a non-empty JSON object of UserConfiguration fields."), nil, nil
					}
					patch := jf.MergeConfig(nil, rawPatch)
					if args.SubtitleLanguage != "" {
						patch = jf.MergeConfig(patch, map[string]any{"SubtitleLanguagePreference": args.SubtitleLanguage})
					}
					if args.AudioLanguage != "" {
						patch = jf.MergeConfig(patch, map[string]any{"AudioLanguagePreference": args.AudioLanguage})
					}
					if args.PlayDefaultAudio != nil {
						patch = jf.MergeConfig(patch, map[string]any{"PlayDefaultAudioTrack": *args.PlayDefaultAudio})
					}
					uid, name, user, err := jf.ResolveUser(ctx, client, args.UserID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					if name == "" {
						name = uid
					}
					params := url.Values{"userId": {uid}}
					current, _ := jf.AsConfigMap(user["Configuration"])
					merged := jf.MergeConfig(current, patch)
					changes := jf.DiffConfig(current, merged)
					if len(current) > 0 && len(changes) == 0 {
						return jf.TextResult(fmt.Sprintf("User '%s' already has that configuration; nothing changed.", name)), nil, nil
					}
					var warning string
					if len(current) > 0 {
						warning = fmt.Sprintf("Update configuration for user '%s'? Changes: %s. Other user preferences stay unchanged.", name, jf.FormatConfigChanges(changes))
					} else {
						warning = fmt.Sprintf("Update configuration for user '%s'? Settings: %s.", name, jf.FormatConfigChanges(changes))
					}
					if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
						return result, nil, nil
					}
					if err := client.PostNoContent(ctx, "/Users/Configuration", params, merged); err != nil {
						return jf.ErrResult("Failed to update user config: %v", err), nil, nil
					}
					return jf.TextResult("User configuration updated."), nil, nil
				}
				// Fetch-merge-POST (like update_policy)
				uid, _, user, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				params := url.Values{"userId": {uid}}
				config := jf.ToMap(user["Configuration"])
				if config == nil {
					config = make(map[string]any)
				}
				if args.SubtitleLanguage != "" {
					config["SubtitleLanguagePreference"] = args.SubtitleLanguage
				}
				if args.AudioLanguage != "" {
					config["AudioLanguagePreference"] = args.AudioLanguage
				}
				if args.PlayDefaultAudio != nil {
					config["PlayDefaultAudioTrack"] = *args.PlayDefaultAudio
				}
				if err := client.PostNoContent(ctx, "/Users/Configuration", params, config); err != nil {
					return jf.ErrResult("Failed to update user config: %v", err), nil, nil
				}
				return jf.TextResult("User configuration updated."), nil, nil

			case "qc_status":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is not supported for qc_status."), nil, nil
				}
				var enabled bool
				if err := client.Get(ctx, "/QuickConnect/Enabled", nil, &enabled); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if enabled {
					return jf.TextResult("Quick Connect is enabled."), nil, nil
				}
				return jf.TextResult("Quick Connect is disabled."), nil, nil

			case "qc_initiate":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is not supported for qc_initiate."), nil, nil
				}
				var result map[string]any
				if err := client.Post(ctx, "/QuickConnect/Initiate", nil, nil, &result); err != nil {
					return jf.ErrResult("Failed to initiate Quick Connect: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Quick Connect initiated:\n\n%s", jf.FormatJSON(jf.LocalizeInstants(result, "DateAdded")))), nil, nil

			case "qc_authorize":
				if args.Code == "" {
					return jf.ErrResult("code is required for qc_authorize. Ask the user for the Quick Connect code displayed on the device they want to sign in."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				var targetUserID, name string
				if args.UserID != "" {
					uid, resolvedName, _, err := jf.ResolveUser(ctx, client, args.UserID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					targetUserID = uid
					name = resolvedName
					if name == "" {
						name = uid
					}
				} else {
					uid, err := client.GetUserID(ctx)
					if err != nil {
						return jf.ErrResult("Jellyfin error: %v", err), nil, nil
					}
					targetUserID = uid
					name = userName(ctx, client, targetUserID)
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Authorize Quick Connect code '%s' for user '%s'? The device showing this code signs in to their account.", args.Code, name)); result != nil {
					return result, nil, nil
				}
				params := url.Values{"code": {args.Code}}
				if targetUserID != "" {
					params.Set("userId", targetUserID)
				}
				if err := client.PostNoContent(ctx, "/QuickConnect/Authorize", params, nil); err != nil {
					return jf.ErrResult("Failed to authorize Quick Connect: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Quick Connect code authorized for user '%s'.", name)), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, get, create, delete, update_policy, update_password, update_config, qc_status, qc_initiate, qc_authorize", args.Action), nil, nil
			}
		})
	}
}

// userName returns the user's name for a message, or the ID when it cannot be read.
func userName(ctx context.Context, client jf.Client, userID string) string {
	var user map[string]any
	if err := client.Get(ctx, "/Users/"+jf.SanitizeID(userID), nil, &user); err == nil {
		if name := jf.GetString(user, "Name"); name != "" {
			return name
		}
	}
	return userID
}

// policyChanges describes the policy fields an update_policy call sets in plain English.
func policyChanges(args jf.UsersInput, folderNames map[string]string) string {
	var parts []string
	if args.IsAdmin != nil {
		if *args.IsAdmin {
			parts = append(parts, "grant administrator access")
		} else {
			parts = append(parts, "remove administrator access")
		}
	}
	if args.IsDisabled != nil {
		if *args.IsDisabled {
			parts = append(parts, "disable account")
		} else {
			parts = append(parts, "re-enable account")
		}
	}
	if len(args.EnabledFolderIDs) > 0 {
		parts = append(parts, fmt.Sprintf("restrict library access to %s", nameList(args.EnabledFolderIDs, folderNames)))
	} else if args.EnableAllFolders != nil {
		if *args.EnableAllFolders {
			parts = append(parts, "grant access to all libraries")
		} else {
			parts = append(parts, "remove access to all libraries")
		}
	}
	return strings.Join(parts, ", ")
}
