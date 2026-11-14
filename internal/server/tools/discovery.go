package tools

import (
	"context"
	"fmt"
	"maps"
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
			Name:         "jellyfin_libraries",
			Title:        "List Libraries",
			InputSchema:  jf.SchemaFor[jf.NoInput](),
			OutputSchema: jf.SchemaFor[jf.LibraryListOutput](),
			Description:  "List configured media libraries with collection types, paths, and item IDs.",
			Annotations:  AnnotReadOnly,
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
			Name:         "jellyfin_search",
			Title:        "Search Media",
			InputSchema:  jf.SchemaFor[jf.SearchInput](),
			OutputSchema: jf.SchemaFor[jf.ItemListOutput](),
			Description: "Search library items, people, studios, music artists, or genres by name. " +
				"Returns compact summaries; use jellyfin_get_item for full metadata and jellyfin_browse for attribute filtering.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SearchInput) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
			trimmedQuery := strings.TrimSpace(args.Query)
			isCatalogType := args.Type == "Person" || args.Type == "Studio" || args.Type == "MusicArtist" || args.Type == "Genre"
			if trimmedQuery == "" && !isCatalogType {
				return jf.ErrResult("query is required unless type is Person, Studio, MusicArtist, or Genre (use jellyfin_browse to list library items without a search query)."), nil, nil
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			defaultLimit := 30
			if trimmedQuery == "" && isCatalogType {
				defaultLimit = 50
			}
			limit := jf.ClampInt(args.Limit, defaultLimit, jf.MaxLimitCap)

			if args.Type == "Studio" {
				studioParams := url.Values{"UserId": {userID}}
				if args.Query != "" {
					studioParams.Set("searchTerm", args.Query)
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Studios", studioParams, limit)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: items}
				if len(items) < total {
					output.Notes = moreNote(len(items), total, limit, "refine query")
				}
				return nil, output, nil
			}

			params := url.Values{
				"UserId":    {userID},
				"Limit":     {fmt.Sprintf("%d", limit)},
				"Recursive": {"true"},
				"Fields":    {"Overview,Width,Height"},
			}
			if args.Query != "" {
				params.Set("searchTerm", args.Query)
			}

			endpoint := "/Items"
			switch args.Type {
			case "Person":
				endpoint = "/Persons"
				params.Set("ExcludePersonTypes", "Artist,AlbumArtist")
				params.Del("Recursive")
				params.Del("Fields")
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
			if endpoint == "/Items" {
				if v, err := client.ServerVersion(ctx); err == nil && v.AtLeast(12, 0) && total >= 3*limit {
					output.TotalIsLowerBound = true
				}
			}
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
			InputSchema: jf.WithEnums[jf.BrowseInput](map[string][]any{
				"resolution": {"sd", "720p", "1080p", "1440p", "4k", "8k"},
			}),
			OutputSchema: jf.SchemaFor[jf.ItemListOutput](),
			Description: "Browse, filter, and count (total_count) library items by genre, year, studio, person, rating, language, resolution, date, or play state. " +
				"Returns compact summaries (or matching series when group_by_series=true); use jellyfin_get_item for full metadata.",
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
			itemTypes := commaList(args.Type)
			if strings.TrimSpace(args.Type) != "" && itemTypes == "" {
				return jf.ErrResult("type has no item types: %q", args.Type), nil, nil
			}
			groupBySeries := args.GroupBySeries != nil && *args.GroupBySeries
			if groupBySeries {
				if !strings.EqualFold(itemTypes, "Episode") {
					return jf.ErrResult("group_by_series needs type=Episode: it lists the series that the matching episodes belong to."), nil, nil
				}
				if byCreated {
					return jf.ErrResult("group_by_series cannot be combined with min_date_created or max_date_created. Omit group_by_series to list the matching episodes with their series_name and date_added."), nil, nil
				}
				if args.SortBy != "" || args.SortOrder != "" {
					return jf.ErrResult("group_by_series lists the series by name, so sort_by and sort_order must be omitted."), nil, nil
				}
			}
			if args.Resolution != "" {
				if args.IsHD != nil || args.Is4K != nil || args.MinWidth != nil || args.MaxWidth != nil || args.MinHeight != nil || args.MaxHeight != nil {
					return jf.ErrResult("resolution cannot be combined with is_hd, is_4k, min_width, max_width, min_height, or max_height."), nil, nil
				}
				if errRes := applyResolutionTier(&args, &itemTypes); errRes != nil {
					return errRes, nil, nil
				}
			}
			for _, f := range []struct {
				name  string
				value *int
			}{{"min_height", args.MinHeight}, {"max_height", args.MaxHeight}, {"min_width", args.MinWidth}, {"max_width", args.MaxWidth}} {
				if f.value != nil && *f.value <= 0 {
					return jf.ErrResult("%s must be a positive number of pixels, not %d.", f.name, *f.value), nil, nil
				}
			}
			if args.MinWidth != nil && args.MaxWidth != nil && *args.MinWidth > *args.MaxWidth {
				return jf.ErrResult("min_width (%d) is greater than max_width (%d).", *args.MinWidth, *args.MaxWidth), nil, nil
			}
			if args.MinHeight != nil && args.MaxHeight != nil && *args.MinHeight > *args.MaxHeight {
				return jf.ErrResult("min_height (%d) is greater than max_height (%d).", *args.MinHeight, *args.MaxHeight), nil, nil
			}
			if args.Is4K != nil && !*args.Is4K {
				return jf.ErrResult("is_4k=false matches nothing: Jellyfin treats is_4k as a bucket to include. Omit is_4k and use resolution='1080p'/'720p'/'sd', is_hd=true alone for HD without 4K/8K, or is_hd=false for standard definition."), nil, nil
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
			if errResult := checkResolutionFilters(ctx, client, args, itemTypes); errResult != nil {
				return errResult, nil, nil
			}
			if audioLangs != "" || subtitleLangs != "" {
				v, err := client.ServerVersion(ctx)
				if err != nil {
					return jf.ErrResult("audio_languages and subtitle_languages need Jellyfin 12 or later, and the server's version could not be read: %v. Use jellyfin_get_item to inspect audio_streams or subtitle_streams on individual items.", err), nil, nil
				}
				if !v.AtLeast(12, 0) {
					return jf.ErrResult("audio_languages and subtitle_languages need Jellyfin 12 or later. This server runs Jellyfin %s. Use jellyfin_get_item to inspect audio_streams or subtitle_streams on individual items.", v), nil, nil
				}
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}
			var userNote []string
			if args.UserID != "" {
				resolvedID, resolvedName, _, err := jf.ResolveUser(ctx, client, args.UserID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				userID = resolvedID
				userNote = []string{fmt.Sprintf("Played state, favorites, and last_played are those of user '%s'.", resolvedName)}
			}
			if args.ParentID != "" {
				resolvedParent, err := jf.ResolveLibraryID(ctx, client, args.ParentID)
				if err != nil {
					return jf.ErrResult("Invalid parent_id: %v", err), nil, nil
				}
				args.ParentID = resolvedParent
			}
			limit := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
			startIndex := max(args.StartIndex, 0)
			params := url.Values{
				"UserId":    {userID},
				"Recursive": {"true"},
				"Fields":    {"Overview,Width,Height"},
			}
			if args.ParentID != "" {
				params.Set("ParentId", args.ParentID)
			}
			if itemTypes != "" {
				params.Set("IncludeItemTypes", itemTypes)
			} else {
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
				parts := jf.SplitIDs([]string{args.Person})
				if len(parts) > 0 && !slices.ContainsFunc(parts, func(p string) bool { return !jf.IsJellyfinID(p) }) {
					norm := make([]string, len(parts))
					for i, p := range parts {
						norm[i] = jf.NormalizeID(p)
					}
					params.Set("PersonIds", strings.Join(norm, ","))
				} else {
					params.Set("Person", args.Person)
				}
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
			for _, f := range []struct {
				value *int
				param string
			}{{args.MinHeight, "MinHeight"}, {args.MaxHeight, "MaxHeight"}, {args.MinWidth, "MinWidth"}, {args.MaxWidth, "MaxWidth"}} {
				if f.value != nil {
					params.Set(f.param, fmt.Sprintf("%d", *f.value))
				}
			}
			if args.IsHD != nil {
				params.Set("IsHD", fmt.Sprintf("%v", *args.IsHD))
			}
			if args.Is4K != nil {
				params.Set("Is4K", "true")
			}
			if args.MaxWidth != nil && args.MinWidth == nil {
				params.Set("MinWidth", "1")
			}
			if args.MaxHeight != nil && args.MinHeight == nil {
				params.Set("MinHeight", "1")
			}
			if args.IsHD != nil && !*args.IsHD && args.MinWidth == nil && !strings.EqualFold(itemTypes, "Series") {
				params.Set("MinWidth", "1")
			}
			if audioLangs != "" {
				params.Set("AudioLanguages", audioLangs)
			}
			if subtitleLangs != "" {
				params.Set("SubtitleLanguages", subtitleLangs)
			}
			noMatches := func() []string {
				a := args
				a.ParentID = params.Get("ParentId")
				return languageHint(ctx, client, userID, a, audioLangs, subtitleLangs)
			}
			var perVersion, seriesVersionNote []string
			if strings.EqualFold(itemTypes, "Series") && (args.IsHD != nil || args.Is4K != nil) {
				ancestorNote := []string{"Series resolution filters read Jellyfin's ancestor index (mixed-resolution shows can match both buckets, and out-of-sync series can be missed); use type=Episode with group_by_series=true for complete results and episode counts."}
				perVersion = ancestorNote
				withoutAncestor := noMatches
				noMatches = func() []string { return slices.Concat(withoutAncestor(), ancestorNote) }
			} else if (args.HasSubtitles != nil || audioLangs != "" || subtitleLangs != "" || args.IsHD != nil || args.Is4K != nil) &&
				(itemTypes == "" || hasVideoType(itemTypes)) {
				if v, err := client.ServerVersion(ctx); err == nil && v.AtLeast(12, 0) {
					perVersion = []string{"On Jellyfin 12, has_subtitles, the language filters, is_hd, and is_4k match each version and part of a video separately, so a title with several versions or parts can appear more than once, and the total counts each."}
					seriesVersionNote = []string{"On Jellyfin 12, has_subtitles, the language filters, is_hd, and is_4k match each version and part of an episode separately, so an episode with several versions or parts counts once per matching version or part in matching_episodes."}
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
			if groupBySeries {
				byResolution := args.MinHeight != nil || args.MaxHeight != nil || args.MinWidth != nil || args.MaxWidth != nil || args.IsHD != nil || args.Is4K != nil
				seriesNotes := slices.Concat(
					userNote,
					[]string{"Each series appears once. matching_episodes counts the episodes that match the filters; total_episodes counts the series' available episodes under the filters other than video resolution."},
					seriesVersionNote,
				)
				return browseSeries(ctx, req, client, params, byResolution, startIndex, limit, noMatches, seriesNotes)
			}
			if byCreated {
				return browseCreated(ctx, client, params, window, startIndex, limit, noMatches, perVersion)
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
			if startIndex > 0 {
				params.Set("StartIndex", fmt.Sprintf("%d", startIndex))
			}

			var result map[string]any
			if err := client.Get(ctx, "/Items", params, &result); err != nil {
				return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
			}
			rawItems := jf.ToSlice(result["Items"])
			items := jf.MediaItemsFrom(rawItems)
			if strings.EqualFold(args.SortBy, "DatePlayed") {
				for i, raw := range rawItems {
					if lp := jf.LastPlayedOf(jf.ToMap(raw)); lp != "" {
						items[i].LastPlayed = jf.LocalDateTime(lp)
					}
				}
			}
			total := jf.GetInt(result, "TotalRecordCount")
			output := &jf.ItemListOutput{TotalCount: total, Shown: len(items), Items: items}
			if next := startIndex + len(items); next < total {
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
			Name:         "jellyfin_get_item",
			Title:        "Item Details",
			InputSchema:  jf.SchemaFor[jf.GetItemInput](),
			OutputSchema: jf.SchemaFor[jf.DetailedItemOutput](),
			Description:  "Get full item metadata: overview, genres, studios, cast/crew, ratings, provider IDs/external URLs, user play state, media streams (codecs, HDR, audio/subtitle tracks), chapters, and collections.",
			Annotations:  AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.GetItemInput) (*mcp.CallToolResult, *jf.DetailedItemOutput, error) {
			if strings.TrimSpace(args.ItemID) == "" {
				return jf.ErrResultWithHint("Use jellyfin_search to find valid item IDs.", "item_id is required."), nil, nil
			}
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
			OutputSchema: jf.SchemaFor[jf.RecommendationsOutput](),
			Description:  "Get personalized suggestions, next-up or upcoming episodes, latest library additions, similar items, movie recommendation groups, or recently marked-played items.",
			Annotations:  AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.RecommendationsInput) (*mcp.CallToolResult, *jf.RecommendationsOutput, error) {
			limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
			userID, err := jf.EffectiveUserID(ctx, client, args.UserID)
			if err != nil {
				return jf.ResolveErrResult(err), nil, nil
			}

			switch args.Action {
			case "next_up":
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
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
				parentID := args.ParentID
				if parentID != "" {
					resolved, err := jf.ResolveLibraryID(ctx, client, parentID)
					if err != nil {
						return jf.ErrResult("Invalid parent_id: %v", err), nil, nil
					}
					parentID = resolved
				}
				items, err := jf.FetchLatestForUser(ctx, client, userID, parentID, limit)
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
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
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
				maxItems := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
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
				return nil, &jf.RecommendationsOutput{Items: &items, TotalCount: &total, Notes: moreNote(len(items), total, maxItems, "")}, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: next_up, suggestions, latest, similar, movie_recs, upcoming, recently_played", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_item_extras ---
	if enabled("jellyfin_item_extras", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_item_extras",
			Title: "Item Extras & Download Link",
			InputSchema: jf.WithEnums[jf.ItemExtrasInput](map[string][]any{
				"action": {"playback_info", "special_features", "theme_songs", "theme_videos", "local_trailers", "segments", "download_link"},
			}),
			Description: "Fetch item playback/transcoding capabilities, special features, local trailers, theme songs/videos, intro/outro segments, or web app download link (with file path and size).",
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

			case "download_link":
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

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: playback_info, special_features, theme_songs, theme_videos, local_trailers, segments, download_link", args.Action), nil, nil
			}
		})
	}
}

func intPtr(v int) *int { return &v }

// applyResolutionTier maps a friendly resolution tier ("sd", "720p", "1080p",
// "1440p", "4k", "8k") to IsHD/Is4K + pixel bounds so cropped widescreen
// encodes, 4:3 HD/UHD transfers, DCI 4K, and portrait 4K all match their tier,
// and defaults type to video item types when omitted.
func applyResolutionTier(args *jf.BrowseInput, itemTypes *string) *mcp.CallToolResult {
	if *itemTypes == "" {
		*itemTypes = strings.Join(videoTypes, ",")
	}
	switch strings.ToLower(strings.TrimSpace(args.Resolution)) {
	case "sd":
		args.IsHD = jf.BoolPtr(false)
	case "720p":
		args.IsHD = jf.BoolPtr(true)
		args.MaxWidth = intPtr(1799)
		args.MaxHeight = intPtr(999)
	case "1080p":
		args.IsHD = jf.BoolPtr(true)
		args.MinWidth = intPtr(1400)
		args.MaxWidth = intPtr(2499)
		args.MaxHeight = intPtr(1399)
	case "1440p":
		args.IsHD = jf.BoolPtr(true)
		args.MinWidth = intPtr(2500)
		args.MaxWidth = intPtr(3799)
		args.MaxHeight = intPtr(2099)
	case "4k":
		args.Is4K = jf.BoolPtr(true)
		args.MaxWidth = intPtr(7599)
		args.MaxHeight = intPtr(4199)
	case "8k":
		args.Is4K = jf.BoolPtr(true)
		args.MinWidth = intPtr(7600)
	default:
		return jf.ErrResult("Invalid resolution %q. Valid values: sd, 720p, 1080p, 1440p, 4k, 8k.", args.Resolution)
	}
	return nil
}

type createdWindow struct {
	min, max time.Time
}

type createdScan struct {
	items   []map[string]any
	undated int
	capped  bool
}

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

func commaList(s string) string {
	var entries []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" && !slices.ContainsFunc(entries, func(p string) bool { return strings.EqualFold(p, e) }) {
			entries = append(entries, e)
		}
	}
	return strings.Join(entries, ",")
}

func languageHint(ctx context.Context, client jf.Client, userID string, args jf.BrowseInput, audio, subtitle string) []string {
	if audio == "" && subtitle == "" {
		return nil
	}
	types := "Movie,Series,Episode"
	if reqTypes := commaList(args.Type); reqTypes != "" {
		for _, e := range strings.Split(reqTypes, ",") {
			switch strings.ToLower(e) {
			case "movie", "series", "season", "episode", "musicvideo", "video":
			default:
				return nil
			}
		}
		types = reqTypes
	} else if strings.TrimSpace(args.Type) != "" {
		return nil
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

func parseMaxDateArg(name, s string) (time.Time, error) {
	t, dateOnly, err := parseDateArg(name, s)
	if err != nil || !dateOnly {
		return t, err
	}
	y, m, d := t.Date()
	return startOfDay(y, m, d+1, time.Local).Add(-time.Nanosecond), nil
}

func startOfDay(y int, m time.Month, d int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if t.Hour() == 0 {
		return t
	}
	if noon := time.Date(y, m, d, 12, 0, 0, 0, loc); t.Day() != noon.Day() {
		_, end := t.ZoneBounds()
		return end
	}
	return t
}

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
		default:
			scan.items = append(scan.items, m)
		}
	}
	return scan, nil
}

func (s createdScan) notes() []string {
	if s.capped {
		return []string{fmt.Sprintf("Only the %d most recently added items were examined, so older matches are not included. Narrow the request with filters such as type or parent_id to reach them.", jf.DefaultMaxItems)}
	}
	return nil
}

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

var videoTypes = []string{"Movie", "Episode", "MusicVideo", "Video"}

func hasVideoType(itemTypes string) bool {
	for _, e := range strings.Split(itemTypes, ",") {
		if slices.ContainsFunc(videoTypes, func(v string) bool { return strings.EqualFold(e, v) }) {
			return true
		}
	}
	return false
}

func checkResolutionFilters(ctx context.Context, client jf.Client, args jf.BrowseInput, itemTypes string) *mcp.CallToolResult {
	pixels := args.MinHeight != nil || args.MaxHeight != nil || args.MinWidth != nil || args.MaxWidth != nil
	if !pixels && args.IsHD == nil && args.Is4K == nil {
		return nil
	}
	var entries []string
	if itemTypes != "" {
		entries = strings.Split(itemTypes, ",")
	}
	if !pixels && len(entries) == 1 && strings.EqualFold(entries[0], "Series") {
		v, err := client.ServerVersion(ctx)
		if err != nil {
			return jf.ErrResult("is_hd and is_4k on a series need Jellyfin 12 or later, and the server's version could not be read: %v. Use type=Episode with group_by_series=true to list the shows instead.", err)
		}
		if !v.AtLeast(12, 0) {
			return jf.ErrResult("is_hd and is_4k on a series need Jellyfin 12 or later. This server runs Jellyfin %s. Use type=Episode with group_by_series=true to list the shows instead.", v)
		}
		return nil
	}
	if !pixels && len(entries) > 1 && slices.ContainsFunc(entries, func(e string) bool { return strings.EqualFold(e, "Series") }) {
		return jf.ErrResult("type=Series cannot be combined with other types when filtering by video resolution: browse type=Series on its own on Jellyfin 12 or later, or use type=Episode with group_by_series=true.")
	}
	allowed := len(entries) > 0 && !slices.ContainsFunc(entries, func(e string) bool {
		return !slices.ContainsFunc(videoTypes, func(v string) bool { return strings.EqualFold(e, v) })
	})
	if !allowed {
		if args.Resolution != "" {
			return jf.ErrResult("resolution requires video item types (Movie, Episode, MusicVideo, or Video; omit type to include all four). To list TV shows by resolution, use type=Episode with group_by_series=true.")
		}
		if pixels {
			return jf.ErrResult("min_width, max_width, min_height, and max_height need type=Movie, Episode, MusicVideo, or Video (or a comma-separated list of them), because other items have no video of their own. To list shows by resolution, use type=Episode with group_by_series=true.")
		}
		return jf.ErrResult("is_hd and is_4k need type=Movie, Episode, MusicVideo, or Video (or a comma-separated list of them), or type=Series on its own on Jellyfin 12 or later, because other items have no video of their own. To list shows with matching episode counts, use type=Episode with group_by_series=true.")
	}
	return nil
}

type seriesTally struct {
	name     string
	episodes map[string]struct{}
}

func tallyEpisodesBySeries(ctx context.Context, req *mcp.CallToolRequest, client jf.Client, params url.Values, what string, step *int) (map[string]*seriesTally, error) {
	p := maps.Clone(params)
	p.Del("Fields")
	p.Set("IsMissing", "false")
	p.Set("EnableImages", "false")
	p.Set("EnableUserData", "false")
	tallies := make(map[string]*seriesTally)
	_, err := jf.EachPage(ctx, client, "/Items", p, func(page int, items []map[string]any) {
		jf.ReportProgress(ctx, req, float64(*step), 0, fmt.Sprintf("Scanning %s, page %d...", what, page))
		*step++
		for _, m := range items {
			id := jf.GetString(m, "Id")
			seriesID := jf.GetString(m, "SeriesId")
			if id == "" || seriesID == "" {
				continue
			}
			t := tallies[seriesID]
			if t == nil {
				t = &seriesTally{episodes: make(map[string]struct{})}
				tallies[seriesID] = t
			}
			if t.name == "" {
				t.name = jf.GetString(m, "SeriesName")
			}
			t.episodes[id] = struct{}{}
		}
	})
	return tallies, err
}

func browseSeries(ctx context.Context, req *mcp.CallToolRequest, client jf.Client, params url.Values, byResolution bool, startIndex, limit int, noMatches func() []string, notes []string) (*mcp.CallToolResult, *jf.ItemListOutput, error) {
	var step int
	matching, err := tallyEpisodesBySeries(ctx, req, client, params, "matching episodes", &step)
	if err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	totals := matching
	if byResolution && len(matching) > 0 {
		all := maps.Clone(params)
		for _, k := range []string{"MinHeight", "MaxHeight", "MinWidth", "MaxWidth", "IsHD", "Is4K"} {
			all.Del(k)
		}
		if totals, err = tallyEpisodesBySeries(ctx, req, client, all, "all episodes", &step); err != nil {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
	}
	list := make([]jf.MediaItem, 0, len(matching))
	for id, t := range matching {
		name := t.name
		totalEpisodes := len(t.episodes)
		if all := totals[id]; all != nil && all != t {
			if name == "" {
				name = all.name
			}
			totalEpisodes = max(len(all.episodes), len(t.episodes))
		}
		list = append(list, jf.MediaItem{ID: id, Name: name, Type: "Series", MatchingEpisodes: len(t.episodes), TotalEpisodes: totalEpisodes})
	}
	slices.SortFunc(list, func(a, b jf.MediaItem) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	total := len(list)
	start := min(max(startIndex, 0), total)
	end := min(start+limit, total)
	output := &jf.ItemListOutput{TotalCount: total, Shown: end - start, Items: list[start:end]}
	if end < total {
		output.NextStartIndex = &end
	}
	if total == 0 {
		output.Notes = noMatches()
	} else {
		output.Notes = notes
	}
	return nil, output, nil
}

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
