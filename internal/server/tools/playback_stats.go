package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

var validPlaybackStatsTypes = []string{
	"Movie", "Series", "Episode", "Audio", "MusicVideo", "Video",
}

var validPlaybackHistoryTypes = []string{
	"Movie", "Episode", "Audio", "MusicVideo", "Video",
}

const defaultPlaybackHistoryTypes = "Movie,Episode,Audio,MusicVideo,Video"

func playbackStatsType(raw, action string) (string, *mcp.CallToolResult) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "Movie", nil
	}
	idx := slices.IndexFunc(validPlaybackStatsTypes, func(valid string) bool {
		return strings.EqualFold(valid, trimmed)
	})
	if idx < 0 {
		return "", jf.ErrResult("type for %s must be one of %s, not %q.", action, strings.Join(validPlaybackStatsTypes, ", "), raw)
	}
	return validPlaybackStatsTypes[idx], nil
}

func playbackHistoryType(raw string) (string, *mcp.CallToolResult) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultPlaybackHistoryTypes, nil
	}
	if strings.EqualFold(trimmed, "Series") {
		return "", jf.ErrResult("type=Series is not supported for playback_history because playback events are recorded on individual episodes; use type=Episode or action=most_played/played_status.")
	}
	idx := slices.IndexFunc(validPlaybackHistoryTypes, func(valid string) bool {
		return strings.EqualFold(valid, trimmed)
	})
	if idx < 0 {
		return "", jf.ErrResult("type for playback_history must be one of %s, not %q.", strings.Join(validPlaybackHistoryTypes, ", "), raw)
	}
	return validPlaybackHistoryTypes[idx], nil
}

func RegisterPlaybackStatsTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {
	if !enabled("jellyfin_playback_stats", AnnotReadOnly) {
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:  "jellyfin_playback_stats",
		Title: "Playback Statistics",
		InputSchema: jf.WithEnums[jf.PlaybackStatsInput](map[string][]any{
			"action": {"playback_history", "played_status", "most_played", "unwatched_by_all"},
		}),
		OutputSchema: jf.SchemaFor[jf.PlaybackStatsOutput](),
		Description:  "Inspect user and household playback statistics: verified watch history (playback_history), per-user watch status for an item (played_status), most-watched items across enabled users (most_played), and items unwatched by every enabled user (unwatched_by_all).",
		Annotations:  AnnotReadOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.PlaybackStatsInput) (*mcp.CallToolResult, *jf.PlaybackStatsOutput, error) {
		switch args.Action {
		case "playback_history":
			return handleStatsPlaybackHistory(ctx, client, args)
		case "played_status":
			return handleStatsPlayedStatus(ctx, client, args)
		case "most_played":
			return handleStatsMostPlayed(ctx, req, client, args)
		case "unwatched_by_all":
			return handleStatsUnwatchedByAll(ctx, req, client, args)
		default:
			return jf.ErrResult("Invalid action '%s'. Valid actions: playback_history, played_status, most_played, unwatched_by_all.", args.Action), nil, nil
		}
	})
}

