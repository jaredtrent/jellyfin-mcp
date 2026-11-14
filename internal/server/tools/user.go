package tools

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterUserTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_user_data ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_user_data", AnnotWriteOp, AnnotReadOnly); ok {
		roActions := []any{"get_user_data"}
		schema := jf.WithEnums[jf.UserDataInput](map[string][]any{
			"action": {"favorite", "unfavorite", "like", "dislike", "clear_rating", "mark_played", "mark_unplayed", "rate", "get_user_data", "set_user_data"},
		})
		desc := "Read or update per-user item state: favorites, like/dislike or 0-10 ratings, played status, play count, and playback position."
		if ro {
			schema = jf.WithEnumsFiltered[jf.UserDataInput](map[string][]any{"action": roActions}, []string{"action", "item_id", "user_id"})
			desc = "Read per-user item state: played status, play count, rating, favorite flag, and resume position."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_user_data",
			Title:       "User Interactions",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.UserDataInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			itemID := jf.SanitizeID(strings.TrimSpace(args.ItemID))

			type simpleAction struct {
				pathFmt    string
				isDelete   bool
				params     url.Values
				successMsg string
				errorVerb  string
			}
			simpleActions := map[string]simpleAction{
				"favorite":      {"/UserFavoriteItems/%s", false, nil, "Item added to favorites.", "add favorite"},
				"unfavorite":    {"/UserFavoriteItems/%s", true, nil, "Item removed from favorites.", "remove favorite"},
				"like":          {"/UserItems/%s/Rating", false, url.Values{"likes": {"true"}}, "Item rated: liked.", "set rating"},
				"dislike":       {"/UserItems/%s/Rating", false, url.Values{"likes": {"false"}}, "Item rated: disliked.", "set rating"},
				"clear_rating":  {"/UserItems/%s/Rating", true, nil, "Rating cleared.", "clear rating"},
				"mark_played":   {"/UserPlayedItems/%s", false, nil, "Item marked as played.", "mark as played"},
				"mark_unplayed": {"/UserPlayedItems/%s", true, nil, "Item marked as unplayed.", "mark as unplayed"},
			}
			if act, ok := simpleActions[args.Action]; ok {
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for get_user_data; write actions update the configured user's own item state (omit user_id)."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for %s.", args.Action), nil, nil
				}
				endpoint := fmt.Sprintf(act.pathFmt, itemID)
				params := make(url.Values, len(act.params)+1)
				maps.Copy(params, act.params)
				params.Set("UserId", userID)
				var err error
				if act.isDelete {
					err = client.Del(ctx, endpoint, params)
				} else {
					err = client.PostNoContent(ctx, endpoint, params, nil)
				}
				if err != nil {
					return jf.ErrResult("Failed to %s: %v", act.errorVerb, err), nil, nil
				}
				return jf.TextResult(act.successMsg), nil, nil
			}

			switch args.Action {
			case "rate":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for get_user_data; write actions update the configured user's own item state (omit user_id)."), nil, nil
				}
				if args.Rating == nil {
					return jf.ErrResult("rating is required (0.0-10.0)."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for rate."), nil, nil
				}
				body := map[string]any{"Rating": *args.Rating}
				endpoint := fmt.Sprintf("/UserItems/%s/UserData", itemID)
				params := url.Values{"UserId": {userID}}
				if err := client.PostNoContent(ctx, endpoint, params, body); err != nil {
					return jf.ErrResult("Failed to set rating: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Rating set to %.1f.", *args.Rating)), nil, nil

			case "get_user_data":
				if itemID == "" {
					return jf.ErrResult("item_id is required for get_user_data."), nil, nil
				}
				targetUser := userID
				if strings.TrimSpace(args.UserID) != "" {
					resolvedUser, err := jf.EffectiveUserID(ctx, client, args.UserID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					targetUser = resolvedUser
				}
				endpoint := fmt.Sprintf("/UserItems/%s/UserData", itemID)
				params := url.Values{"UserId": {targetUser}}
				var data map[string]any
				if err := client.Get(ctx, endpoint, params, &data); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				result := map[string]any{
					"played":     jf.GetBool(data, "Played"),
					"play_count": jf.GetInt(data, "PlayCount"),
					"favorite":   jf.GetBool(data, "IsFavorite"),
				}
				if pos := jf.GetInt64(data, "PlaybackPositionTicks"); pos > 0 {
					result["position_ticks"] = pos
					result["position_seconds"] = pos / jf.TicksPerSecond
				}
				if rating := jf.GetFloat(data, "Rating"); rating > 0 {
					result["rating"] = rating
				}
				if lp := jf.GetString(data, "LastPlayedDate"); lp != "" {
					result["last_played"] = jf.LocalDate(lp)
				}
				if pct := jf.GetFloat(data, "PlayedPercentage"); pct > 0 {
					result["played_percentage"] = pct
				}
				return jf.TextResult(fmt.Sprintf("User data:\n\n%s", jf.FormatJSON(result))), nil, nil

			case "set_user_data":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for get_user_data; write actions update the configured user's own item state (omit user_id)."), nil, nil
				}
				body := make(map[string]any)
				var fields []string
				if args.Played != nil {
					body["Played"] = *args.Played
					fields = append(fields, fmt.Sprintf("played=%v", *args.Played))
				}
				if args.PlayCount != nil {
					body["PlayCount"] = *args.PlayCount
					fields = append(fields, fmt.Sprintf("play_count=%d", *args.PlayCount))
				}
				if args.PositionTicks != nil {
					body["PlaybackPositionTicks"] = *args.PositionTicks
					fields = append(fields, fmt.Sprintf("position=%s", formatPositionTicks(*args.PositionTicks)))
				}
				if args.Rating != nil {
					body["Rating"] = *args.Rating
					fields = append(fields, fmt.Sprintf("rating=%.1f", *args.Rating))
				}
				if len(body) == 0 {
					return jf.ErrResult("Provide at least one field to update: played, play_count, position_ticks, rating."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for set_user_data."), nil, nil
				}
				var warning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					item, errRes := requireItemName(ctx, client, args.ItemID, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					uName := userName(ctx, client, userID)
					warning = fmt.Sprintf("Update play state of '%s' for user '%s' (%s)? Other play state fields stay unchanged.", item, uName, strings.Join(fields, ", "))
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/UserItems/%s/UserData", itemID)
				params := url.Values{"UserId": {userID}}
				if err := client.PostNoContent(ctx, endpoint, params, body); err != nil {
					return jf.ErrResult("Failed to set user data: %v", err), nil, nil
				}
				return jf.TextResult("User data updated."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: favorite, unfavorite, like, dislike, clear_rating, mark_played, mark_unplayed, rate, get_user_data, set_user_data", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_playlists ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_playlists", AnnotDestructive, AnnotReadOnly); ok {
		locks := newPlaylistLocks()
		roActions := []any{"list", "get"}
		schema := jf.WithEnums[jf.PlaylistsInput](map[string][]any{
			"action": {"list", "create", "get", "add_items", "remove_items", "move_item", "deduplicate", "delete"},
		})
		desc := "List, read, create, edit, reorder, deduplicate, or delete playlists as the configured user. " +
			"move_item and deduplicate rewrite trailing entries via Jellyfin's remove-and-append API (use dry_run=true to preview)."
		if ro {
			schema = jf.WithEnumsFiltered[jf.PlaylistsInput](map[string][]any{"action": roActions}, []string{"action", "playlist_id", "user_id"})
			desc = "List playlists or read a playlist's items."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_playlists",
			Title:       "Playlists",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PlaylistsInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			switch args.Action {
			case "list":
				targetUser := userID
				if strings.TrimSpace(args.UserID) != "" {
					resolvedUser, err := jf.EffectiveUserID(ctx, client, args.UserID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					targetUser = resolvedUser
				}
				params := url.Values{
					"UserId":           {targetUser},
					"IncludeItemTypes": {"Playlist"},
					"Recursive":        {"true"},
					"Fields":           {"ChildCount"},
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Items", params, jf.DefaultMaxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					items = append(items, map[string]any{
						"id":         jf.GetString(m, "Id"),
						"name":       jf.GetString(m, "Name"),
						"item_count": jf.GetInt(m, "ChildCount"),
					})
				}
				found := fmt.Sprintf("Found %d %s:", len(items), plural(len(items), "playlist", "playlists"))
				if total > len(items) {
					found = fmt.Sprintf("Found %d playlists; showing the first %d:", total, len(items))
				}
				return jf.TextResult(fmt.Sprintf("%s\n\n%s", found, jf.FormatJSON(items))), nil, nil

			case "create":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for list and get; playlist write actions operate as the configured user (omit user_id)."), nil, nil
				}
				if args.Name == "" {
					return jf.ErrResult("name is required to create a playlist."), nil, nil
				}
				body := map[string]any{
					"Name":   args.Name,
					"UserId": userID,
				}
				if ids := jf.SplitIDs(args.ItemIDs); len(ids) > 0 {
					body["Ids"] = ids
				}
				if args.MediaType != "" {
					body["MediaType"] = args.MediaType
				}
				var result map[string]any
				if err := client.Post(ctx, "/Playlists", nil, body, &result); err != nil {
					return jf.ErrResult("Failed to create playlist: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Playlist created: %s (ID: %s)", args.Name, jf.GetString(result, "Id"))), nil, nil

			case "delete":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for list and get; playlist write actions operate as the configured user (omit user_id)."), nil, nil
				}
				if args.PlaylistID == "" {
					return jf.ErrResult("playlist_id is required for 'delete' action."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				resolvedID, err := jf.ResolvePlaylistID(ctx, client, userID, args.PlaylistID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				args.PlaylistID = resolvedID
				name := itemName(ctx, client, args.PlaylistID, userID)
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete playlist '%s'? Its entries are dropped; the media stays in the library.", name)); result != nil {
					return result, nil, nil
				}
				if err := client.Del(ctx, "/Items/"+jf.SanitizeID(args.PlaylistID), nil); err != nil {
					return jf.ErrResult("Failed to delete playlist: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Playlist '%s' deleted.", name)), nil, nil

			case "get":
				if args.PlaylistID == "" {
					return jf.ErrResult("playlist_id is required for 'get' action."), nil, nil
				}
				targetUser := userID
				if strings.TrimSpace(args.UserID) != "" {
					resolvedUser, err := jf.EffectiveUserID(ctx, client, args.UserID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					targetUser = resolvedUser
				}
				resolvedID, err := jf.ResolvePlaylistID(ctx, client, targetUser, args.PlaylistID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				args.PlaylistID = resolvedID
				params := url.Values{
					"UserId": {targetUser},
				}
				endpoint := fmt.Sprintf("/Playlists/%s/Items", jf.SanitizeID(args.PlaylistID))
				rawItems, total, err := jf.FetchAllPages(ctx, client, endpoint, params, 1000)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				header := fmt.Sprintf("Playlist items (%d)", len(items))
				if total > len(items) {
					header = fmt.Sprintf("Playlist items (showing the first %d of %d)", len(items), total)
				}
				return jf.TextResult(fmt.Sprintf("%s:\n\n%s", header, jf.FormatJSON(items))), nil, nil

			case "add_items":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for list and get; playlist write actions operate as the configured user (omit user_id)."), nil, nil
				}
				args.ItemIDs = jf.SplitIDs(args.ItemIDs)
				if args.PlaylistID == "" || len(args.ItemIDs) == 0 {
					return jf.ErrResult("playlist_id and item_ids are required for 'add_items' action."), nil, nil
				}
				resolvedID, err := jf.ResolvePlaylistID(ctx, client, userID, args.PlaylistID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				args.PlaylistID = resolvedID
				unlock, err := locks.lock(ctx, args.PlaylistID)
				if err != nil {
					return jf.ErrResult("Stopped while waiting for another change to this playlist to finish: %v", err), nil, nil
				}
				defer unlock()
				// The server can add more or fewer entries than the IDs sent, so
				// the result reports the change in the playlist's entry count.
				before, err := jf.CountPlaylistEntries(ctx, client, args.PlaylistID, userID)
				if err != nil {
					return jf.ErrResult("Failed to read playlist: %v", err), nil, nil
				}
				sent, err := jf.AppendPlaylistItems(ctx, client, args.PlaylistID, userID, args.ItemIDs)
				if err != nil {
					return addItemsFailure(ctx, client, args, userID, before, sent, err), nil, nil
				}
				after, err := jf.CountPlaylistEntries(ctx, client, args.PlaylistID, userID)
				if err != nil {
					return jf.TextResult(fmt.Sprintf("Sent %d items to the playlist, but it could not be read back to count the new entries: %v", len(args.ItemIDs), err)), nil, nil
				}
				added := after - before
				msg := fmt.Sprintf("Added %d %s to the playlist, which now has %d.", added, plural(added, "entry", "entries"), after)
				if added != len(args.ItemIDs) {
					msg += fmt.Sprintf(" %d item %s sent: Jellyfin adds an album, series, or other folder as the items inside it, skips IDs it cannot find or the user cannot see, and before Jellyfin 12 skips items already in the playlist.", len(args.ItemIDs), plural(len(args.ItemIDs), "ID was", "IDs were"))
				}
				return jf.TextResult(msg), nil, nil

			case "remove_items", "move_item", "deduplicate":
				if strings.TrimSpace(args.UserID) != "" {
					return jf.ErrResult("user_id is only supported for list and get; playlist write actions operate as the configured user (omit user_id)."), nil, nil
				}
				return handlePlaylistEdit(ctx, req, client, locks, userID, args)

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, create, get, add_items, remove_items, move_item, deduplicate, delete", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_collections ---
	if enabled("jellyfin_collections", AnnotDestructive) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_collections",
			Title: "Collections",
			InputSchema: jf.WithEnums[jf.CollectionsInput](map[string][]any{
				"action": {"create", "add_items", "remove_items"},
			}),
			Description: "Create BoxSet collections or add and remove collection items.",
			Annotations: AnnotDestructive,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.CollectionsInput) (*mcp.CallToolResult, any, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			switch args.Action {
			case "create":
				if args.Name == "" {
					return jf.ErrResult("name is required to create a collection."), nil, nil
				}
				params := url.Values{"name": {args.Name}}
				if len(args.ItemIDs) > 0 {
					params.Set("ids", jf.JoinIDs(args.ItemIDs))
				}
				if args.ParentID != "" {
					parentID, err := jf.ResolveLibraryID(ctx, client, args.ParentID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					params.Set("ParentId", parentID)
				}
				var result map[string]any
				if err := client.Post(ctx, "/Collections", params, nil, &result); err != nil {
					return jf.ErrResult("Failed to create collection: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Collection '%s' created (ID: %s).", args.Name, jf.GetString(result, "Id"))), nil, nil

			case "add_items":
				if args.CollectionID == "" || len(args.ItemIDs) == 0 {
					return jf.ErrResult("collection_id and item_ids are required for 'add_items' action."), nil, nil
				}
				colID, err := jf.ResolveCollectionID(ctx, client, userID, args.CollectionID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				args.CollectionID = colID
				ids := jf.SplitIDs(args.ItemIDs)
				params := url.Values{"ids": {jf.JoinIDs(ids)}}
				endpoint := fmt.Sprintf("/Collections/%s/Items", jf.SanitizeID(args.CollectionID))
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to add items to collection: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Added %d %s to collection '%s'.", len(ids), plural(len(ids), "item", "items"), itemName(ctx, client, args.CollectionID, userID))), nil, nil

			case "remove_items":
				if args.CollectionID == "" || len(args.ItemIDs) == 0 {
					return jf.ErrResult("collection_id and item_ids are required for 'remove_items' action."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				colID, err := jf.ResolveCollectionID(ctx, client, userID, args.CollectionID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				args.CollectionID = colID
				ids := jf.SplitIDs(args.ItemIDs)
				collection := itemName(ctx, client, args.CollectionID, userID)
				var warning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					names, errRes := requireItemNames(ctx, client, ids, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					warning = fmt.Sprintf("Remove %s from collection '%s'? The %s in the library.", nameList(ids, names), collection, plural(len(ids), "item stays", "items stay"))
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				params := url.Values{"ids": {jf.JoinIDs(ids)}}
				endpoint := fmt.Sprintf("/Collections/%s/Items", jf.SanitizeID(args.CollectionID))
				if err := client.Del(ctx, endpoint, params); err != nil {
					return jf.ErrResult("Failed to remove items from collection: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Removed %d %s from collection '%s'.", len(ids), plural(len(ids), "item", "items"), collection)), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: create, add_items, remove_items", args.Action), nil, nil
			}
		})
	}
}

func formatPositionTicks(ticks int64) string {
	if ticks <= 0 {
		return "00:00:00"
	}
	totalSeconds := ticks / jf.TicksPerSecond
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
