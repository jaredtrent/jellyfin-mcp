package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func fetchSeriesSeasons(ctx context.Context, client jf.Client, userID, seriesID, fields string) ([]any, error) {
	params := url.Values{
		"UserId":           {userID},
		"ParentId":         {seriesID},
		"IncludeItemTypes": {"Season"},
	}
	if fields != "" {
		params.Set("Fields", fields)
	}
	var result map[string]any
	err := client.Get(ctx, "/Items", params, &result)
	if err != nil && !jf.IsJellyfinID(seriesID) {
		var apiErr *jf.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 400 || apiErr.StatusCode == 404) {
			resolvedID, resErr := jf.ResolveSeriesID(ctx, client, userID, seriesID)
			if resErr != nil {
				return nil, resErr
			}
			if resolvedID != seriesID {
				params.Set("ParentId", resolvedID)
				result = nil
				err = client.Get(ctx, "/Items", params, &result)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return jf.ToSlice(result["Items"]), nil
}

func RegisterMediaTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_tv_shows ---
	if enabled("jellyfin_tv_shows", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_tv_shows",
			Title: "TV Shows",
			InputSchema: jf.WithEnums[jf.TVShowsInput](map[string][]any{
				"action": {"seasons", "episodes", "next_up"},
			}),
			OutputSchema: jf.SchemaFor[jf.TVShowsOutput](),
			Description:  "List seasons of a TV series, episodes in a season, or next-up unplayed episodes (use jellyfin_browse with type='Episode' for cross-season filtering).",
			Annotations:  AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.TVShowsInput) (*mcp.CallToolResult, *jf.TVShowsOutput, error) {
			userID, err := jf.EffectiveUserID(ctx, client, args.UserID)
			if err != nil {
				return jf.ResolveErrResult(err), nil, nil
			}

			switch args.Action {
			case "seasons":
				if args.SeriesID == "" {
					return jf.ErrResult("series_id is required for 'seasons' action. Use jellyfin_search with type 'Series' to find the series ID."), nil, nil
				}
				rawItems, err := fetchSeriesSeasons(ctx, client, userID, args.SeriesID, "ChildCount")
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				seasons := make([]jf.SeasonInfo, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					seasons = append(seasons, jf.SeasonInfo{
						ID:           jf.GetString(m, "Id"),
						Name:         jf.GetString(m, "Name"),
						SeasonNumber: jf.GetInt(m, "IndexNumber"),
						EpisodeCount: jf.GetInt(m, "ChildCount"),
					})
				}
				return nil, &jf.TVShowsOutput{Seasons: &seasons}, nil

			case "episodes":
				if args.SeriesID == "" {
					return jf.ErrResult("series_id is required for 'episodes' action."), nil, nil
				}
				if args.SeasonNumber == nil {
					return jf.ErrResult("season_number is required for 'episodes' action (such as 1 for Season 1)."), nil, nil
				}

				jf.ReportProgress(ctx, req, 0, 2, "Finding season...")

				seasonItems, err := fetchSeriesSeasons(ctx, client, userID, args.SeriesID, "")
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				var seasonID string
				for _, s := range seasonItems {
					m := jf.ToMap(s)
					if jf.GetInt(m, "IndexNumber") == *args.SeasonNumber {
						seasonID = jf.GetString(m, "Id")
						break
					}
				}
				if seasonID == "" {
					return jf.ErrResult("Season %d not found for this series. Use action 'seasons' to list its season numbers.", *args.SeasonNumber), nil, nil
				}

				jf.ReportProgress(ctx, req, 1, 2, "Fetching episodes...")

				params := url.Values{
					"UserId":           {userID},
					"ParentId":         {seasonID},
					"IncludeItemTypes": {"Episode"},
					"Fields":           {"Overview"},
				}
				var result map[string]any
				if err := client.Get(ctx, "/Items", params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				rawItems := jf.ToSlice(result["Items"])
				episodes := make([]jf.EpisodeInfo, 0, len(rawItems))
				for _, raw := range rawItems {
					m := jf.ToMap(raw)
					item := jf.MediaItemFrom(m)
					episodes = append(episodes, jf.EpisodeInfo{
						ID:              item.ID,
						Name:            item.Name,
						SeasonNumber:    *args.SeasonNumber,
						EpisodeNumber:   item.IndexNumber,
						Overview:        jf.Truncate(item.Overview, jf.SummaryMaxLen),
						CommunityRating: item.CommunityRating,
						RuntimeMinutes:  item.RuntimeMinutes,
						Played:          item.Played,
						Progress:        item.Progress,
						PremiereDate:    jf.Truncate(jf.GetString(m, "PremiereDate"), jf.DateOnlyLen),
					})
				}
				return nil, &jf.TVShowsOutput{Episodes: &episodes}, nil

			case "next_up":
				maxItems := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview"},
				}
				if args.SeriesID != "" {
					resolvedSeries, err := jf.ResolveSeriesID(ctx, client, userID, args.SeriesID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					params.Set("SeriesId", resolvedSeries)
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Shows/NextUp", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				return nil, &jf.TVShowsOutput{NextUp: &items, Notes: moreNote(len(items), total, maxItems, "pass a series_id")}, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: seasons, episodes, next_up", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_music ---
	if enabled("jellyfin_music", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_music",
			Title: "Music",
			InputSchema: jf.WithEnums[jf.MusicInput](map[string][]any{
				"action": {"artists", "album_artists", "genres", "instant_mix"},
			}),
			Description: "Browse music artists, album artists, or music genres, or generate an instant mix playlist from a seed item.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.MusicInput) (*mcp.CallToolResult, any, error) {
			limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			switch args.Action {
			case "artists", "album_artists":
				maxItems := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
				endpoint := "/Artists"
				if args.Action == "album_artists" {
					endpoint = "/Artists/AlbumArtists"
				}
				params := url.Values{
					"UserId": {userID},
					"Fields": {"Overview,Genres"},
				}
				if args.Query != "" {
					params.Set("SearchTerm", args.Query)
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, endpoint, params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				msg := fmt.Sprintf("Found %d artists", len(items))
				if total > len(items) {
					msg += fmt.Sprintf(" (of %d total)", total)
				}
				return jf.TextResult(fmt.Sprintf("%s:\n\n%s", msg, jf.FormatJSON(items))), nil, nil

			case "genres":
				return handleMusicGenres(ctx, client, userID, args)

			case "instant_mix":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for instant_mix. Provide an album, artist, song, or playlist ID."), nil, nil
				}
				params := url.Values{
					"Limit":  {fmt.Sprintf("%d", limit)},
					"UserId": {userID},
				}
				endpoint := fmt.Sprintf("/Items/%s/InstantMix", jf.SanitizeID(args.ItemID))
				var result map[string]any
				if err := client.Get(ctx, endpoint, params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.ItemsFrom(result)
				return jf.TextResult(fmt.Sprintf("Instant mix (%d tracks):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: artists, album_artists, genres, instant_mix", args.Action), nil, nil
			}
		})
	}
}

func handleMusicGenres(ctx context.Context, client jf.Client, userID string, args jf.MusicInput) (*mcp.CallToolResult, any, error) {
	maxItems := jf.ClampInt(args.Limit, 50, jf.MaxLimitCap)
	var views map[string]any
	if err := client.Get(ctx, "/UserViews", url.Values{"UserId": {userID}}, &views); err != nil {
		return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
	}
	var libraryIDs []string
	for _, raw := range jf.ToSlice(views["Items"]) {
		view := jf.ToMap(raw)
		switch jf.GetString(view, "CollectionType") {
		case "music", "musicvideos":
			if id := jf.GetString(view, "Id"); id != "" {
				libraryIDs = append(libraryIDs, id)
			}
		}
	}
	if len(libraryIDs) == 0 {
		return jf.TextResult("No music or music-video library is available to this user, so there are no music genres to list."), nil, nil
	}

	seen := make(map[string]bool)
	var genres []map[string]any
	cut := false
	for _, libraryID := range libraryIDs {
		params := url.Values{
			"UserId":   {userID},
			"ParentId": {libraryID},
		}
		if args.Query != "" {
			params.Set("SearchTerm", args.Query)
		}
		rawItems, total, err := jf.FetchAllPages(ctx, client, "/Genres", params, maxItems)
		if err != nil {
			return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
		}
		cut = cut || total > len(rawItems)
		for _, raw := range rawItems {
			m := jf.ToMap(raw)
			if m == nil {
				continue
			}
			key := jf.GetString(m, "Id")
			if key == "" {
				key = "name:" + strings.ToLower(jf.GetString(m, "Name"))
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			genres = append(genres, m)
		}
	}
	sort.SliceStable(genres, func(i, j int) bool {
		a, b := jf.GetString(genres[i], "Name"), jf.GetString(genres[j], "Name")
		if la, lb := strings.ToLower(a), strings.ToLower(b); la != lb {
			return la < lb
		}
		return a < b
	})
	if len(genres) > maxItems {
		genres = genres[:maxItems]
		cut = true
	}
	items := make([]jf.MediaItem, 0, len(genres))
	for _, g := range genres {
		items = append(items, jf.MediaItemFrom(g))
	}
	note := ""
	if cut {
		note = " More genres match than are shown; narrow with query"
		if maxItems < jf.MaxLimitCap {
			note += fmt.Sprintf(", or increase limit (currently %d, at most %d)", maxItems, jf.MaxLimitCap)
		}
		note += "."
	}
	return jf.TextResult(fmt.Sprintf("Music genres (%d):%s\n\n%s", len(items), note, jf.FormatJSON(items))), nil, nil
}
