package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterDiscoveryTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_libraries ---
	if enabled("jellyfin_libraries", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_libraries",
			Title: "List Libraries",
			Description: "List all media libraries configured on the Jellyfin server, including Movies, TV Shows, Music, and other collection types. " +
				"Use this tool first to discover available libraries and their IDs before browsing content with jellyfin_browse.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, _ jf.NoInput) (*mcp.CallToolResult, *jf.LibraryListOutput, error) {
			var libs []map[string]any
			if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
				return jf.ErrResultWithHint("Check server with jellyfin_system_info action='ping'.", "Jellyfin API error: %v", err), nil, nil
			}
			items := jf.ExtractLibraries(libs)
			output := &jf.LibraryListOutput{Count: len(items), Libraries: jf.ToLibraryInfos(items)}
			return jf.TextResult(fmt.Sprintf("Found %d libraries:\n\n%s", len(items), jf.FormatJSON(items))), output, nil
		})
	}

	// --- jellyfin_search ---
	if enabled("jellyfin_search", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_search",
			Title: "Search Media",
			Description: "Search for media across the Jellyfin server by keyword. " +
				"Use this when the user provides a specific name or title to look up. " +
				"Returns compact results (name, type, year, truncated overview, rating, runtime, play status) — use jellyfin_get_item for full metadata. " +
				"NOTE: This tool searches by keyword only. To filter by genre, year, rating, or other attributes, use jellyfin_browse — e.g. jellyfin_browse genre=\"Horror\" rather than searching for \"horror\".",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SearchInput) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			limit := jf.ClampInt(args.Limit, 30, jf.MaxLimitCap)
			params := url.Values{
				"searchTerm": {args.Query},
				"Limit":      {fmt.Sprintf("%d", limit)},
				"Recursive":  {"true"},
				"Fields":     {"Overview,ProductionYear,CommunityRating,OfficialRating,RunTimeTicks,UserData"},
			}
			if args.Type != "" {
				params.Set("IncludeItemTypes", args.Type)
			}

			var result map[string]any
			endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
			if err := client.Get(ctx, endpoint, params, &result); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			rawItems := jf.ToSlice(result["Items"])
			items := jf.MapExtract(rawItems, jf.ExtractMediaItem)
			total := jf.GetInt(jf.ToMap(result), "TotalRecordCount")
			msg := fmt.Sprintf("Found %d results (showing %d):\n\n%s", total, len(items), jf.FormatJSON(items))
			if len(items) < total {
				msg += fmt.Sprintf("\n\nMore results available. Refine your query or use jellyfin_browse with start_index=%d to see additional items.", len(items))
			}
			output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: jf.ToMediaItems(items)}
			return jf.TextResult(msg), output, nil
		})
	}

	// --- jellyfin_browse ---
	if enabled("jellyfin_browse", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_browse",
			Title: "Browse & Filter",
			Description: "Browse and filter media items with rich filtering options including genre, year, studio, person, played status, favorite status, and sort orders. " +
				"Use this for attribute-based filtering (genre, year, rating). For looking up a specific title by name, use jellyfin_search instead. " +
				"Returns compact results (name, type, year, truncated overview, rating, runtime, play status). " +
				"Genres, studios, cast, and provider IDs are NOT included — use jellyfin_get_item for full metadata.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.BrowseInput) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			limit := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
			params := url.Values{
				"Limit":     {fmt.Sprintf("%d", limit)},
				"Recursive": {"true"},
				"Fields":    {"Overview,ProductionYear,CommunityRating,OfficialRating,RunTimeTicks,UserData"},
			}
			if args.ParentID != "" {
				params.Set("ParentId", args.ParentID)
			}
			if args.Type != "" {
				params.Set("IncludeItemTypes", args.Type)
			}
			if args.Genre != "" {
				params.Set("Genres", args.Genre)
			}
			if args.Year != nil {
				params.Set("Years", fmt.Sprintf("%d", *args.Year))
			}
			if args.Studio != "" {
				params.Set("Studios", args.Studio)
			}
			if args.Person != "" {
				params.Set("Person", args.Person)
			}
			if args.Tags != "" {
				params.Set("Tags", args.Tags)
			}
			if args.SortBy != "" {
				params.Set("SortBy", args.SortBy)
			} else {
				params.Set("SortBy", "SortName")
			}
			if args.SortOrder != "" {
				params.Set("SortOrder", args.SortOrder)
			}
			if args.IsFavorite != nil {
				params.Set("IsFavorite", fmt.Sprintf("%v", *args.IsFavorite))
			}
			if args.IsPlayed != nil {
				params.Set("IsPlayed", fmt.Sprintf("%v", *args.IsPlayed))
			}
			if args.MinRating != nil {
				params.Set("MinCommunityRating", fmt.Sprintf("%.1f", *args.MinRating))
			}
			if args.OfficialRating != "" {
				params.Set("OfficialRatings", args.OfficialRating)
			}
			if args.HasSubtitles != nil {
				params.Set("HasSubtitles", fmt.Sprintf("%v", *args.HasSubtitles))
			}
			if args.MinDateCreated != "" {
				params.Set("MinDateCreated", args.MinDateCreated)
			}
			if args.MaxDateCreated != "" {
				params.Set("MaxDateCreated", args.MaxDateCreated)
			}
			if args.MinPremiereDate != "" {
				params.Set("MinPremiereDate", args.MinPremiereDate)
			}
			if args.MaxPremiereDate != "" {
				params.Set("MaxPremiereDate", args.MaxPremiereDate)
			}
			if args.StartIndex > 0 {
				params.Set("StartIndex", fmt.Sprintf("%d", args.StartIndex))
			}

			var result map[string]any
			endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
			if err := client.Get(ctx, endpoint, params, &result); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			rawItems := jf.ToSlice(result["Items"])
			items := jf.MapExtract(rawItems, jf.ExtractMediaItem)
			total := jf.GetInt(jf.ToMap(result), "TotalRecordCount")
			msg := fmt.Sprintf("Showing %d of %d items:\n\n%s", len(items), total, jf.FormatJSON(items))
			if len(items) < total {
				nextIndex := args.StartIndex + len(items)
				msg += fmt.Sprintf("\n\nMore results available. Use start_index=%d to see the next page.", nextIndex)
			}
			output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: jf.ToMediaItems(items)}
			return jf.TextResult(msg), output, nil
		})
	}

	// --- jellyfin_get_item ---
	if enabled("jellyfin_get_item", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_get_item",
			Title: "Item Details",
			Description: "Get comprehensive details about a specific media item by its ID. Returns full metadata including overview, genres, studios, cast/crew, " +
				"community and critic ratings, runtime, provider IDs (IMDb, TMDB, TVDB), user data (played status, favorite, rating), " +
				"and media source info (codecs, resolution, bitrate, bit depth, HDR format, all audio tracks, all subtitle tracks with languages).",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.GetItemInput) (*mcp.CallToolResult, *jf.DetailedItemOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			var result map[string]any
			endpoint := fmt.Sprintf("/Users/%s/Items/%s", jf.SanitizeID(userID), jf.SanitizeID(args.ID))
			if err := client.Get(ctx, endpoint, nil, &result); err != nil {
				return jf.ErrResultWithHint("Use jellyfin_search to find valid item IDs.", "Jellyfin API error: %v", err), nil, nil
			}
			item := jf.ExtractDetailedItem(result)
			output := jf.ToDetailedItemOutput(item)
			return jf.TextResult(jf.FormatJSON(item)), output, nil
		})
	}

	// --- jellyfin_recommendations ---
	if enabled("jellyfin_recommendations", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_recommendations",
			Title: "Recommendations",
			InputSchema: jf.WithEnums[jf.RecommendationsInput](map[string][]any{
				"type": {"next_up", "suggestions", "latest", "similar", "movie_recs", "upcoming", "recently_played"},
			}),
			Description: "Get personalized content recommendations from Jellyfin. Returns compact results — use jellyfin_get_item for full metadata. " +
				"Common use cases: 'What should I watch?' -> try 'suggestions' for mixed content or 'movie_recs' for movies. " +
				"'What is next in my shows?' -> use 'next_up'. 'What is new on the server?' -> use 'latest'. " +
				"'More like this movie' -> use 'similar' with item_id. 'What did I watch recently?' -> use 'recently_played'.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.RecommendationsInput) (*mcp.CallToolResult, *jf.RecommendationsOutput, error) {
			limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			switch args.Type {
			case "next_up":
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview,ProductionYear"},
				}
				rawItems, _, err := jf.FetchAllPages(ctx, client, "/Shows/NextUp", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MapExtract(rawItems, jf.ExtractMediaItem)
				return jf.TextResult(fmt.Sprintf("Next Up (%d episodes):\n\n%s", len(items), jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			case "suggestions":
				params := url.Values{
					"Limit":  {fmt.Sprintf("%d", limit)},
					"UserId": {userID},
				}
				var result map[string]any
				if err := client.Get(ctx, "/Items/Suggestions", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ExtractItemList(result)
				return jf.TextResult(fmt.Sprintf("Suggestions (%d items):\n\n%s", len(items), jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			case "latest":
				params := url.Values{
					"Limit":  {fmt.Sprintf("%d", limit)},
					"UserId": {userID},
					"Fields": {"Overview,ProductionYear,CommunityRating"},
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				var result []map[string]any
				if err := client.Get(ctx, "/Items/Latest", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(result))
				for _, raw := range result {
					items = append(items, jf.ExtractMediaItem(raw))
				}
				return jf.TextResult(fmt.Sprintf("Latest additions (%d items):\n\n%s", len(items), jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			case "similar":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for 'similar' recommendations. Search for an item first to get its ID."), nil, nil
				}
				params := url.Values{
					"Limit":  {fmt.Sprintf("%d", limit)},
					"UserId": {userID},
				}
				endpoint := fmt.Sprintf("/Items/%s/Similar", jf.SanitizeID(args.ItemID))
				var result map[string]any
				if err := client.Get(ctx, endpoint, params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ExtractItemList(result)
				return jf.TextResult(fmt.Sprintf("Similar items (%d):\n\n%s", len(items), jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			case "movie_recs":
				params := url.Values{
					"ItemLimit": {fmt.Sprintf("%d", limit)},
					"UserId":    {userID},
				}
				var result []map[string]any
				if err := client.Get(ctx, "/Movies/Recommendations", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				// Deduplicate: group items by RecommendationType, skip duplicate item IDs
				seen := make(map[string]bool)
				grouped := make(map[string][]map[string]any)
				var order []string
				for _, cat := range result {
					recType := jf.GetString(cat, "RecommendationType")
					if _, exists := grouped[recType]; !exists {
						order = append(order, recType)
					}
					for _, raw := range jf.ToSlice(cat["Items"]) {
						m := jf.ToMap(raw)
						id := jf.GetString(m, "Id")
						if !seen[id] {
							seen[id] = true
							grouped[recType] = append(grouped[recType], jf.ExtractMediaItem(m))
						}
					}
				}
				categories := make([]map[string]any, 0, len(order))
				allItems := make([]map[string]any, 0)
				for _, recType := range order {
					categories = append(categories, map[string]any{
						"recommendation_type": recType,
						"items":               grouped[recType],
					})
					allItems = append(allItems, grouped[recType]...)
				}
				return jf.TextResult(fmt.Sprintf("Movie recommendations (%d categories):\n\n%s", len(categories), jf.FormatJSON(categories))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(allItems)}, nil

			case "upcoming":
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview,ProductionYear"},
				}
				rawItems, _, err := jf.FetchAllPages(ctx, client, "/Shows/Upcoming", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MapExtract(rawItems, jf.ExtractMediaItem)
				return jf.TextResult(fmt.Sprintf("Upcoming episodes (%d):\n\n%s", len(items), jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			case "recently_played":
				maxItems := jf.ClampInt(args.Limit, 500, jf.MaxLimitCap)
				params := url.Values{
					"Recursive": {"true"},
					"IsPlayed":  {"true"},
					"SortBy":    {"DatePlayed"},
					"SortOrder": {"Descending"},
					"Fields":    {"Overview,ProductionYear,CommunityRating,UserData"},
				}
				endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
				rawItems, total, err := jf.FetchAllPages(ctx, client, endpoint, params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					item := jf.ExtractMediaItem(m)
					if ud := jf.ToMap(m["UserData"]); ud != nil {
						if lp := jf.GetString(ud, "LastPlayedDate"); lp != "" {
							item["last_played"] = jf.Truncate(lp, jf.DateOnlyLen)
						}
					}
					items = append(items, item)
				}
				return jf.TextResult(fmt.Sprintf("Recently played (%d of %d):\n\n%s", len(items), total, jf.FormatJSON(items))), &jf.RecommendationsOutput{Items: jf.ToMediaItems(items)}, nil

			default:
				return jf.ErrResult("Invalid type '%s'. Valid types: next_up, suggestions, latest, similar, movie_recs, upcoming, recently_played", args.Type), nil, nil
			}
		})
	}

	// --- jellyfin_item_extras ---
	if enabled("jellyfin_item_extras", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_item_extras",
			Title: "Item Extras",
			InputSchema: jf.WithEnums[jf.ItemExtrasInput](map[string][]any{
				"action": {"playback_info", "special_features", "theme_songs", "theme_videos", "local_trailers", "segments", "download_url"},
			}),
			Description: "Get extended item information beyond basic metadata. " +
				"Use 'playback_info' for transcoding/direct play diagnostics (shows SupportsDirectPlay, SupportsTranscoding, TranscodingUrl). " +
				"Use 'special_features' for behind-the-scenes, extras, featurettes. " +
				"Use 'theme_songs' or 'theme_videos' for theme media. Use 'local_trailers' for trailers. " +
				"Use 'segments' for intro/outro/commercial markers (10.10+). " +
				"Use 'download_url' to get a direct download link for the item. " +
				"All actions require item_id from jellyfin_search or jellyfin_browse.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.ItemExtrasInput) (*mcp.CallToolResult, any, error) {
			if args.ItemID == "" {
				return jf.ErrResultWithHint("Use jellyfin_search to find item IDs.", "item_id is required."), nil, nil
			}
			itemID := jf.SanitizeID(args.ItemID)

			switch args.Action {
			case "playback_info":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				body := map[string]any{"UserId": userID}
				var result map[string]any
				endpoint := fmt.Sprintf("/Items/%s/PlaybackInfo", itemID)
				if err := client.Post(ctx, endpoint, nil, body, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				sources := jf.ToSlice(result["MediaSources"])
				items := make([]map[string]any, 0, len(sources))
				for _, raw := range sources {
					sm := jf.ToMap(raw)
					if sm == nil {
						continue
					}
					source := map[string]any{
						"id":                     jf.GetString(sm, "Id"),
						"name":                   jf.GetString(sm, "Name"),
						"container":              jf.GetString(sm, "Container"),
						"supports_direct_play":   jf.GetBool(sm, "SupportsDirectPlay"),
						"supports_direct_stream": jf.GetBool(sm, "SupportsDirectStream"),
						"supports_transcoding":   jf.GetBool(sm, "SupportsTranscoding"),
					}
					if tu := jf.GetString(sm, "TranscodingUrl"); tu != "" {
						source["transcoding_url"] = tu
					}
					if br := jf.GetInt64(sm, "Bitrate"); br > 0 {
						source["bitrate_kbps"] = br / jf.UnitsPerKilo
					}
					items = append(items, source)
				}
				return jf.TextResult(fmt.Sprintf("Playback info (%d sources):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "special_features":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var result []map[string]any
				endpoint := fmt.Sprintf("/Users/%s/Items/%s/SpecialFeatures", jf.SanitizeID(userID), itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(result))
				for _, m := range result {
					items = append(items, jf.ExtractMediaItem(m))
				}
				return jf.TextResult(fmt.Sprintf("Special features (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "theme_songs":
				var result map[string]any
				endpoint := fmt.Sprintf("/Items/%s/ThemeSongs", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ExtractItemList(result)
				return jf.TextResult(fmt.Sprintf("Theme songs (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "theme_videos":
				var result map[string]any
				endpoint := fmt.Sprintf("/Items/%s/ThemeVideos", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ExtractItemList(result)
				return jf.TextResult(fmt.Sprintf("Theme videos (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "local_trailers":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var result []map[string]any
				endpoint := fmt.Sprintf("/Users/%s/Items/%s/LocalTrailers", jf.SanitizeID(userID), itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(result))
				for _, m := range result {
					items = append(items, jf.ExtractMediaItem(m))
				}
				return jf.TextResult(fmt.Sprintf("Local trailers (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "segments":
				var result map[string]any
				endpoint := fmt.Sprintf("/MediaSegments/%s", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v. Media segments require Jellyfin 10.10+.", err), nil, nil
				}
				rawItems := jf.ToSlice(result["Items"])
				segments := make([]map[string]any, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					if m == nil {
						continue
					}
					segments = append(segments, map[string]any{
						"type":        jf.GetString(m, "Type"),
						"start_ticks": jf.GetInt64(m, "StartTicks"),
						"end_ticks":   jf.GetInt64(m, "EndTicks"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Media segments (%d):\n\n%s", len(segments), jf.FormatJSON(segments))), nil, nil

			case "download_url":
				downloadURL := fmt.Sprintf("%s/Items/%s/Download?ApiKey=%s", client.BaseURL(), args.ItemID, client.APIKey())
				return jf.TextResult(fmt.Sprintf("WARNING: This URL contains an embedded API key. Do not share it publicly or log it in untrusted contexts.\n\nDownload URL:\n%s", downloadURL)), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: playback_info, special_features, theme_songs, theme_videos, local_trailers, segments, download_url", args.Action), nil, nil
			}
		})
	}
}
