package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

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
				"Use this tool first to discover available libraries and their IDs before browsing content with jellyfin_browse. " +
				"Each library's item_id is the ID that parent_id and enabled_folder_ids take; jellyfin_library_manage takes the library's name.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, _ jf.NoInput) (*mcp.CallToolResult, *jf.LibraryListOutput, error) {
			var libs []map[string]any
			if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
				return jf.ErrResultWithHint("Check server with jellyfin_system_info action='ping'.", "Jellyfin API error: %v", err), nil, nil
			}
			items := jf.LibrariesFrom(libs)
			return nil, &jf.LibraryListOutput{Count: len(items), Libraries: items}, nil
		})
	}

	// --- jellyfin_search ---
	if enabled("jellyfin_search", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_search",
			Title: "Search Media",
			Description: "Search for media across the Jellyfin server by keyword. " +
				"Use this when the user provides a specific name or title to look up. " +
				"Returns compact results (name, type, year, truncated overview, rating, runtime, play status). Use jellyfin_get_item for full metadata. " +
				"NOTE: This tool searches by keyword only. To filter by genre, year, rating, or other attributes, use jellyfin_browse, such as jellyfin_browse genre=\"Horror\" rather than searching for \"horror\".",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SearchInput) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			limit := jf.ClampInt(args.Limit, 30, jf.MaxLimitCap)
			params := url.Values{
				"UserId":     {userID},
				"searchTerm": {args.Query},
				"Limit":      {fmt.Sprintf("%d", limit)},
				"Recursive":  {"true"},
				"Fields":     {"Overview"},
			}
			// People, artists, and genres are not library items: /Items
			// never lists them, so their names are searched on their own
			// endpoints, which take the same searchTerm and Limit.
			endpoint := "/Items"
			switch args.Type {
			case "Person":
				endpoint = "/Persons"
			case "MusicArtist":
				endpoint = "/Artists"
			case "Genre":
				endpoint = "/Genres"
			case "":
			default:
				params.Set("IncludeItemTypes", args.Type)
			}

			var result map[string]any
			if err := client.Get(ctx, endpoint, params, &result); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			items := jf.ItemsFrom(result)
			total := jf.GetInt(result, "TotalRecordCount")
			output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: items}
			// Jellyfin 12 ranks a window of at most three times limit
			// candidates by relevance and reports the window's size as the
			// total, so a full window means more may match.
			if v, err := client.ServerVersion(ctx); err == nil && v.AtLeast(12, 0) && total >= 3*limit {
				output.TotalIsLowerBound = true
			}
			// The note never offers paging. Jellyfin 12 applies StartIndex
			// inside the ranked window, so a page past it comes back empty.
			if len(items) < total {
				output.Notes = append(output.Notes, fmt.Sprintf("More results match than are shown. Refine the query, narrow it with type, or raise limit (up to %d) to see more.", jf.MaxLimitCap))
			}
			return nil, output, nil
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
				"Genres, studios, cast, and provider IDs are not included. Use jellyfin_get_item for full metadata. " +
				fmt.Sprintf("Filtering by min_date_created or max_date_created returns items newest first by date added and examines at most the %d most recently added items that match the other filters. ", jf.DefaultMaxItems) +
				"audio_languages and subtitle_languages filter by track language on Jellyfin 12 or later. " +
				"For another person's viewing, such as what they watched recently or what they haven't seen, pass their user_id from jellyfin_users list.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.BrowseInput) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
			window, byCreated, err := parseCreatedWindow(args.MinDateCreated, args.MaxDateCreated)
			if err != nil {
				return jf.ErrResult("%v", err), nil, nil
			}
			if byCreated && args.SortBy != "" && !strings.EqualFold(args.SortBy, "DateCreated") {
				return jf.ErrResult("min_date_created and max_date_created order results by creation date, newest first, so sort_by must be omitted or DateCreated, not %q.", args.SortBy), nil, nil
			}
			if byCreated && args.SortOrder != "" && !strings.EqualFold(args.SortOrder, "Descending") {
				return jf.ErrResult("min_date_created and max_date_created order results by creation date, newest first, so sort_order must be omitted or Descending, not %q.", args.SortOrder), nil, nil
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			// Another user is looked up once, here, so an unknown ID gets one
			// clear error whichever request path the filters take, and the
			// result can name whose play state it shows.
			var userNote []string
			if args.UserID != "" {
				var user map[string]any
				if err := client.Get(ctx, "/Users/"+jf.SanitizeID(args.UserID), nil, &user); err != nil {
					var apiErr *jf.APIError
					if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusBadRequest) {
						return jf.ErrResult("No user has ID '%s'. Use jellyfin_users action=list to find user IDs.", args.UserID), nil, nil
					}
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				userID = jf.GetString(user, "Id")
				userNote = []string{fmt.Sprintf("Played state, favorites, and last_played are those of user '%s'.", jf.GetString(user, "Name"))}
			}
			limit := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
			params := url.Values{
				"UserId":    {userID},
				"Recursive": {"true"},
				"Fields":    {"Overview"},
			}
			if args.ParentID != "" {
				params.Set("ParentId", args.ParentID)
			}
			if args.Type != "" {
				params.Set("IncludeItemTypes", args.Type)
			} else {
				// A recursive listing under a library includes the folders of
				// its paths, which are not media.
				params.Set("ExcludeItemTypes", "Folder")
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
			audioLangs, subtitleLangs := commaList(args.AudioLanguages), commaList(args.SubtitleLanguages)
			for _, f := range []struct{ name, given, codes string }{{"audio_languages", args.AudioLanguages, audioLangs}, {"subtitle_languages", args.SubtitleLanguages, subtitleLangs}} {
				if strings.TrimSpace(f.given) != "" && f.codes == "" {
					return jf.ErrResult("%s has no language codes: %q", f.name, f.given), nil, nil
				}
			}
			if subtitleLangs != "" && args.HasSubtitles != nil && !*args.HasSubtitles {
				return jf.ErrResult("subtitle_languages cannot be combined with has_subtitles=false: Jellyfin then ignores the languages and returns only items with no subtitles at all."), nil, nil
			}
			if audioLangs != "" || subtitleLangs != "" {
				// Jellyfin 10.11 ignores these parameters and would return
				// unfiltered results.
				v, err := client.ServerVersion(ctx)
				if err != nil {
					return jf.ErrResult("audio_languages and subtitle_languages need Jellyfin 12 or later, and the server's version could not be read: %v", err), nil, nil
				}
				if !v.AtLeast(12, 0) {
					return jf.ErrResult("audio_languages and subtitle_languages need Jellyfin 12 or later. This server runs Jellyfin %s.", v), nil, nil
				}
				if audioLangs != "" {
					params.Set("AudioLanguages", audioLangs)
				}
				if subtitleLangs != "" {
					params.Set("SubtitleLanguages", subtitleLangs)
				}
			}
			noMatches := func() []string { return languageHint(ctx, client, userID, args, audioLangs, subtitleLangs) }
			// Jellyfin 12 matches stream filters against every version and
			// part of a title, and lists each match as its own item.
			var perVersion []string
			if args.HasSubtitles != nil || audioLangs != "" || subtitleLangs != "" {
				if v, err := client.ServerVersion(ctx); err == nil && v.AtLeast(12, 0) {
					perVersion = []string{"On Jellyfin 12, has_subtitles and the language filters match each version and part of a title separately, so a title with several versions or parts can appear more than once, and the total counts each."}
				}
			}
			if userNote != nil {
				withoutUser := noMatches
				noMatches = func() []string { return slices.Concat(userNote, withoutUser()) }
				perVersion = slices.Concat(userNote, perVersion)
			}
			if args.MinPremiereDate != "" {
				params.Set("MinPremiereDate", args.MinPremiereDate)
			}
			if args.MaxPremiereDate != "" {
				params.Set("MaxPremiereDate", args.MaxPremiereDate)
			}
			if byCreated {
				return browseCreated(ctx, client, params, window, args.StartIndex, limit, noMatches, perVersion)
			}

			params.Set("Limit", fmt.Sprintf("%d", limit))
			if args.SortBy != "" {
				params.Set("SortBy", args.SortBy)
			} else {
				params.Set("SortBy", "SortName")
			}
			if args.SortOrder != "" {
				params.Set("SortOrder", args.SortOrder)
			}
			if args.StartIndex > 0 {
				params.Set("StartIndex", fmt.Sprintf("%d", args.StartIndex))
			}

			var result map[string]any
			if err := client.Get(ctx, "/Items", params, &result); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			rawItems := jf.ToSlice(result["Items"])
			items := jf.MediaItemsFrom(rawItems)
			if strings.EqualFold(args.SortBy, "DatePlayed") {
				// Sorted by play date, the date is what the caller is after.
				for i, raw := range rawItems {
					if lp := jf.LastPlayedOf(jf.ToMap(raw)); lp != "" {
						items[i].LastPlayed = jf.LocalDateTime(lp)
					}
				}
			}
			total := jf.GetInt(result, "TotalRecordCount")
			output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: items}
			if next := args.StartIndex + len(items); next < total {
				output.NextStartIndex = &next
			}
			if total == 0 {
				output.Notes = noMatches()
			} else {
				output.Notes = perVersion
			}
			return nil, output, nil
		})
	}

	// --- jellyfin_get_item ---
	if enabled("jellyfin_get_item", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_get_item",
			Title: "Item Details",
			Description: "Get comprehensive details about a specific media item by its ID. Returns full metadata including overview, genres, studios, cast/crew, " +
				"community and critic ratings, runtime, provider IDs (IMDb, TMDB, TVDB), user data (played status, favorite, rating), " +
				"media source info (codecs, resolution, bitrate, bit depth, HDR format, all audio tracks, all subtitle tracks with languages), " +
				"chapters (with start_ticks to seek to), and on Jellyfin 12 or later the original language and the collections that include the item.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.GetItemInput) (*mcp.CallToolResult, *jf.DetailedItemOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			item, err := jf.FetchItemDetails(ctx, client, args.ItemID, userID)
			if err != nil {
				return jf.ErrResultWithHint("Use jellyfin_search to find valid item IDs.", "Jellyfin API error: %v", err), nil, nil
			}
			return nil, &item, nil
		})
	}

	// --- jellyfin_recommendations ---
	if enabled("jellyfin_recommendations", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_recommendations",
			Title: "Recommendations",
			InputSchema: jf.WithEnums[jf.RecommendationsInput](map[string][]any{
				"action": {"next_up", "suggestions", "latest", "similar", "movie_recs", "upcoming", "recently_played"},
			}),
			Description: "Get personalized content recommendations from Jellyfin. Returns compact results. Use jellyfin_get_item for full metadata. " +
				"Common use cases: 'What should I watch?' -> try 'suggestions' for mixed content or 'movie_recs' for movies. " +
				"'What is next in my shows?' -> use 'next_up'. 'What is new on the server?' -> use 'latest'. " +
				"'More like this movie' -> use 'similar' with item_id. For what the user actually watched, use jellyfin_system_info playback_history; 'recently_played' also lists items marked played by hand.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.RecommendationsInput) (*mcp.CallToolResult, *jf.RecommendationsOutput, error) {
			limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			switch args.Action {
			case "next_up":
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview"},
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Shows/NextUp", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return nil, &jf.RecommendationsOutput{Items: &items, Notes: moreNote(len(items), total, maxItems, "use jellyfin_tv_shows next_up with a series_id")}, nil

			case "suggestions":
				params := url.Values{
					"Limit":  {fmt.Sprintf("%d", limit)},
					"UserId": {userID},
				}
				var result map[string]any
				if err := client.Get(ctx, "/Items/Suggestions", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ItemsFrom(result)
				return nil, &jf.RecommendationsOutput{Items: &items}, nil

			case "latest":
				items, err := jf.FetchLatest(ctx, client, args.ParentID, limit)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return nil, &jf.RecommendationsOutput{Items: &items}, nil

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
				items := jf.ItemsFrom(result)
				return nil, &jf.RecommendationsOutput{Items: &items}, nil

			case "movie_recs":
				params := url.Values{
					"ItemLimit": {fmt.Sprintf("%d", limit)},
					"UserId":    {userID},
				}
				var result []map[string]any
				if err := client.Get(ctx, "/Movies/Recommendations", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				// Jellyfin lists a movie under every reason that recommends
				// it; here it appears once, in the first group of its type.
				seen := make(map[string]bool)
				grouped := make(map[string]*jf.RecommendationCategory)
				categories := make([]jf.RecommendationCategory, 0, len(result))
				for _, cat := range result {
					recType := jf.GetString(cat, "RecommendationType")
					group, exists := grouped[recType]
					if !exists {
						categories = append(categories, jf.RecommendationCategory{Type: recType, Items: []jf.MediaItem{}})
						group = &categories[len(categories)-1]
						grouped[recType] = group
					}
					for _, raw := range jf.ToSlice(cat["Items"]) {
						m := jf.ToMap(raw)
						id := jf.GetString(m, "Id")
						if !seen[id] {
							seen[id] = true
							group.Items = append(group.Items, jf.MediaItemFrom(m))
						}
					}
				}
				return nil, &jf.RecommendationsOutput{Categories: &categories}, nil

			case "upcoming":
				maxItems := jf.ClampInt(args.Limit, 100, jf.MaxLimitCap)
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview"},
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Shows/Upcoming", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return nil, &jf.RecommendationsOutput{Items: &items, Notes: moreNote(len(items), total, maxItems, "")}, nil

			case "recently_played":
				maxItems := jf.ClampInt(args.Limit, 500, jf.MaxLimitCap)
				params := url.Values{
					"UserId":    {userID},
					"Recursive": {"true"},
					"IsPlayed":  {"true"},
					"SortBy":    {"DatePlayed"},
					"SortOrder": {"Descending"},
					"Fields":    {"Overview"},
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Items", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				for i, raw := range rawItems {
					if lp := jf.LastPlayedOf(jf.ToMap(raw)); lp != "" {
						items[i].LastPlayed = jf.LocalDate(lp)
					}
				}
				return nil, &jf.RecommendationsOutput{Items: &items, TotalCount: &total}, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: next_up, suggestions, latest, similar, movie_recs, upcoming, recently_played", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_item_extras ---
	if enabled("jellyfin_item_extras", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_item_extras",
			Title: "Item Extras",
			InputSchema: jf.WithEnums[jf.ItemExtrasInput](map[string][]any{
				"action": {"playback_info", "special_features", "theme_songs", "theme_videos", "local_trailers", "segments"},
			}),
			Description: "Get extended item information beyond basic metadata. " +
				"Use 'playback_info' for transcoding/direct play diagnostics (shows SupportsDirectPlay, SupportsTranscoding, TranscodingUrl). " +
				"Use 'special_features' for behind-the-scenes, extras, featurettes. " +
				"Use 'theme_songs' or 'theme_videos' for theme media. Use 'local_trailers' for trailers. " +
				"Use 'segments' for intro/outro/commercial markers. " +
				"For a download link, use jellyfin_download_link. " +
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
				endpoint := fmt.Sprintf("/Items/%s/SpecialFeatures", itemID)
				if err := client.Get(ctx, endpoint, url.Values{"UserId": {userID}}, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]jf.MediaItem, 0, len(result))
				for _, m := range result {
					items = append(items, jf.MediaItemFrom(m))
				}
				return jf.TextResult(fmt.Sprintf("Special features (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "theme_songs":
				var result map[string]any
				endpoint := fmt.Sprintf("/Items/%s/ThemeSongs", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ItemsFrom(result)
				return jf.TextResult(fmt.Sprintf("Theme songs (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "theme_videos":
				var result map[string]any
				endpoint := fmt.Sprintf("/Items/%s/ThemeVideos", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ItemsFrom(result)
				return jf.TextResult(fmt.Sprintf("Theme videos (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "local_trailers":
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				var result []map[string]any
				endpoint := fmt.Sprintf("/Items/%s/LocalTrailers", itemID)
				if err := client.Get(ctx, endpoint, url.Values{"UserId": {userID}}, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]jf.MediaItem, 0, len(result))
				for _, m := range result {
					items = append(items, jf.MediaItemFrom(m))
				}
				return jf.TextResult(fmt.Sprintf("Local trailers (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "segments":
				var result map[string]any
				endpoint := fmt.Sprintf("/MediaSegments/%s", itemID)
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
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

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: playback_info, special_features, theme_songs, theme_videos, local_trailers, segments", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_download_link ---
	if enabled("jellyfin_download_link", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_download_link",
			Title:       "Download Link",
			InputSchema: jf.WithEnums[jf.DownloadLinkInput](nil),
			Description: "Get the link to download an item: its page in the Jellyfin web app, where a signed-in user downloads the file with their own account, plus the file's path and size. " +
				"Use this whenever someone asks for a download link or wants to save a file from the server. " +
				"The item_id comes from jellyfin_search or jellyfin_browse.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.DownloadLinkInput) (*mcp.CallToolResult, any, error) {
			if args.ItemID == "" {
				return jf.ErrResultWithHint("Use jellyfin_search to find item IDs.", "item_id is required."), nil, nil
			}
			itemID := jf.SanitizeID(args.ItemID)
			// The download route needs the API key, which must not reach the
			// client, so the link opens the item in the Jellyfin web app,
			// where the signed-in user downloads it with their own account
			// and Jellyfin's per-user download permission applies.
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			var item map[string]any
			if err := client.Get(ctx, fmt.Sprintf("/Items/%s", itemID), url.Values{"UserId": {userID}}, &item); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			link := map[string]any{
				"name":     jf.GetString(item, "Name"),
				"web_page": fmt.Sprintf("%s/web/#/details?id=%s", client.BaseURL(), itemID),
			}
			if p := jf.GetString(item, "Path"); p != "" {
				link["file_path"] = p
			}
			var size int64
			for _, src := range jf.ToSlice(item["MediaSources"]) {
				size += jf.GetInt64(jf.ToMap(src), "Size")
			}
			if size > 0 {
				link["file_size_bytes"] = size
			}
			return jf.TextResult(fmt.Sprintf("Open this page in the Jellyfin web app and use its Download button (the download runs with the signed-in user's own permissions):\n\n%s", jf.FormatJSON(link))), nil, nil
		})
	}
}

// createdWindow is a range of DateCreated values. Both ends are inclusive, and
// a zero end is open.
type createdWindow struct {
	min, max time.Time
}

// createdScan is what scanCreated found.
type createdScan struct {
	items   []map[string]any // raw items inside the window, newest first
	undated int              // items left out because DateCreated was absent or unreadable
	capped  bool             // the scan reached its bound, so older matches may be missing
}

// parseCreatedWindow reads the min_date_created and max_date_created inputs
// and reports whether either is set. A YYYY-MM-DD date covers that whole day
// in the server's time zone (see parseDateArg); an RFC 3339 timestamp is exact.
func parseCreatedWindow(minArg, maxArg string) (createdWindow, bool, error) {
	var w createdWindow
	if minArg != "" {
		t, _, err := parseDateArg("min_date_created", minArg)
		if err != nil {
			return w, false, err
		}
		w.min = t
	}
	if maxArg != "" {
		t, err := parseMaxDateArg("max_date_created", maxArg)
		if err != nil {
			return w, false, err
		}
		w.max = t
	}
	if minArg != "" && maxArg != "" && w.min.After(w.max) {
		return w, false, fmt.Errorf("min_date_created (%s) is later than max_date_created (%s)", minArg, maxArg)
	}
	return w, minArg != "" || maxArg != "", nil
}

// parseDateArg parses a date input given as YYYY-MM-DD, which it reads as the
// start of that day in the server's time zone and reports as date-only, or as
// an RFC 3339 timestamp. The server's time zone is the one the server
// instructions give the current date in, so a date the model derives from them
// means the day it intends.
func parseDateArg(name, s string) (time.Time, bool, error) {
	if t, err := time.Parse(jf.DateOnlyFormat, s); err == nil {
		y, m, d := t.Date()
		return startOfDay(y, m, d, time.Local), true, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, false, nil
	}
	return time.Time{}, false, fmt.Errorf("%s must be a date such as 2024-01-31 or an RFC 3339 timestamp such as 2024-01-31T18:00:00Z, not %q", name, s)
}

// commaList trims the entries of a comma-separated list and drops empty ones.
func commaList(s string) string {
	var entries []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			entries = append(entries, e)
		}
	}
	return strings.Join(entries, ",")
}

// languageHint explains a language filter that matched nothing: for each
// requested code that no item in the browsed scope has, it lists the codes
// that do occur. It returns "" when every code occurs, because then another
// filter excluded the items. Jellyfin reports stream languages only for video
// types, so a non-video type gets no hint, and an untyped browse asks about
// movies, series, and episodes. audio and subtitle are the comma-separated
// code lists sent.
func languageHint(ctx context.Context, client jf.Client, userID string, args jf.BrowseInput, audio, subtitle string) []string {
	if audio == "" && subtitle == "" {
		return nil
	}
	types := "Movie,Series,Episode"
	if args.Type != "" {
		switch strings.ToLower(args.Type) {
		case "movie", "series", "season", "episode":
			types = args.Type
		default:
			return nil
		}
	}
	params := url.Values{"UserId": {userID}, "IncludeItemTypes": {types}}
	if args.ParentID != "" {
		params.Set("ParentId", args.ParentID)
	}
	var filters map[string]any
	if err := client.Get(ctx, "/Items/Filters2", params, &filters); err != nil {
		return nil
	}
	var hint []string
	for _, kind := range []struct{ label, key, requested string }{{"audio", "AudioLanguages", audio}, {"subtitle", "SubtitleLanguages", subtitle}} {
		if kind.requested == "" {
			continue
		}
		present := map[string]bool{}
		var known []string
		for _, l := range jf.ToSlice(filters[kind.key]) {
			m := jf.ToMap(l)
			code := jf.GetString(m, "Value")
			if code == "" {
				continue
			}
			present[code] = true
			// Jellyfin names a language "English (eng)", or by its code alone.
			if name := strings.TrimSuffix(jf.GetString(m, "Name"), " ("+code+")"); name != "" && name != code {
				code += " (" + name + ")"
			}
			known = append(known, code)
		}
		var missing []string
		for _, code := range strings.Split(kind.requested, ",") {
			if !present[code] {
				missing = append(missing, code)
			}
		}
		switch {
		case len(missing) == 0:
		case len(known) == 0:
			hint = append(hint, fmt.Sprintf("No item here has %s tracks in %s, or any %s track with a language.", kind.label, strings.Join(missing, ", "), kind.label))
		default:
			hint = append(hint, fmt.Sprintf("No item here has %s tracks in %s. The %s languages here are %s.", kind.label, strings.Join(missing, ", "), kind.label, strings.Join(known, ", ")))
		}
	}
	return hint
}

// parseMaxDateArg parses an upper date bound given as for parseDateArg. A
// YYYY-MM-DD date includes that whole day, so it ends at the last instant
// before the next day starts.
func parseMaxDateArg(name, s string) (time.Time, error) {
	t, dateOnly, err := parseDateArg(name, s)
	if err != nil || !dateOnly {
		return t, err
	}
	y, m, d := t.Date()
	return startOfDay(y, m, d+1, time.Local).Add(-time.Nanosecond), nil
}

// startOfDay returns the first instant of the given day in loc. The day
// normally starts at midnight, but where a clock change skips midnight, such as
// a daylight saving start at 00:00, it starts when the new offset takes effect.
// d may be out of range, as time.Date allows.
func startOfDay(y int, m time.Month, d int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if t.Hour() == 0 {
		return t
	}
	// Midnight is in the gap. time.Date puts it either after the gap, which is
	// the day's start, or late on the previous day at the old offset, in which
	// case the day starts where that offset ends.
	if noon := time.Date(y, m, d, 12, 0, 0, 0, loc); t.Day() != noon.Day() {
		_, end := t.ZoneBounds()
		return end
	}
	return t
}

// scanCreated lists /Items newest first by DateCreated and keeps the items
// inside w. It sets SortBy, SortOrder, and Fields in params. The endpoint has
// no creation-date filter, so the window is applied here: items newer than
// w.max are skipped, and paging stops at the first item older than w.min. At
// most jf.DefaultMaxItems items are examined. An item without a readable
// DateCreated cannot be placed in the window; it is left out and counted
// rather than ending the scan.
func scanCreated(ctx context.Context, client jf.Client, params url.Values, w createdWindow) (createdScan, error) {
	params.Set("SortBy", "DateCreated")
	params.Set("SortOrder", "Descending")
	fields := "DateCreated"
	if f := params.Get("Fields"); f != "" {
		fields = f + "," + fields
	}
	params.Set("Fields", fields)

	var stop func(map[string]any) bool
	if !w.min.IsZero() {
		stop = func(item map[string]any) bool {
			created, ok := jf.ParseTime(jf.GetString(item, "DateCreated"))
			return ok && created.Before(w.min)
		}
	}
	rawItems, total, err := jf.FetchAllPagesUntil(ctx, client, "/Items", params, jf.DefaultMaxItems, stop)
	if err != nil {
		return createdScan{}, err
	}

	scan := createdScan{capped: len(rawItems) >= jf.DefaultMaxItems && total > len(rawItems)}
	// Offset paging over a list that can grow while it is read can return an
	// item on two pages: an addition at the top pushes the previous page's last
	// item onto the next one. Each item is counted once.
	seen := make(map[string]bool, len(rawItems))
	for _, raw := range rawItems {
		m := jf.ToMap(raw)
		if id := jf.GetString(m, "Id"); id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		created, ok := jf.ParseTime(jf.GetString(m, "DateCreated"))
		switch {
		case !ok:
			scan.undated++
		case !w.max.IsZero() && created.After(w.max):
			// Newer than the window.
		default:
			scan.items = append(scan.items, m)
		}
	}
	return scan, nil
}

// notes explains what the scan could not cover. The undated count is a
// field of the result; the note is for the bound.
func (s createdScan) notes() []string {
	if s.capped {
		return []string{fmt.Sprintf("Only the %d most recently added items were examined, so older matches are not included. Narrow the request with filters such as type or parent_id to reach them.", jf.DefaultMaxItems)}
	}
	return nil
}

// page is the scan's items from start to end, newest first, each with its
// date added.
func (s createdScan) page(start, end int) []jf.MediaItem {
	items := make([]jf.MediaItem, 0, end-start)
	for _, m := range s.items[start:end] {
		item := jf.MediaItemFrom(m)
		if dc := jf.GetString(m, "DateCreated"); dc != "" {
			item.DateAdded = jf.LocalDate(dc)
		}
		items = append(items, item)
	}
	return items
}

// browseCreated answers jellyfin_browse when a creation-date filter is set.
// The window is applied by scanCreated, so start_index and limit page through
// the filtered list here, and the total counts only matching items. When
// nothing matches, the result's notes end with what noMatches returns, and
// otherwise with perVersion.
func browseCreated(ctx context.Context, client jf.Client, params url.Values, w createdWindow, startIndex, limit int, noMatches func() []string, perVersion []string) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
	scan, err := scanCreated(ctx, client, params, w)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	total := len(scan.items)
	start := min(max(startIndex, 0), total)
	end := min(start+limit, total)
	items := scan.page(start, end)
	output := &jf.ItemListOutput{TotalCount: total, TotalIsLowerBound: scan.capped, Shown: len(items), UndatedCount: scan.undated, Items: items, Notes: scan.notes()}
	if end < total {
		output.NextStartIndex = &end
	}
	if total == 0 {
		output.Notes = append(output.Notes, noMatches()...)
	} else {
		output.Notes = append(output.Notes, perVersion...)
	}
	return nil, output, nil
}
