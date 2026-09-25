package tools

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAnalyticsTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_analytics ---
	if enabled("jellyfin_analytics", AnnotReadOnly) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_analytics",
			Title: "Analytics",
			InputSchema: jf.WithEnums[jf.AnalyticsInput](map[string][]any{
				"action": {"library_stats", "library_size", "size_report", "codec_report", "never_played", "recently_added", "duplicate_check", "played_status"},
			}),
			Description: "Library analytics and reports. All actions are read-only. Use parent_id to scope to a specific library.",
			Annotations: AnnotReadOnly,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.AnalyticsInput) (*mcp.CallToolResult, *jf.AnalyticsOutput, error) {
			userID, err := client.GetUserID(ctx)
			if err != nil {
				return jf.ErrResult("Jellyfin error: %v", err), nil, nil
			}

			switch args.Action {
			case "library_stats":
				types := []string{"Movie", "Series", "Episode", "Audio", "MusicAlbum", "MusicArtist", "MusicVideo", "Book", "BoxSet"}
				if args.Type != "" {
					types = []string{args.Type}
				}
				stats := make([]jf.TypeCount, 0, len(types))
				var unread []string
				for _, t := range types {
					params := url.Values{
						"UserId":           {userID},
						"IncludeItemTypes": {t},
						"Recursive":        {"true"},
						"Limit":            {"0"},
						// Missing episodes are placeholders a metadata provider
						// adds for episodes the library lacks.
						"IsMissing": {"false"},
					}
					if args.ParentID != "" {
						params.Set("ParentId", args.ParentID)
					}
					var result map[string]any
					if err := client.Get(ctx, "/Items", params, &result); err != nil {
						unread = append(unread, fmt.Sprintf("%s (%v)", t, err))
						continue
					}
					if count := jf.GetInt(result, "TotalRecordCount"); count > 0 {
						stats = append(stats, jf.TypeCount{Type: t, Count: count})
					}
				}
				out := &jf.AnalyticsOutput{Stats: &stats, UnreadTypes: unread}
				if len(unread) > 0 {
					out.Notes = []string{fmt.Sprintf("Counts could not be read for: %s. They are missing from stats, not zero.", strings.Join(unread, "; "))}
				}
				return nil, out, nil

			case "codec_report":
				itemType := args.Type
				if itemType == "" {
					itemType = "Movie"
				}
				params := url.Values{
					"UserId":           {userID},
					"IncludeItemTypes": {itemType},
					"Recursive":        {"true"},
					"Fields":           {"MediaSources"},
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				videoCodecs := make(map[string]int)
				audioCodecs := make(map[string]int)
				containers := make(map[string]int)
				resolutions := make(map[string]int)
				videoRanges := make(map[string]int)
				bitDepths := make(map[string]int)
				total := 0

				// Every item is read, a page at a time, so the counts cover the
				// whole library however large it is.
				_, err := jf.EachPage(ctx, client, "/Items", params, func(page int, items []map[string]any) {
					jf.ReportProgress(ctx, req, float64(page-1), 0, fmt.Sprintf("Scanning page %d...", page))
					for _, m := range items {
						sources := jf.ToSlice(m["MediaSources"])
						for _, src := range sources {
							sm := jf.ToMap(src)
							if sm == nil {
								continue
							}
							total++
							if c := jf.GetString(sm, "Container"); c != "" {
								containers[strings.ToLower(c)]++
							}
							for _, st := range jf.ToSlice(sm["MediaStreams"]) {
								stm := jf.ToMap(st)
								if stm == nil {
									continue
								}
								switch jf.GetString(stm, "Type") {
								case "Video":
									if codec := jf.GetString(stm, "Codec"); codec != "" {
										videoCodecs[strings.ToLower(codec)]++
									}
									if w := jf.GetInt(stm, "Width"); w > 0 {
										h := jf.GetInt(stm, "Height")
										var res string
										switch {
										case w >= 3840:
											res = "4K (2160p)"
										case w >= 1920:
											res = "1080p"
										case w >= 1280:
											res = "720p"
										case w >= 720:
											res = "480p"
										default:
											res = fmt.Sprintf("%dx%d", w, h)
										}
										resolutions[res]++
									}
									// HDR and bit depth
									if vr := jf.GetString(stm, "VideoRangeType"); vr != "" {
										videoRanges[vr]++
									} else if vr := jf.GetString(stm, "VideoRange"); vr != "" {
										videoRanges[vr]++
									}
									if bd := jf.GetInt(stm, "BitDepth"); bd > 0 {
										bitDepths[fmt.Sprintf("%d-bit", bd)]++
									}
								case "Audio":
									if codec := jf.GetString(stm, "Codec"); codec != "" {
										audioCodecs[strings.ToLower(codec)]++
									}
								}
							}
						}
					}
				})
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}

				return nil, &jf.AnalyticsOutput{CodecReport: &jf.CodecDistribution{
					TotalMediaSources: total,
					VideoCodecs:       videoCodecs,
					AudioCodecs:       audioCodecs,
					Containers:        containers,
					Resolutions:       resolutions,
					VideoRanges:       videoRanges,
					BitDepths:         bitDepths,
				}}, nil

			case "never_played":
				maxItems := jf.ClampInt(args.Limit, 500, jf.MaxLimitCap)
				params := url.Values{
					"UserId":    {userID},
					"IsPlayed":  {"false"},
					"IsMissing": {"false"},
					"Recursive": {"true"},
					"SortBy":    {"DateCreated"},
					"SortOrder": {"Descending"},
					"Fields":    {"Overview,DateCreated"},
				}
				if args.Type != "" {
					params.Set("IncludeItemTypes", args.Type)
				} else {
					params.Set("IncludeItemTypes", "Movie,Episode")
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				rawItems, total, err := jf.FetchAllPages(ctx, client, "/Items", params, maxItems)
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := jf.MediaItemsFrom(rawItems)
				shown := len(items)
				out := &jf.AnalyticsOutput{Items: &items, TotalCount: &total, Shown: &shown}
				if len(items) < total {
					if maxItems < jf.MaxLimitCap {
						out.Notes = []string{fmt.Sprintf("More results available. Increase limit (currently %d, at most %d) to see more.", maxItems, jf.MaxLimitCap)}
					} else {
						out.Notes = []string{fmt.Sprintf("More results available. The limit is at its maximum of %d, so narrow with type or parent_id, or page through them with jellyfin_browse using is_played=false and start_index.", jf.MaxLimitCap)}
					}
				}
				return nil, out, nil

			case "recently_added":
				days := args.Days
				if days <= 0 {
					days = 30
				}
				maxItems := jf.ClampInt(args.Limit, 500, jf.MaxLimitCap)
				params := url.Values{
					"UserId":    {userID},
					"Recursive": {"true"},
					"Fields":    {"Overview"},
				}
				if args.Type != "" {
					params.Set("IncludeItemTypes", args.Type)
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				scan, err := scanCreated(ctx, client, params, createdWindow{min: time.Now().AddDate(0, 0, -days)})
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				total := len(scan.items)
				items := scan.page(0, min(maxItems, total))
				shown := len(items)
				out := &jf.AnalyticsOutput{Items: &items, TotalCount: &total, TotalIsLowerBound: scan.capped, Shown: &shown, Days: days, UndatedCount: scan.undated}
				if len(items) < total {
					if maxItems < jf.MaxLimitCap {
						out.Notes = append(out.Notes, fmt.Sprintf("More results available. Increase limit (currently %d, at most %d) to see more.", maxItems, jf.MaxLimitCap))
					} else {
						// A smaller days window keeps the newest items, so it cannot reach the ones cut off here.
						out.Notes = append(out.Notes, fmt.Sprintf("More results available. The limit is at its maximum of %d, so narrow with type or parent_id, or page through them with jellyfin_browse using min_date_created and start_index.", jf.MaxLimitCap))
					}
				}
				out.Notes = append(out.Notes, scan.notes()...)
				return nil, out, nil

			case "duplicate_check":
				itemType := args.Type
				if itemType == "" {
					itemType = "Movie"
				}
				params := url.Values{
					"UserId":           {userID},
					"IncludeItemTypes": {itemType},
					"Recursive":        {"true"},
					"Fields":           {"Path"},
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				// Group by normalized name + year, over every item of the type.
				type itemInfo struct {
					ID   string
					Name string
					Year int
					Path string
				}
				groups := make(map[string][]itemInfo)
				_, err := jf.EachPage(ctx, client, "/Items", params, func(page int, items []map[string]any) {
					jf.ReportProgress(ctx, req, float64(page-1), 0, fmt.Sprintf("Scanning page %d...", page))
					for _, m := range items {
						name := jf.GetString(m, "Name")
						year := jf.GetInt(m, "ProductionYear")
						key := strings.ToLower(strings.TrimSpace(name))
						if year > 0 {
							key = fmt.Sprintf("%s (%d)", key, year)
						}
						groups[key] = append(groups[key], itemInfo{
							ID:   jf.GetString(m, "Id"),
							Name: name,
							Year: year,
							Path: jf.GetString(m, "Path"),
						})
					}
				})
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}

				// Groups with two or more copies are duplicates.
				duplicates := make([]jf.DuplicateGroup, 0)
				for _, items := range groups {
					if len(items) < 2 {
						continue
					}
					group := jf.DuplicateGroup{Name: items[0].Name, Year: items[0].Year, Count: len(items), Copies: make([]jf.DuplicateCopy, 0, len(items))}
					for _, it := range items {
						group.Copies = append(group.Copies, jf.DuplicateCopy{ID: it.ID, Name: it.Name, Year: it.Year, Path: it.Path})
					}
					duplicates = append(duplicates, group)
				}
				return nil, &jf.AnalyticsOutput{Duplicates: &duplicates}, nil

			case "library_size":
				types := []string{"Movie", "Episode", "Audio", "MusicVideo"}
				if args.Type != "" {
					types = []string{args.Type}
				}
				var totalBytes int64
				var unreadSizes []string
				typeSizes := make([]jf.TypeSize, 0)
				totalSteps := float64(len(types))
				for i, t := range types {
					jf.ReportProgress(ctx, req, float64(i), totalSteps, fmt.Sprintf("Calculating %s sizes...", t))
					// Fetch items in pages to aggregate sizes
					pageSize := 500
					startIndex := 0
					var typeBytes int64
					typeCount := 0
					for {
						params := url.Values{
							"UserId":           {userID},
							"IncludeItemTypes": {t},
							"Recursive":        {"true"},
							"Limit":            {fmt.Sprintf("%d", pageSize)},
							"StartIndex":       {fmt.Sprintf("%d", startIndex)},
							"Fields":           {"MediaSources"},
						}
						if args.ParentID != "" {
							params.Set("ParentId", args.ParentID)
						}
						var result map[string]any
						if err := client.Get(ctx, "/Items", params, &result); err != nil {
							unreadSizes = append(unreadSizes, fmt.Sprintf("%s (%v)", t, err))
							break
						}
						rawItems := jf.ToSlice(result["Items"])
						if len(rawItems) == 0 {
							break
						}
						for _, raw := range rawItems {
							m := jf.ToMap(raw)
							for _, src := range jf.ToSlice(m["MediaSources"]) {
								sm := jf.ToMap(src)
								if sz := jf.GetInt64(sm, "Size"); sz > 0 {
									typeBytes += sz
									typeCount++
								}
							}
						}
						totalRecords := jf.GetInt(result, "TotalRecordCount")
						startIndex += len(rawItems)
						if startIndex >= totalRecords {
							break
						}
					}
					if typeBytes > 0 {
						totalBytes += typeBytes
						typeSizes = append(typeSizes, jf.TypeSize{
							Type:   t,
							Files:  typeCount,
							SizeGB: fmt.Sprintf("%.2f", float64(typeBytes)/float64(jf.BytesPerGB)),
							SizeMB: typeBytes / jf.BytesPerMB,
						})
					}
				}
				jf.ReportProgress(ctx, req, totalSteps, totalSteps, "Size calculation complete")
				totalMB := totalBytes / jf.BytesPerMB
				out := &jf.AnalyticsOutput{
					TotalSizeGB: fmt.Sprintf("%.2f", float64(totalBytes)/float64(jf.BytesPerGB)),
					TotalSizeMB: &totalMB,
					ByType:      &typeSizes,
					UnreadTypes: unreadSizes,
				}
				if len(unreadSizes) > 0 {
					out.Notes = []string{fmt.Sprintf("Sizes could not be fully read for: %s. The totals leave out what was not read.", strings.Join(unreadSizes, "; "))}
				}
				return nil, out, nil

			case "played_status":
				// 1. Validate item_id
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for played_status"), nil, nil
				}

				// 2. Fetch all users
				var users []map[string]any
				if err := client.Get(ctx, "/Users", nil, &users); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}

				// 3. Fetch item metadata using the authenticated user
				var item map[string]any
				itemEndpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID))
				if err := client.Get(ctx, itemEndpoint, url.Values{"UserId": {userID}}, &item); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				itemName := jf.GetString(item, "Name")
				itemType := jf.GetString(item, "Type")

				// 4. If Series, get total episode count via Limit=0 query
				totalEpisodes := 0
				if itemType == "Series" {
					epParams := url.Values{
						"UserId":           {userID},
						"ParentId":         {args.ItemID},
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

				// 5. Query each non-disabled user's watch data
				userResults := make([]jf.PlayedStatusUser, 0, len(users))
				var unread []string
				for _, u := range users {
					if policy := jf.ToMap(u["Policy"]); policy != nil && jf.GetBool(policy, "IsDisabled") {
						continue
					}
					uid := jf.GetString(u, "Id")
					name := jf.GetString(u, "Name")

					var userItem map[string]any
					userEndpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID))
					if err := client.Get(ctx, userEndpoint, url.Values{"UserId": {uid}}, &userItem); err != nil {
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
						if itemType == "Series" && totalEpisodes > 0 {
							played := totalEpisodes - jf.GetInt(ud, "UnplayedItemCount")
							entry.EpisodesPlayed = &played
							entry.TotalEpisodes = totalEpisodes
						}
					}
					userResults = append(userResults, entry)
				}
				return nil, &jf.AnalyticsOutput{
					ItemSummary: &jf.PlayedStatusItem{
						ID:            args.ItemID,
						Name:          itemName,
						Type:          itemType,
						TotalEpisodes: totalEpisodes,
					},
					Users: &userResults,
					Notes: unreadUsersNote(unread),
				}, nil

			case "size_report":
				itemType := args.Type
				if itemType == "" {
					itemType = "Series"
				}
				limit := jf.ClampInt(args.Limit, 25, jf.MaxLimitCap)

				// For Series, fetch all Episodes and group by SeriesId
				fetchType := itemType
				if itemType == "Series" {
					fetchType = "Episode"
				}

				type sizeEntry struct {
					Name  string
					ID    string
					Type  string
					Bytes int64
					Files int
				}

				entries := make(map[string]*sizeEntry)
				params := url.Values{
					"UserId":           {userID},
					"IncludeItemTypes": {fetchType},
					"Recursive":        {"true"},
					"Fields":           {"MediaSources"},
				}
				if args.ParentID != "" {
					params.Set("ParentId", args.ParentID)
				}
				_, err := jf.EachPage(ctx, client, "/Items", params, func(page int, items []map[string]any) {
					jf.ReportProgress(ctx, req, float64(page-1), 0, fmt.Sprintf("Scanning %s page %d...", fetchType, page))
					for _, m := range items {
						var entryID, entryName, entryType string
						if itemType == "Series" {
							entryID = jf.GetString(m, "SeriesId")
							entryName = jf.GetString(m, "SeriesName")
							entryType = "Series"
							if entryID == "" {
								continue
							}
						} else {
							entryID = jf.GetString(m, "Id")
							entryName = jf.GetString(m, "Name")
							entryType = jf.GetString(m, "Type")
						}
						for _, src := range jf.ToSlice(m["MediaSources"]) {
							sm := jf.ToMap(src)
							if sz := jf.GetInt64(sm, "Size"); sz > 0 {
								e, ok := entries[entryID]
								if !ok {
									e = &sizeEntry{Name: entryName, ID: entryID, Type: entryType}
									entries[entryID] = e
								}
								e.Bytes += sz
								e.Files++
							}
						}
					}
				})
				if err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}

				// Sort descending by size
				sorted := make([]*sizeEntry, 0, len(entries))
				for _, e := range entries {
					sorted = append(sorted, e)
				}
				sort.Slice(sorted, func(i, j int) bool {
					return sorted[i].Bytes > sorted[j].Bytes
				})

				if len(sorted) > limit {
					sorted = sorted[:limit]
				}

				sizeEntries := make([]jf.SizeEntry, 0, len(sorted))
				for _, e := range sorted {
					sizeEntries = append(sizeEntries, jf.SizeEntry{
						Name:   e.Name,
						ID:     e.ID,
						Type:   e.Type,
						Files:  e.Files,
						SizeGB: fmt.Sprintf("%.2f", float64(e.Bytes)/float64(jf.BytesPerGB)),
						SizeMB: e.Bytes / jf.BytesPerMB,
					})
				}
				out := &jf.AnalyticsOutput{SizeReportType: itemType, SizeReport: &sizeEntries}
				out.Notes = moreNote(len(sizeEntries), len(entries), limit, "narrow with parent_id")
				if itemType == "Series" {
					out.Notes = append(out.Notes, "Each series' size is the sum of its episodes' file sizes.")
				}
				return nil, out, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: library_stats, codec_report, never_played, recently_added, duplicate_check, library_size, size_report, played_status", args.Action), nil, nil
			}
		})
	}
}

// unreadUsersNote names the users whose play state of an item could not be
// read, usually because the item is in a library they cannot see, so they are
// missing from the result rather than unplayed. It is nil when there are none.
func unreadUsersNote(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("The play state of %s could not be read, usually because the item is in a library they can't see, so %s left out.", strings.Join(names, ", "), plural(len(names), "that user is", "those users are"))}
}
