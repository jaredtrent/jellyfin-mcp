package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminUserTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_users ---
	if enabled("jellyfin_users", AnnotDestructive) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_users",
			Title: "User Management",
			InputSchema: jf.WithEnums[jf.UsersInput](map[string][]any{
				"action": {"list", "get", "create", "delete", "update_policy", "update_password", "update_config", "qc_status", "qc_initiate", "qc_authorize"},
			}),
			Description: "Manage Jellyfin user accounts. Use 'list' to see all users, 'get' for user details, 'create' to add a new user, " +
				"'delete' to remove a user (destructive, cannot be undone). " +
				"Use 'update_policy' to change permissions: is_admin, is_disabled, enable_all_folders, and enabled_folder_ids (library access). " +
				"Use 'update_password' to change a user's password. " +
				"Use 'update_config' to set per-user preferences: subtitle_language, audio_language, play_default_audio. " +
				"Quick Connect: 'qc_status' checks if enabled, 'qc_initiate' starts a code exchange, 'qc_authorize' authorizes a code. " +
				"User IDs come from the 'list' action.",
			Annotations: AnnotDestructive,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.UsersInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "list":
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
				var user map[string]any
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(args.UserID))
				if err := client.Get(ctx, endpoint, nil, &user); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(jf.ExtractDetailedUser(user))), nil, nil

			case "create":
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
				name := userName(ctx, client, args.UserID)
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete user '%s' permanently? Their watch history, settings, and other data are lost.", name)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(args.UserID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete user: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("User '%s' deleted.", name)), nil, nil

			case "update_policy":
				if args.UserID == "" {
					return jf.ErrResult("user_id is required. Use 'list' to find user IDs."), nil, nil
				}
				if args.IsAdmin == nil && args.IsDisabled == nil && args.EnableAllFolders == nil && len(args.EnabledFolderIDs) == 0 {
					return jf.ErrResult("Provide at least one of is_admin, is_disabled, enable_all_folders, or enabled_folder_ids for update_policy."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Change the permissions of user '%s'? Changes: %s.", userName(ctx, client, args.UserID), policyChanges(args))); result != nil {
					return result, nil, nil
				}

				jf.ReportProgress(ctx, req, 0, 2, "Fetching current policy...")

				var user map[string]any
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(args.UserID))
				if err := client.Get(ctx, endpoint, nil, &user); err != nil {
					return jf.ErrResult("Failed to get user: %v", err), nil, nil
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
				if args.EnableAllFolders != nil {
					policy["EnableAllFolders"] = *args.EnableAllFolders
				}
				if len(args.EnabledFolderIDs) > 0 {
					policy["EnabledFolders"] = args.EnabledFolderIDs
				}
				policyEndpoint := fmt.Sprintf("/Users/%s/Policy", jf.SanitizeID(args.UserID))
				if err := client.PostNoContent(ctx, policyEndpoint, nil, policy); err != nil {
					return jf.ErrResult("Failed to update policy: %v", err), nil, nil
				}
				return jf.TextResult("User policy updated."), nil, nil

			case "update_password":
				if args.UserID == "" || args.Password == "" {
					return jf.ErrResult("user_id and password are required for update_password."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Replace the password of user '%s'? Their signed-in clients keep working, and the old password stops working immediately.", userName(ctx, client, args.UserID))); result != nil {
					return result, nil, nil
				}
				body := map[string]any{
					"NewPw": args.Password,
				}
				params := url.Values{"userId": {args.UserID}}
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
					return jf.ErrResult("Provide subtitle_language, audio_language, or play_default_audio to update preferences, or config to replace the whole configuration."), nil, nil
				}
				params := url.Values{"userId": {args.UserID}}
				// A raw config replaces the user's whole configuration.
				if args.Config != nil {
					if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Replace the entire configuration of user '%s' with the config given? Every preference it leaves out is reset.", userName(ctx, client, args.UserID))); result != nil {
						return result, nil, nil
					}
					if err := client.PostNoContent(ctx, "/Users/Configuration", params, args.Config); err != nil {
						return jf.ErrResult("Failed to update user config: %v", err), nil, nil
					}
					return jf.TextResult("User configuration updated."), nil, nil
				}
				// Fetch-merge-POST (like update_policy)
				var user map[string]any
				endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(args.UserID))
				if err := client.Get(ctx, endpoint, nil, &user); err != nil {
					return jf.ErrResult("Failed to get user: %v", err), nil, nil
				}
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
				var enabled bool
				if err := client.Get(ctx, "/QuickConnect/Enabled", nil, &enabled); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if enabled {
					return jf.TextResult("Quick Connect is enabled."), nil, nil
				}
				return jf.TextResult("Quick Connect is disabled."), nil, nil

			case "qc_initiate":
				var result map[string]any
				if err := client.Post(ctx, "/QuickConnect/Initiate", nil, nil, &result); err != nil {
					return jf.ErrResult("Failed to initiate Quick Connect: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Quick Connect initiated:\n\n%s", jf.FormatJSON(jf.LocalizeInstants(result, "DateAdded")))), nil, nil

			case "qc_authorize":
				if args.Code == "" {
					return jf.ErrResult("code is required for qc_authorize. Get the code from qc_initiate."), nil, nil
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Authorize Quick Connect code '%s'? The device that shows it signs in as this user.", args.Code)); result != nil {
					return result, nil, nil
				}
				params := url.Values{"code": {args.Code}}
				if err := client.PostNoContent(ctx, "/QuickConnect/Authorize", params, nil); err != nil {
					return jf.ErrResult("Failed to authorize Quick Connect: %v", err), nil, nil
				}
				return jf.TextResult("Quick Connect code authorized."), nil, nil

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

// policyChanges describes the policy fields an update_policy call sets.
func policyChanges(args jf.UsersInput) string {
	var parts []string
	if args.IsAdmin != nil {
		parts = append(parts, fmt.Sprintf("administrator %s", onOff(*args.IsAdmin)))
	}
	if args.IsDisabled != nil {
		parts = append(parts, fmt.Sprintf("account disabled %s", onOff(*args.IsDisabled)))
	}
	if args.EnableAllFolders != nil {
		parts = append(parts, fmt.Sprintf("access to all libraries %s", onOff(*args.EnableAllFolders)))
	}
	if len(args.EnabledFolderIDs) > 0 {
		parts = append(parts, fmt.Sprintf("access limited to %d %s", len(args.EnabledFolderIDs), plural(len(args.EnabledFolderIDs), "library", "libraries")))
	}
	return strings.Join(parts, ", ")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
