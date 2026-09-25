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
			Description: "View active playback sessions and resumable items. Use action 'list' to see all connected clients, what they're playing, " +
				"and their session IDs (needed for jellyfin_playback_control and jellyfin_play). " +
				"Use action 'resume' to see items with in-progress playback that can be continued. " +
				"Session IDs change when clients reconnect, so always list sessions before sending playback commands.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SessionsInput) (*mcp.CallToolResult, *jf.SessionsOutput, error) {
			switch args.Action {
			case "list":
				var sessions []map[string]any
				if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				// Sessions that are playing come first.
				infos := jf.SessionsFrom(sessions)
				slices.SortStableFunc(infos, func(a, b jf.SessionInfo) int {
					return strings.Compare(b.Status, a.Status)
				})
				return nil, &jf.SessionsOutput{Sessions: &infos}, nil

			case "resume":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
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

	// --- jellyfin_playback_control ---
	if enabled("jellyfin_playback_control", AnnotWriteOp) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_playback_control",
			Title: "Playback Control",
			InputSchema: jf.WithEnums[jf.PlaybackControlInput](map[string][]any{
				"command": {"Pause", "Unpause", "Stop", "NextTrack", "PreviousTrack", "Seek", "Mute", "Unmute", "ToggleMute", "SetVolume", "SendMessage", "GoHome", "GoToSettings", "ChannelUp", "ChannelDown", "DisplayContent"},
			}),
			Description: "Send playback commands to an active client session. Supports Pause, Unpause, Stop, NextTrack, PreviousTrack, Seek, " +
				"Mute, Unmute, ToggleMute, SetVolume, and SendMessage. " +
				"The session_id must come from jellyfin_sessions (list action). For Seek, provide seek_position_ticks (10,000,000 ticks = 1 second). " +
				"To convert: multiply seconds by 10,000,000 to get ticks. The position_ticks field from jellyfin_sessions provides the raw value directly for seek operations. " +
				"For SetVolume, provide volume (0-100). For SendMessage, provide message text. " +
				"Also supports GoHome, GoToSettings, ChannelUp, ChannelDown (client navigation), and DisplayContent (navigate the client to an item; requires item_id). " +
				"The session must be actively playing for transport commands to work.",
			Annotations: AnnotWriteCreate,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PlaybackControlInput) (*mcp.CallToolResult, any, error) {
			sessionID := jf.SanitizeID(args.SessionID)
			if result := controllableSession(ctx, client, args.SessionID); result != nil {
				return result, nil, nil
			}

			switch args.Command {
			case "Pause", "Unpause", "Stop", "NextTrack", "PreviousTrack":
				endpoint := fmt.Sprintf("/Sessions/%s/Playing/%s", sessionID, args.Command)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v. Verify the session_id is valid using jellyfin_sessions.", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Command)), nil, nil

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
				endpoint := fmt.Sprintf("/Sessions/%s/System/%s", sessionID, args.Command)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Command)), nil, nil

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
				endpoint := fmt.Sprintf("/Sessions/%s/System/%s", sessionID, args.Command)
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to send command: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Command '%s' sent to session.", args.Command)), nil, nil

			case "DisplayContent":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for DisplayContent. Use jellyfin_search to find item IDs."), nil, nil
				}
				// Fetch item to get required type and name for the Viewing endpoint
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var item map[string]any
				itemEndpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID))
				itemParams := url.Values{"UserId": {userID}}
				if err := client.Get(ctx, itemEndpoint, itemParams, &item); err != nil {
					return jf.ErrResultWithHint("Use jellyfin_search to find valid item IDs.", "Failed to look up item: %v", err), nil, nil
				}
				endpoint := fmt.Sprintf("/Sessions/%s/Viewing", sessionID)
				params := url.Values{
					"itemType": {jf.GetString(item, "Type")},
					"itemId":   {args.ItemID},
					"itemName": {jf.GetString(item, "Name")},
				}
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to display content: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Client navigated to '%s'.", jf.GetString(item, "Name"))), nil, nil

			default:
				return jf.ErrResult("Invalid command '%s'. Valid commands: Pause, Unpause, Stop, NextTrack, PreviousTrack, Seek, Mute, Unmute, ToggleMute, SetVolume, SendMessage, GoHome, GoToSettings, ChannelUp, ChannelDown, DisplayContent", args.Command), nil, nil
			}
		})
	}

	// --- jellyfin_play ---
	if enabled("jellyfin_play", AnnotWriteOp) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_play",
			Title: "Play Media",
			InputSchema: jf.WithEnums[jf.PlayInput](map[string][]any{
				"play_command": {"PlayNow", "PlayNext", "PlayLast"},
			}),
			Description: "Start playback of items on a client session. Sends a list of item IDs to play on the specified session. " +
				"Use play_command 'PlayNow' (default) to replace the current queue and start playing, 'PlayNext' to insert after the current item, " +
				"or 'PlayLast' to append to the end of the queue. Use start_index to begin from a specific item in the list. " +
				"The session_id must come from jellyfin_sessions (list action). Item IDs come from search, browse, or playlist results.",
			Annotations: AnnotWriteCreate,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PlayInput) (*mcp.CallToolResult, any, error) {
			args.ItemIDs = jf.SplitIDs(args.ItemIDs)
			if len(args.ItemIDs) == 0 {
				return jf.ErrResult("item_ids is required. Provide at least one item ID to play."), nil, nil
			}

			if result := controllableSession(ctx, client, args.SessionID); result != nil {
				return result, nil, nil
			}

			playCmd := args.PlayCommand
			if playCmd == "" {
				playCmd = "PlayNow"
			}

			params := url.Values{
				"PlayCommand": {playCmd},
			}
			for _, itemID := range args.ItemIDs {
				params.Add("ItemIds", itemID)
			}
			if args.StartIndex != nil {
				params.Set("StartIndex", fmt.Sprintf("%d", *args.StartIndex))
			}
			endpoint := fmt.Sprintf("/Sessions/%s/Playing", jf.SanitizeID(args.SessionID))

			if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
				return jf.ErrResult("Failed to start playback: %v. Verify the session_id is valid using jellyfin_sessions.", err), nil, nil
			}
			return jf.TextResult(fmt.Sprintf("Playback started: %d items queued with command '%s'.", len(args.ItemIDs), playCmd)), nil, nil
		})
	}
}

// controllableSession returns an error result when no active session has the
// ID, or when the session's client does not accept remote control, so that a
// command is not sent to a client that silently ignores it.
func controllableSession(ctx context.Context, client jf.Client, sessionID string) *mcp.CallToolResult {
	if sessionID == "" {
		return jf.ErrResult("session_id is required. Use jellyfin_sessions list to find active sessions.")
	}
	var sessions []map[string]any
	if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err)
	}
	for _, s := range sessions {
		if jf.NormalizeID(jf.GetString(s, "Id")) != jf.NormalizeID(sessionID) {
			continue
		}
		if !jf.GetBool(jf.ToMap(s["Capabilities"]), "SupportsMediaControl") {
			return jf.ErrResult("Session '%s' (%s) does not accept remote control, so nothing was sent. Use jellyfin_sessions list and pick a session whose supports_media_control is true.", jf.GetString(s, "DeviceName"), jf.GetString(s, "Client"))
		}
		return nil
	}
	return jf.ErrResult("No active session has ID '%s'. Use jellyfin_sessions list to find active sessions.", sessionID)
}