func handleStatsPlaybackHistory(ctx context.Context, client jf.Client, args jf.PlaybackStatsInput) (*mcp.CallToolResult, *jf.PlaybackStatsOutput, error) {
	if strings.TrimSpace(args.ItemID) != "" {
		return jf.ErrResult("item_id is not supported for playback_history (use action=played_status to check a specific item)."), nil, nil
	}
	includeTypes, errRes := playbackHistoryType(args.Type)
	if errRes != nil {
		return errRes, nil, nil
	}
	targetUser, err := jf.EffectiveUserID(ctx, client, args.UserID)
	if err != nil {
		return jf.ResolveErrResult(err), nil, nil
	}
	parentID := ""
	if strings.TrimSpace(args.ParentID) != "" {
		resolvedParent, err := jf.ResolveLibraryID(ctx, client, args.ParentID)
		if err != nil {
			return jf.ResolveErrResult(err), nil, nil
		}
		parentID = resolvedParent
	}
	maxItems := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
	params := url.Values{
		"UserId":           {targetUser},
		"IncludeItemTypes": {includeTypes},
		"Recursive":        {"true"},
		"Filters":          {"IsPlayed"},
		"SortBy":           {"DatePlayed"},
		"SortOrder":        {"Descending"},
		"Fields":           {"Overview"},
	}
	if parentID != "" {
		params.Set("ParentId", parentID)
	}
	rawItems, total, err := jf.FetchAllPages(ctx, client, "/Items", params, maxItems)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}

	verifiedOnly := args.VerifiedOnly == nil || *args.VerifiedOnly
	playedViaPlayback, activityCapped, verifyErr := jf.PlayedItemIDsWithCap(ctx, client, targetUser)
	if verifyErr != nil && verifiedOnly {
		return jf.ErrResult("Jellyfin API error: %v. Set verified_only=false to list items marked played without verifying them.", verifyErr), nil, nil
	}

	entries := make([]jf.PlaybackHistoryEntry, 0, len(rawItems))
	for _, raw := range rawItems {
		m := jf.ToMap(raw)
		entry := jf.PlaybackHistoryEntry{MediaItem: jf.MediaItemFrom(m)}
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

	shown := len(entries)
	excluded := len(rawItems) - shown
	var notes []string
	if verifiedOnly {
		if total > len(rawItems) {
			if maxItems < jf.MaxLimitCap {
				notes = append(notes, fmt.Sprintf("Checked the %d most recently played items out of %d marked played against the activity log (%d lacked a playback event; set verified_only=false to include them). Increase limit (currently %d, at most %d) to check older items.", len(rawItems), total, excluded, maxItems, jf.MaxLimitCap))
			} else {
				notes = append(notes, fmt.Sprintf("Checked the %d most recently played items out of %d marked played against the activity log (%d lacked a playback event; limit is at its maximum of %d).", len(rawItems), total, excluded, jf.MaxLimitCap))
			}
		} else if excluded > 0 {
			notes = append(notes, fmt.Sprintf("Excluded %d marked-played %s with no activity-log playback event (total_count=%d counts all items marked played; set verified_only=false to include them).", excluded, plural(excluded, "item", "items"), total))
		}
	} else {
		notes = append(notes, moreNote(shown, total, maxItems, "")...)
	}
	if verifyErr != nil {
		notes = append(notes, fmt.Sprintf("Playback could not be verified, so actual_playback is left out: %v", verifyErr))
	} else if activityCapped {
		notes = append(notes, fmt.Sprintf("Activity-log verification checked the %d most recent activity entries, so older plays may appear unverified.", jf.ActivityLogLookback))
	}

	return nil, &jf.PlaybackStatsOutput{
		History:    &entries,
		TotalCount: &total,
		Shown:      &shown,
		Notes:      notes,
	}, nil
}

