package tools

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterPlaybackTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_sessions ---
	if enabled("jellyfin_sessions", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_sessions",
			Title: "Sessions",
			InputSchema: jf.WithEnums[jf.SessionsInput](map[string][]any{
				"action": {"list", "resume"},
			}),
			OutputSchema: jf.SchemaFor[jf.SessionsOutput](),
			Description: "List active client sessions (playback status, direct/remux/transcode details, and session IDs for jellyfin_playback) or resumable in-progress items. " +
				"Session IDs change on reconnect, so list sessions before sending playback commands.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SessionsInput) (*mcp.CallToolResult, *jf.SessionsOutput, error) {
			switch args.Action {
			case "list":
				targetUser := strings.TrimSpace(args.UserID)
				var resolvedUID string
				if targetUser != "" {
					uid, err := jf.ResolveUserID(ctx, client, targetUser)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					resolvedUID = uid
				}
				var sessions []map[string]any
				if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				if targetUser != "" {
					normUID := jf.NormalizeID(resolvedUID)
					filtered := make([]map[string]any, 0, len(sessions))
					for _, s := range sessions {
						if (normUID != "" && jf.NormalizeID(jf.GetString(s, "UserId")) == normUID) ||
							strings.EqualFold(jf.GetString(s, "UserName"), targetUser) {
							filtered = append(filtered, s)
						}
					}
					sessions = filtered
				}
				// Sessions that are playing come first.
				infos := jf.SessionsFrom(sessions)
				slices.SortStableFunc(infos, func(a, b jf.SessionInfo) int {
					return strings.Compare(b.Status, a.Status)
				})
				return nil, &jf.SessionsOutput{Sessions: &infos}, nil

			case "resume":
				userID, err := jf.EffectiveUserID(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
				// /UserItems/Resume lists what the user is partway through,
				// most recently played first. It leaves out what a session is
				// playing now, and on Jellyfin 12 it lists the version of a
				// title that was actually played.
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview"},
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/UserItems/Resume", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return nil, &jf.SessionsOutput{Resume: &items, Notes: moreNote(len(items), total, maxItems, "")}, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, resume", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_playback ---
	if enabled("jellyfin_playback", AnnotWriteCreate) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_playback",
			Title: "Playback & Remote Control",
			InputSchema: jf.WithEnums[jf.PlaybackInput](map[string][]any{
				"action": {"PlayNow", "PlayNext", "PlayLast", "Pause", "Unpause", "Stop", "NextTrack", "PreviousTrack", "Seek", "Mute", "Unmute", "ToggleMute", "SetVolume", "SendMessage", "GoHome", "GoToSettings", "ChannelUp", "ChannelDown", "DisplayContent"},
			}),
			Description: "Start/queue playback or send transport, volume, message, and UI navigation commands to a controllable client session (get session_id from jellyfin_sessions list).",
			Annotations: AnnotWriteCreate,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PlaybackInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "PlayNow", "PlayNext", "PlayLast":
				ids := jf.SplitIDs(args.ItemIDs)
				if len(ids) == 0 && args.ItemID != "" {
					ids = jf.SplitIDs([]string{args.ItemID})
				}
				if len(ids) == 0 {
					return jf.ErrResult("item_ids is required. Provide at least one item ID to play."), nil, nil
				}
				sessionID, errRes := controllableSession(ctx, client, args.SessionID)
				if errRes != nil {
					return errRes, nil, nil
				}
				params := url.Values{
					"PlayCommand": {args.Action},
				}
				for _, itemID := range ids {
					params.Add("ItemIds", itemID)
				}
				if args.StartIndex != nil {
					params.Set("StartIndex", fmt.Sprintf("%d", *args.StartIndex))
				}
				endpoint := fmt.Sprintf("/Sessions/%s/Playing", sessionID)
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to start playback: %v. Verify the session_id is valid using jellyfin_sessions.", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Playback started: %d items queued with command '%s'.", len(ids), args.Action)), nil, nil
			}

			sessionID, errRes := controllableSession(ctx, client, args.SessionID)
			if errRes != nil {
				return errRes, nil, nil
			}

			switch args.Action {
			case "Pause", "Unpause", "Stop", "NextTrack", "PreviousTrack":
				endpoint := fmt.Sprintf("/Sessions/%s/Playing/%s", sessionID, args.Action)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v. Verify the session_id is valid using jellyfin_sessions.", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Action)), nil, nil

			case "Seek":
				if args.SeekTicks == nil {
					return jf.ErrResult("seek_position_ticks is required for Seek command (10,000,000 ticks = 1 second)."), nil, nil
				}
				endpoint := fmt.Sprintf("/Sessions/%s/Playing/Seek", sessionID)
				params := url.Values{"seekPositionTicks": {fmt.Sprintf("%d", *args.SeekTicks)}}
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to seek: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Seeked to position %d ticks.", *args.SeekTicks)), nil, nil

			case "Mute", "Unmute", "ToggleMute":
				endpoint := fmt.Sprintf("/Sessions/%s/System/%s", sessionID, args.Action)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Action)), nil, nil

			case "SetVolume":
				if args.Volume == nil {
					return jf.ErrResult("volume (0-100) is required for SetVolume command."), nil, nil
				}
				if *args.Volume < 0 || *args.Volume > 100 {
					return jf.ErrResult("volume must be between 0 and 100, got %d.", *args.Volume), nil, nil
				}
				// The System/{command} route builds its command from the route alone and
				// cannot carry a level, so the volume travels as a GeneralCommand argument.
				endpoint := fmt.Sprintf("/Sessions/%s/Command", sessionID)
				body := map[string]any{
					"Name":      "SetVolume",
					"Arguments": map[string]string{"Volume": strconv.Itoa(*args.Volume)},
				}
				if err := client.PostNoContent(ctx, endpoint, nil, body); err != nil {
					return jf.ErrResult("Failed to set volume: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Volume set to %d.", *args.Volume)), nil, nil

			case "SendMessage":
				if args.Message == "" {
					return jf.ErrResult("message text is required for SendMessage command."), nil, nil
				}
				endpoint := fmt.Sprintf("/Sessions/%s/Message", sessionID)
				body := map[string]any{"Text": args.Message}
				if err := client.PostNoContent(ctx, endpoint, nil, body); err != nil {
					return jf.ErrResult("Failed to send message: %v", err), nil, nil
				}
				return jf.TextResult("Message sent to session."), nil, nil

			case "GoHome", "GoToSettings", "ChannelUp", "ChannelDown":
				endpoint := fmt.Sprintf("/Sessions/%s/System/%s", sessionID, args.Action)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Action)), nil, nil

			case "DisplayContent":
				targetItem := args.ItemID
				if targetItem == "" {
					if ids := jf.SplitIDs(args.ItemIDs); len(ids) == 1 {
						targetItem = ids[0]
					}
				}
				if targetItem == "" {
					return jf.ErrResult("item_id is required for DisplayContent. Use jellyfin_search to find item IDs."), nil, nil
				}
				// Fetch item to get required type and name for the Viewing endpoint
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var item map[string]any
				itemEndpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(targetItem))
				itemParams := url.Values{"UserId": {userID}}
				if err := client.Get(ctx, itemEndpoint, itemParams, &item); err != nil {
					return jf.ErrResultWithHint("Use jellyfin_search to find valid item IDs.", "Failed to look up item: %v", err), nil, nil
				}
				endpoint := fmt.Sprintf("/Sessions/%s/Viewing", sessionID)
				params := url.Values{
					"itemType": {jf.GetString(item, "Type")},
					"itemId":   {targetItem},
					"itemName": {jf.GetString(item, "Name")},
				}
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to display content: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Client navigated to '%s'.", jf.GetString(item, "Name"))), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: PlayNow, PlayNext, PlayLast, Pause, Unpause, Stop, NextTrack, PreviousTrack, Seek, Mute, Unmute, ToggleMute, SetVolume, SendMessage, GoHome, GoToSettings, ChannelUp, ChannelDown, DisplayContent", args.Action), nil, nil
			}
		})
	}
}