func handleStatsPlayedStatus(ctx context.Context, client jf.Client, args jf.PlaybackStatsInput) (*mcp.CallToolResult, *jf.PlaybackStatsOutput, error) {
	if strings.TrimSpace(args.UserID) != "" {
		return jf.ErrResult("user_id is not supported for played_status, which already reports all enabled household users (use jellyfin_user_data action=get_user_data with user_id for a single user)."), nil, nil
	}
	if strings.TrimSpace(args.ParentID) != "" {
		return jf.ErrResult("parent_id is not supported for played_status, which inspects a single item_id."), nil, nil
	}
	if args.VerifiedOnly != nil {
		return jf.ErrResult("verified_only is only supported for playback_history."), nil, nil
	}
	itemID := strings.TrimSpace(args.ItemID)
	if itemID == "" {
		return jf.ErrResult("item_id is required for played_status."), nil, nil
	}
	wantType := ""
	if strings.TrimSpace(args.Type) != "" {
		var errRes *mcp.CallToolResult
		wantType, errRes = playbackStatsType(args.Type, args.Action)
		if errRes != nil {
			return errRes, nil, nil
		}
	}

	userID, err := client.GetUserID(ctx)
	if err != nil {
		return jf.ErrResult("Jellyfin error: %v", err), nil, nil
	}

	users, notes, err := fetchHouseholdUsers(ctx, client, userID)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}

	var item map[string]any
	itemEndpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(itemID))
	err = client.Get(ctx, itemEndpoint, url.Values{"UserId": {userID}}, &item)
	directTypeMismatch := err == nil && len(item) > 0 && wantType != "" && jf.GetString(item, "Type") != "" && !strings.EqualFold(jf.GetString(item, "Type"), wantType)
	if (err != nil || len(item) == 0 || directTypeMismatch) && !jf.IsJellyfinID(itemID) {
		var apiErr *jf.APIError
		if err != nil && (ctx.Err() != nil || !errors.As(err, &apiErr) || (apiErr.StatusCode != 400 && apiErr.StatusCode != 404)) {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
		resolvedID, resErr := jf.ResolveItemByTitleTyped(ctx, client, userID, itemID, wantType)
		if resErr != nil {
			return jf.ResolveErrResult(resErr), nil, nil
		}
		itemID = resolvedID
		itemEndpoint = fmt.Sprintf("/Items/%s", jf.SanitizeID(itemID))
		item = nil
		if err = client.Get(ctx, itemEndpoint, url.Values{"UserId": {userID}}, &item); err != nil {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
	} else if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	if actualID := jf.GetString(item, "Id"); actualID != "" {
		itemID = actualID
		itemEndpoint = fmt.Sprintf("/Items/%s", jf.SanitizeID(itemID))
	}

	itemName := jf.GetString(item, "Name")
	itemType := jf.GetString(item, "Type")
	if wantType != "" && itemType != "" && !strings.EqualFold(itemType, wantType) {
		return jf.ErrResult("Item '%s' (%s) has type %s, which does not match type=%s.", itemName, itemID, itemType, wantType), nil, nil
	}

	totalEpisodes := 0
	if itemType == "Series" {
		epParams := url.Values{
			"UserId":           {userID},
			"ParentId":         {itemID},
			"IncludeItemTypes": {"Episode"},
			"Recursive":        {"true"},
			"Limit":            {"0"},
			"IsMissing":        {"false"},
		}
		var epResult map[string]any
		if err := client.Get(ctx, "/Items", epParams, &epResult); err == nil {
			totalEpisodes = jf.GetInt(epResult, "TotalRecordCount")
		}
	}

	userResults := make([]jf.PlayedStatusUser, 0, len(users))
	var unread []string
	for _, u := range users {
		if policy := jf.ToMap(u["Policy"]); policy != nil && jf.GetBool(policy, "IsDisabled") {
			continue
		}
		uid := jf.GetString(u, "Id")
		name := jf.GetString(u, "Name")

		var userItem map[string]any
		if err := client.Get(ctx, itemEndpoint, url.Values{"UserId": {uid}}, &userItem); err != nil {
			unread = append(unread, name)
			continue
		}

		entry := jf.PlayedStatusUser{Name: name}
		if ud := jf.ToMap(userItem["UserData"]); ud != nil {
			entry.Played = jf.GetBool(ud, "Played")
			if pc := jf.GetInt(ud, "PlayCount"); pc > 0 {
				entry.PlayCount = pc
			}
			if lp := jf.GetString(ud, "LastPlayedDate"); lp != "" {
				entry.LastPlayed = jf.LocalDate(lp)
			}
			epTotal := totalEpisodes
			if userTotal := jf.GetInt(userItem, "RecursiveItemCount"); userTotal > 0 {
				epTotal = userTotal
			}
			if itemType == "Series" && epTotal > 0 {
				if rawUnplayed, hasUnplayed := ud["UnplayedItemCount"]; hasUnplayed && rawUnplayed != nil {
					unplayed := jf.GetNum[int](ud, "UnplayedItemCount")
					played := max(0, min(epTotal, epTotal-unplayed))
					entry.EpisodesPlayed = &played
					entry.TotalEpisodes = epTotal
				} else if entry.Played {
					played := epTotal
					entry.EpisodesPlayed = &played
					entry.TotalEpisodes = epTotal
				}
			}
		}
		userResults = append(userResults, entry)
	}
	notes = append(notes, unreadUsersNote(unread)...)

	return nil, &jf.PlaybackStatsOutput{
		ItemSummary: &jf.PlayedStatusItem{
			ID:            itemID,
			Name:          itemName,
			Type:          itemType,
			TotalEpisodes: totalEpisodes,
		},
		Users: &userResults,
		Notes: notes,
	}, nil
}

func handleStatsMostPlayed(ctx context.Context, req *mcp.CallToolRequest, client jf.Client, args jf.PlaybackStatsInput) (*mcp.CallToolResult, *jf.PlaybackStatsOutput, error) {
	if strings.TrimSpace(args.UserID) != "" {
		return jf.ErrResult("user_id is not supported for most_played, which aggregates across all enabled household users (use jellyfin_browse with user_id, is_played=true, and sort_by=PlayCount for one user)."), nil, nil
	}
	if strings.TrimSpace(args.ItemID) != "" {
		return jf.ErrResult("item_id is not supported for most_played (use action=played_status to check a specific item)."), nil, nil
	}
	if args.VerifiedOnly != nil {
		return jf.ErrResult("verified_only is only supported for playback_history."), nil, nil
	}
	itemType, errRes := playbackStatsType(args.Type, args.Action)
	if errRes != nil {
		return errRes, nil, nil
	}

	userID, err := client.GetUserID(ctx)
	if err != nil {
		return jf.ErrResult("Jellyfin error: %v", err), nil, nil
	}

	parentID := ""
	if args.ParentID != "" {
		resolvedParent, err := jf.ResolveLibraryID(ctx, client, args.ParentID)
		if err != nil {
			return jf.ResolveErrResult(err), nil, nil
		}
		parentID = resolvedParent
	}

	limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
	enabledIDs, notes, err := fetchEnabledHouseholdUserIDs(ctx, client, userID)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	if len(enabledIDs) == 0 {
		empty := []jf.PopularItemEntry{}
		zero := 0
		return nil, &jf.PlaybackStatsOutput{
			MostPlayed: &empty,
			TotalCount: &zero,
			Shown:      &zero,
			Notes:      notes,
		}, nil
	}

	type popularAccum struct {
		id         string
		name       string
		typ        string
		year       int
		seriesName string
		users      map[string]bool
		totalPlays int
		rawLast    string
	}
	byID := make(map[string]*popularAccum)

	isSeries := strings.EqualFold(itemType, "Series")
	fetchType := itemType
	if isSeries {
		fetchType = "Episode"
	}

	for idx, uid := range enabledIDs {
		jf.ReportProgress(ctx, req, float64(idx), float64(len(enabledIDs)), fmt.Sprintf("Scanning played %s items (%d/%d users)...", fetchType, idx+1, len(enabledIDs)))
		params := url.Values{
			"UserId":           {uid},
			"IncludeItemTypes": {fetchType},
			"IsPlayed":         {"true"},
			"Recursive":        {"true"},
			"SortBy":           {"SortName"},
		}
		if parentID != "" {
			params.Set("ParentId", parentID)
		}
		normUID := jf.NormalizeID(uid)
		seenInUser := make(map[string]bool)
		if _, err := jf.EachPage(ctx, client, "/Items", params, func(_ int, items []map[string]any) {
			for _, m := range items {
				if rawID := jf.NormalizeID(jf.GetString(m, "Id")); rawID != "" {
					if seenInUser[rawID] {
						continue
					}
					seenInUser[rawID] = true
				}
				var entryID, entryName, entryType, seriesName string
				var year int
				if isSeries {
					entryID = jf.GetString(m, "SeriesId")
					entryName = jf.GetString(m, "SeriesName")
					entryType = "Series"
				} else {
					entryID = jf.GetString(m, "Id")
					entryName = jf.GetString(m, "Name")
					entryType = jf.GetString(m, "Type")
					if entryType == "" {
						entryType = itemType
					}
					year = jf.GetInt(m, "ProductionYear")
					seriesName = jf.GetString(m, "SeriesName")
				}
				if entryID == "" {
					continue
				}
				key := jf.NormalizeID(entryID)
				acc, ok := byID[key]
				if !ok {
					acc = &popularAccum{
						id:         entryID,
						name:       entryName,
						typ:        entryType,
						year:       year,
						seriesName: seriesName,
						users:      make(map[string]bool),
					}
					byID[key] = acc
				} else {
					if acc.name == "" && entryName != "" {
						acc.name = entryName
					}
					if acc.year == 0 && year > 0 {
						acc.year = year
					}
					if acc.seriesName == "" && seriesName != "" {
						acc.seriesName = seriesName
					}
				}
				acc.users[normUID] = true
				plays := 1
				if ud := jf.ToMap(m["UserData"]); ud != nil {
					if pc := jf.GetInt(ud, "PlayCount"); pc > 1 {
						plays = pc
					}
					if lp := jf.GetString(ud, "LastPlayedDate"); lp != "" && (acc.rawLast == "" || jf.NewerFirst(lp, acc.rawLast)) {
						acc.rawLast = lp
					}
				}
				acc.totalPlays += plays
			}
		}); err != nil {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
	}

	sorted := make([]*popularAccum, 0, len(byID))
	for _, acc := range byID {
		sorted = append(sorted, acc)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if len(a.users) != len(b.users) {
			return len(a.users) > len(b.users)
		}
		if a.totalPlays != b.totalPlays {
			return a.totalPlays > b.totalPlays
		}
		if a.rawLast != b.rawLast {
			if a.rawLast == "" {
				return false
			}
			if b.rawLast == "" {
				return true
			}
			if jf.NewerFirst(a.rawLast, b.rawLast) {
				return true
			}
			if jf.NewerFirst(b.rawLast, a.rawLast) {
				return false
			}
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.id < b.id
	})

	total := len(sorted)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	mostPlayed := make([]jf.PopularItemEntry, 0, len(sorted))
	for _, acc := range sorted {
		entry := jf.PopularItemEntry{
			ID:          acc.id,
			Name:        acc.name,
			Type:        acc.typ,
			Year:        acc.year,
			SeriesName:  acc.seriesName,
			UsersPlayed: len(acc.users),
			TotalPlays:  acc.totalPlays,
		}
		if acc.rawLast != "" {
			entry.LastPlayed = jf.LocalDate(acc.rawLast)
		}
		mostPlayed = append(mostPlayed, entry)
	}

	shown := len(mostPlayed)
	notes = append(notes, moreNote(shown, total, limit, "narrow with parent_id or type")...)
	if isSeries {
		notes = append(notes, "For Series, users_played counts enabled users who finished at least one episode, and total_plays sums episode play counts across enabled users.")
	}

	return nil, &jf.PlaybackStatsOutput{
		MostPlayed: &mostPlayed,
		TotalCount: &total,
		Shown:      &shown,
		Notes:      notes,
	}, nil
}

func handleStatsUnwatchedByAll(ctx context.Context, req *mcp.CallToolRequest, client jf.Client, args jf.PlaybackStatsInput) (*mcp.CallToolResult, *jf.PlaybackStatsOutput, error) {
	if strings.TrimSpace(args.UserID) != "" {
		return jf.ErrResult("user_id is not supported for unwatched_by_all, which checks all enabled household users (use jellyfin_analytics action=never_played with user_id for one user)."), nil, nil
	}
	if strings.TrimSpace(args.ItemID) != "" {
		return jf.ErrResult("item_id is not supported for unwatched_by_all (use action=played_status to check a specific item)."), nil, nil
	}
	if args.VerifiedOnly != nil {
		return jf.ErrResult("verified_only is only supported for playback_history."), nil, nil
	}
	itemType, errRes := playbackStatsType(args.Type, args.Action)
	if errRes != nil {
		return errRes, nil, nil
	}

	userID, err := client.GetUserID(ctx)
	if err != nil {
		return jf.ErrResult("Jellyfin error: %v", err), nil, nil
	}

	parentID := ""
	if args.ParentID != "" {
		resolvedParent, err := jf.ResolveLibraryID(ctx, client, args.ParentID)
		if err != nil {
			return jf.ResolveErrResult(err), nil, nil
		}
		parentID = resolvedParent
	}

	limit := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
	enabledIDs, notes, err := fetchEnabledHouseholdUserIDs(ctx, client, userID)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	if len(enabledIDs) == 0 {
		empty := []jf.MediaItem{}
		zero := 0
		return nil, &jf.PlaybackStatsOutput{
			Unwatched:  &empty,
			TotalCount: &zero,
			Shown:      &zero,
			Notes:      notes,
		}, nil
	}

	playedByAny := make(map[string]bool)
	isSeries := strings.EqualFold(itemType, "Series")

	for idx, uid := range enabledIDs {
		jf.ReportProgress(ctx, req, float64(idx), float64(len(enabledIDs)+1), fmt.Sprintf("Checking played items (%d/%d users)...", idx+1, len(enabledIDs)))
		if isSeries {
			epParams := url.Values{
				"UserId":           {uid},
				"IncludeItemTypes": {"Episode"},
				"IsPlayed":         {"true"},
				"Recursive":        {"true"},
				"SortBy":           {"SortName"},
			}
			if parentID != "" {
				epParams.Set("ParentId", parentID)
			}
			if _, err := jf.EachPage(ctx, client, "/Items", epParams, func(_ int, items []map[string]any) {
				for _, m := range items {
					if sid := jf.GetString(m, "SeriesId"); sid != "" {
						playedByAny[jf.NormalizeID(sid)] = true
					}
				}
			}); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
		}

		params := url.Values{
			"UserId":           {uid},
			"IncludeItemTypes": {itemType},
			"IsPlayed":         {"true"},
			"Recursive":        {"true"},
			"SortBy":           {"SortName"},
		}
		if parentID != "" {
			params.Set("ParentId", parentID)
		}
		if _, err := jf.EachPage(ctx, client, "/Items", params, func(_ int, items []map[string]any) {
			for _, m := range items {
				if id := jf.GetString(m, "Id"); id != "" {
					playedByAny[jf.NormalizeID(id)] = true
				}
			}
		}); err != nil {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
	}

	jf.ReportProgress(ctx, req, float64(len(enabledIDs)), float64(len(enabledIDs)+1), fmt.Sprintf("Scanning %s catalog...", itemType))
	catalogParams := url.Values{
		"UserId":           {userID},
		"IncludeItemTypes": {itemType},
		"IsMissing":        {"false"},
		"Recursive":        {"true"},
		"SortBy":           {"DateCreated"},
		"SortOrder":        {"Descending"},
		"Fields":           {"Overview,DateCreated"},
	}
	if parentID != "" {
		catalogParams.Set("ParentId", parentID)
	}

	unwatched := make([]jf.MediaItem, 0, min(limit, 50))
	seen := make(map[string]bool)
	total := 0
	if _, err := jf.EachPage(ctx, client, "/Items", catalogParams, func(_ int, items []map[string]any) {
		for _, m := range items {
			id := jf.GetString(m, "Id")
			norm := jf.NormalizeID(id)
			if norm == "" || seen[norm] || playedByAny[norm] {
				continue
			}
			seen[norm] = true
			total++
			if len(unwatched) < limit {
				mi := jf.MediaItemFrom(m)
				mi.Played = false
				mi.Progress = ""
				mi.Favorite = false
				if dc := jf.GetString(m, "DateCreated"); dc != "" {
					mi.DateAdded = jf.LocalDate(dc)
				}
				unwatched = append(unwatched, mi)
			}
		}
	}); err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}

	shown := len(unwatched)
	notes = append(notes, moreNote(shown, total, limit, "narrow with parent_id or type")...)

	return nil, &jf.PlaybackStatsOutput{
		Unwatched:  &unwatched,
		TotalCount: &total,
		Shown:      &shown,
		Notes:      notes,
	}, nil
}

func fetchHouseholdUsers(ctx context.Context, client jf.Client, fallbackUserID string) ([]map[string]any, []string, error) {
	var users []map[string]any
	if err := client.Get(ctx, "/Users", nil, &users); err != nil {
		var apiErr *jf.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 403 {
			name := "current user"
			var self map[string]any
			if err := client.Get(ctx, "/Users/"+jf.SanitizeID(fallbackUserID), nil, &self); err == nil {
				if n := jf.GetString(self, "Name"); n != "" {
					name = n
				}
			}
			return []map[string]any{{"Id": fallbackUserID, "Name": name}},
				[]string{"/Users returned 403 Forbidden, so results reflect only the current user."},
				nil
		}
		return nil, nil, err
	}
	return users, nil, nil
}

func fetchEnabledHouseholdUserIDs(ctx context.Context, client jf.Client, fallbackUserID string) ([]string, []string, error) {
	users, notes, err := fetchHouseholdUsers(ctx, client, fallbackUserID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(users))
	seen := make(map[string]bool, len(users))
	for _, u := range users {
		if policy := jf.ToMap(u["Policy"]); policy != nil && jf.GetBool(policy, "IsDisabled") {
			continue
		}
		uid := jf.GetString(u, "Id")
		norm := jf.NormalizeID(uid)
		if norm != "" && !seen[norm] {
			seen[norm] = true
			ids = append(ids, uid)
		}
	}
	return ids, notes, nil
}