// controllableSession resolves sessionID (by exact session ID or unambiguous
// DeviceName) and returns an error result when no active session matches or
// when the target client does not accept remote control.
func controllableSession(ctx context.Context, client jf.Client, sessionID string) (string, *mcp.CallToolResult) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "", jf.ErrResult("session_id is required. Use jellyfin_sessions list to find active sessions.")
	}
	var sessions []map[string]any
	if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
		return "", jf.ErrResult("Jellyfin API error: %v", err)
	}
	normTarget := jf.NormalizeID(trimmed)
	for _, s := range sessions {
		id := jf.GetString(s, "Id")
		if id == "" || (id != trimmed && jf.NormalizeID(id) != normTarget) {
			continue
		}
		if !jf.GetBool(jf.ToMap(s["Capabilities"]), "SupportsMediaControl") {
			return "", jf.ErrResult("Session '%s' (%s) does not accept remote control, so nothing was sent. Use jellyfin_sessions list and pick a session whose supports_media_control is true.", jf.GetString(s, "DeviceName"), jf.GetString(s, "Client"))
		}
		return jf.SanitizeID(id), nil
	}

	var exact, fold []map[string]any
	var controllableDevices []string
	for _, s := range sessions {
		id := jf.GetString(s, "Id")
		dev := jf.GetString(s, "DeviceName")
		if id == "" || dev == "" {
			continue
		}
		if jf.GetBool(jf.ToMap(s["Capabilities"]), "SupportsMediaControl") && !slices.Contains(controllableDevices, dev) {
			controllableDevices = append(controllableDevices, dev)
		}
		if dev == trimmed {
			exact = append(exact, s)
		}
		if strings.EqualFold(dev, trimmed) {
			fold = append(fold, s)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = fold
	}
	if len(matches) == 1 {
		s := matches[0]
		if !jf.GetBool(jf.ToMap(s["Capabilities"]), "SupportsMediaControl") {
			return "", jf.ErrResult("Session '%s' (%s) does not accept remote control, so nothing was sent. Use jellyfin_sessions list and pick a session whose supports_media_control is true.", jf.GetString(s, "DeviceName"), jf.GetString(s, "Client"))
		}
		return jf.SanitizeID(jf.GetString(s, "Id")), nil
	}
	if len(matches) > 1 {
		var ctrl []map[string]any
		for _, s := range matches {
			if jf.GetBool(jf.ToMap(s["Capabilities"]), "SupportsMediaControl") {
				ctrl = append(ctrl, s)
			}
		}
		if len(ctrl) == 1 {
			return jf.SanitizeID(jf.GetString(ctrl[0], "Id")), nil
		}
		return "", jf.ErrResult("Multiple active sessions match device '%s'. Use jellyfin_sessions list and pass the exact session_id.", trimmed)
	}

	if len(controllableDevices) > 0 {
		return "", jf.ErrResult("No active session has ID '%s' (controllable devices: %s). Use jellyfin_sessions list to find active sessions.", trimmed, strings.Join(controllableDevices, ", "))
	}
	return "", jf.ErrResult("No active session has ID '%s'. Use jellyfin_sessions list to find active sessions.", trimmed)
}
