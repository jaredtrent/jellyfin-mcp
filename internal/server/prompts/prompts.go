package prompts

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterPrompts(server *mcp.Server, _ jf.Client) {

	// --- find-and-play ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "find-and-play",
		Title:       "Find and Play",
		Description: "Search for media by name and start playback on a connected client",
		Arguments: []*mcp.PromptArgument{
			{Name: "query", Description: "What to search for, such as a title, artist, or album", Required: true},
			{Name: "type", Description: "Media type filter: Movie, Series, Episode, Audio, MusicAlbum"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := req.Params.Arguments["query"]
		mediaType := req.Params.Arguments["type"]

		instruction := fmt.Sprintf(
			"Help me find and play \"%s\". Follow these steps:\n"+
				"1. Use jellyfin_search with query=\"%s\"",
			query, query)
		if mediaType != "" {
			instruction += fmt.Sprintf(" and type=\"%s\"", mediaType)
		}
		instruction += "\n2. Show me the results and let me pick one" +
			"\n3. Use jellyfin_sessions action=\"list\" to find an active session" +
			"\n4. If no sessions are active, tell me I need to open a Jellyfin client (web, mobile, or TV app) first" +
			"\n5. Use jellyfin_play with the chosen item and session"

		return &mcp.GetPromptResult{
			Description: "Find and play media",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- resume-watching ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "resume-watching",
		Title:       "Resume Watching",
		Description: "Pick up where you left off: in-progress movies, episodes, and audio with resume positions",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		instruction := "Show me what I can continue watching or listening to. Follow these steps:\n" +
			"1. Gather data in parallel:\n" +
			"   - jellyfin_sessions action=\"resume\" to get items with saved playback positions\n" +
			"   - jellyfin_recommendations action=\"next_up\" to find the next episode in series I'm following\n" +
			"2. Present a combined \"pick up where you left off\" list organized by type (movies, TV, audio), " +
			"showing the title and the progress percentage or time remaining for each item"

		return &mcp.GetPromptResult{
			Description: "Resume in-progress media",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- whats-new ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "whats-new",
		Title:       "What's New",
		Description: "A personalized viewing guide of recently added media and next episodes to watch",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to scope results, such as Movies or Shows"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]

		instruction := "Show me what's new and what I should watch next. Follow these steps:\n"
		if library != "" {
			instruction += fmt.Sprintf(
				"1. Use jellyfin_libraries to find the library ID for \"%s\"\n"+
					"2. Gather data in parallel:\n"+
					"   - jellyfin_recommendations action=\"latest\" with that parent_id\n"+
					"   - jellyfin_recommendations action=\"next_up\" to see next episodes to watch\n"+
					"   - jellyfin_sessions action=\"resume\" to find in-progress items\n", library)
			instruction += "3. Present a combined viewing guide: new additions, next episodes, and items to resume, " +
				"organized by category with ratings and brief descriptions"
		} else {
			instruction += "1. Gather data in parallel:\n" +
				"   - jellyfin_recommendations action=\"latest\" to see recently added items\n" +
				"   - jellyfin_recommendations action=\"next_up\" to see next episodes to watch\n" +
				"   - jellyfin_sessions action=\"resume\" to find in-progress items\n"
			instruction += "2. Present a combined viewing guide: new additions, next episodes, and items to resume, " +
				"organized by category with ratings and brief descriptions"
		}

		return &mcp.GetPromptResult{
			Description: "Latest additions and next episodes",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- movie-night ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "movie-night",
		Title:       "Movie Night",
		Description: "Curated movie suggestions from your library, filtered by genre and rating",
		Arguments: []*mcp.PromptArgument{
			{Name: "genre", Description: "Preferred genre, such as Action, Comedy, Drama, Horror, Sci-Fi, or Thriller"},
			{Name: "mood", Description: "Viewing mood: relaxing, exciting, thought-provoking, funny, scary"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		genre := req.Params.Arguments["genre"]
		mood := req.Params.Arguments["mood"]

		instruction := "Help me pick a movie for tonight. Follow these steps:\n"

		if genre != "" {
			instruction += fmt.Sprintf(
				"1. Use jellyfin_browse with type=\"Movie\", genre=\"%s\", is_played=false, "+
					"sort_by=\"CommunityRating\", sort_order=\"Descending\", limit=10\n", genre)
		} else {
			instruction += "1. Use jellyfin_browse with type=\"Movie\", is_played=false, " +
				"sort_by=\"CommunityRating\", sort_order=\"Descending\", limit=10\n"
		}

		instruction += "2. Use jellyfin_recommendations action=\"movie_recs\" for personalized picks\n" +
			"3. Present the top 5 suggestions with ratings, year, runtime, and a brief overview"

		if mood != "" {
			instruction += fmt.Sprintf(
				"\n4. When ranking results, prioritize movies that match a \"%s\" mood based on their genre and overview. "+
					"For example, \"relaxing\" favors light dramas and comedies, \"exciting\" favors action and thrillers, "+
					"\"scary\" favors horror and suspense", mood)
		}

		return &mcp.GetPromptResult{
			Description: "Movie night suggestions",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- music-listen ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "music-listen",
		Title:       "Listen to Music",
		Description: "Find and play music: search by artist, album, or song, and optionally generate a smart mix",
		Arguments: []*mcp.PromptArgument{
			{Name: "query", Description: "Artist name, album title, or song name", Required: true},
			{Name: "type", Description: "What to search for: artist, album, song, genre, playlist"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := req.Params.Arguments["query"]
		searchType := req.Params.Arguments["type"]

		// Map friendly type names to Jellyfin item types
		jellyfinType := ""
		switch searchType {
		case "artist":
			jellyfinType = "MusicArtist"
		case "album":
			jellyfinType = "MusicAlbum"
		case "song":
			jellyfinType = "Audio"
		case "playlist":
			jellyfinType = "Playlist"
		}

		instruction := fmt.Sprintf("Help me find and play music: \"%s\". Follow these steps:\n", query)

		if searchType == "genre" {
			instruction += fmt.Sprintf(
				"1. Use jellyfin_browse with type=\"Audio\", genre=\"%s\", sort_by=\"Random\", limit=20\n", query)
		} else {
			instruction += fmt.Sprintf("1. Use jellyfin_search with query=\"%s\"", query)
			if jellyfinType != "" {
				instruction += fmt.Sprintf(", type=\"%s\"", jellyfinType)
			} else {
				instruction += ", type=\"MusicAlbum\"; if no album matches, also try type=\"MusicArtist\""
			}
			instruction += "\n"
		}

		instruction += "2. Show me the results and let me pick one\n" +
			"3. If I pick an artist or album, offer to generate a smart mix using " +
			"jellyfin_music action=\"instant_mix\" with that item's ID for a playlist of similar tracks\n" +
			"4. Use jellyfin_sessions action=\"list\" to find an active session\n" +
			"5. Use jellyfin_play with the chosen item(s) and session"

		return &mcp.GetPromptResult{
			Description: "Find and play music",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- binge-watch ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "binge-watch",
		Title:       "Binge-Watch a Series",
		Description: "Check progress on a TV series and queue up the next episodes to watch",
		Arguments: []*mcp.PromptArgument{
			{Name: "query", Description: "TV series name", Required: true},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := req.Params.Arguments["query"]

		instruction := fmt.Sprintf("Help me set up a binge session for \"%s\". Follow these steps:\n", query) +
			fmt.Sprintf("1. Use jellyfin_search with query=\"%s\", type=\"Series\" to find the show\n", query) +
			"2. Use jellyfin_tv_shows action=\"seasons\" with the series_id to list the seasons and each season's episode_count\n" +
			"3. Use jellyfin_tv_shows action=\"next_up\" with the series_id to find where I left off. The returned episode's parent_index_number is its season number and its index_number is its episode number. " +
			"If next_up returns nothing, ask whether I am starting the series or have finished it\n" +
			"4. Show my progress: where I am now, and how many episodes remain from the next-up episode to the end of the series, counted from the episode_count of the current and later seasons\n" +
			"5. Offer to queue the next 3-5 episodes. Use jellyfin_tv_shows action=\"episodes\" with the series_id and season_number set to the next-up episode's season, " +
			"and take the next-up episode and the ones after it by episode_number, continuing into the next season if needed. " +
			"Then use jellyfin_sessions action=\"list\" to pick a session and jellyfin_play with play_command=\"PlayNow\" and those episode IDs in order"

		return &mcp.GetPromptResult{
			Description: "TV binge session setup",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- fix-subtitles ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "fix-subtitles",
		Title:       "Fix Subtitles",
		Description: "Search for and download missing subtitles for a movie or episode",
		Arguments: []*mcp.PromptArgument{
			{Name: "query", Description: "Movie or episode name to find subtitles for", Required: true},
			{Name: "language", Description: "Subtitle language code: en, es, fr, de, ja, pt, it, zh, ko, ru (default: en)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := req.Params.Arguments["query"]
		language := req.Params.Arguments["language"]
		if language == "" {
			language = "en"
		}

		instruction := fmt.Sprintf("Help me find subtitles for \"%s\". Follow these steps:\n", query) +
			fmt.Sprintf("1. Use jellyfin_search with query=\"%s\" to find the item (try type=\"Movie\", then type=\"Episode\" if no match)\n", query) +
			fmt.Sprintf("2. Use jellyfin_subtitles_lyrics action=\"search_subtitles\" with the item_id and language=\"%s\"\n", language) +
			"3. Show me the available subtitles with their provider, format, language, and download count\n" +
			"4. Let me pick one, then use jellyfin_subtitles_lyrics action=\"download_subtitle\" with subtitle_id set to the chosen result's id"

		return &mcp.GetPromptResult{
			Description: "Find and download subtitles",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- who-is-watching ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "who-is-watching",
		Title:       "Who Is Watching",
		Description: "Dashboard of all active playback sessions: who's watching what, on which device",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		instruction := "Show me what's happening on the Jellyfin server right now. Follow these steps:\n" +
			"1. Use jellyfin_sessions action=\"list\" to get all active sessions\n" +
			"2. Present a dashboard for each session showing: user name, device/client name, " +
			"what's playing (title, type, season/episode for TV), playback state (playing/paused), " +
			"and progress (current position / total duration)\n" +
			"3. If no sessions are active, say so and suggest checking jellyfin_devices action=\"list\" " +
			"to see recently connected devices"

		return &mcp.GetPromptResult{
			Description: "Active session dashboard",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- troubleshoot ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "troubleshoot",
		Title:       "Troubleshoot the Server",
		Description: "Diagnose a server issue: check server logs for errors, failed tasks, plugin status, and system status",
		Arguments: []*mcp.PromptArgument{
			{Name: "issue", Description: "Description of the problem you're experiencing"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		issue := req.Params.Arguments["issue"]

		instruction := "Diagnose the Jellyfin server. Follow these steps:\n" +
			"1. Use jellyfin_system_info action=\"ping\" to check if the server is responsive\n" +
			"2. After ping succeeds, gather diagnostic data in parallel:\n" +
			"   - jellyfin_system_info action=\"info\" for server version and pending restart\n" +
			"   - jellyfin_tasks action=\"list\" to check for failed or stuck tasks\n" +
			"   - jellyfin_plugins action=\"list\" for each plugin's status: Malfunctioned or NotSupported means the plugin failed to load, and Restart or Superseded means a server restart is pending\n" +
			"   - jellyfin_system_info action=\"logs\" to find the most recent server log (named log_*.log, not FFmpeg logs)\n" +
			"   - jellyfin_system_info action=\"activity_log\" severity=\"Error,Warning\" for recent failures such as failed logins, tasks, or plugin updates\n" +
			"3. Use jellyfin_system_info action=\"log_file\" with the most recent server log and severity=\"warn+error\"; each entry includes its exception message. To look at a stretch of time, such as when the problem started, add min_date and max_date\n"

		if issue != "" {
			// Every group renders under its symptom, and the model picks the
			// groups that fit: an issue can fit several, and its wording
			// rarely names the subsystem that fails.
			instruction += fmt.Sprintf(
				"4. The user reported this issue: \"%s\". Run the checks below that fit it; an issue can fit more than one group:\n", issue) +
				"   - A title that won't play, stops, stutters, buffers, or transcodes:\n" +
				"     - jellyfin_sessions action=\"list\" to check active playback sessions\n" +
				"     - If the issue names a title, find it with jellyfin_search, then jellyfin_item_extras action=\"playback_info\" with its item_id to see whether it direct plays or transcodes and why\n" +
				"     - If the issue names a person, find their user_id with jellyfin_users action=\"list\", then jellyfin_system_info action=\"activity_log\" with that user_id (and the item_id, if known) for their playback starts, stops, and failures\n" +
				"     - jellyfin_system_info action=\"logs\" lists the FFmpeg logs (named FFmpeg.*.log), one per transcode; read the newest with action=\"log_file\" for the ffmpeg error\n" +
				"     - jellyfin_analytics action=\"codec_report\" to identify transcoding-heavy codecs\n" +
				"     - Reference jellyfin://guides/transcoding for hardware transcoding setup\n" +
				"   - A client that can't connect, or a server that can't be reached from outside the home network:\n" +
				"     - jellyfin_server action=\"get_config_section\" key=\"network\" for the base URL, ports, and remote access settings\n" +
				"     - Reference jellyfin://guides/remote-access for reverse proxy and port forwarding setup\n" +
				"   - Items missing from a library, a scan that doesn't finish, or wrong metadata:\n" +
				"     - jellyfin_tasks action=\"list\" to check library scan status\n" +
				"     - jellyfin_libraries to verify library configuration\n" +
				"     - Reference jellyfin://guides/library-setup for library organization\n" +
				"   - A plugin that fails or misbehaves:\n" +
				"     - jellyfin_plugins action=\"list\" to check installed plugins and versions\n" +
				"     - Reference jellyfin://guides/plugins for plugin troubleshooting\n" +
				"   - An issue that fits none of these:\n" +
				"     - jellyfin_system_info action=\"storage\" to check disk space\n" +
				"     - jellyfin_server action=\"list_backups\" for backup context\n" +
				"     - Correlate error messages from logs with the reported symptom\n"
			instruction += "5. Summarize: root cause (if identifiable), affected components, and suggested fix\n" +
				"6. Reference jellyfin://guides/troubleshooting for common solutions if applicable\n"
		} else {
			instruction += "4. Also gather:\n" +
				"   - jellyfin_system_info action=\"storage\" to check disk space\n" +
				"   - jellyfin_server action=\"list_backups\" for backup context\n" +
				"5. Summarize: any issues found, affected components, and suggested fixes\n" +
				"6. Reference jellyfin://guides/troubleshooting for common solutions if applicable\n"
		}

		return &mcp.GetPromptResult{
			Description: "Troubleshoot server issues",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- bulk-metadata-fix ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "bulk-metadata-fix",
		Title:       "Bulk Metadata Fix",
		Description: "Find and fix metadata issues across a library: missing overviews, wrong years, missing genres, or re-identify items",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to scan, such as Movies or Shows", Required: true},
			{Name: "issue", Description: "Issue type: missing_overview, wrong_year, missing_genres, wrong_title, re_identify", Required: true},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]
		issue := req.Params.Arguments["issue"]

		// batch_update writes one value to every listed item, so values that
		// differ per item go through apply or update on one item at a time.
		perItemFix := func(field string) string {
			return "After I confirm, fix each item on its own: use jellyfin_metadata action=\"apply\" on that item with provider_name set to a key from the chosen match's provider_ids (such as Tmdb) and provider_id set to its value, " +
				"or action=\"update\" with that item's item_id and " + field + ". Never use batch_update for these values, because it writes the same value to every item\n"
		}

		instruction := fmt.Sprintf("Help me fix metadata issues in the \"%s\" library. Issue type: %s\n\n", library, issue)
		instruction += fmt.Sprintf("1. Use jellyfin_libraries to find the library ID for \"%s\"\n", library)

		switch issue {
		case "missing_overview":
			instruction += "2. Use jellyfin_browse with the library parent_id, sort_by=\"SortName\" to list items (paginate with start_index to cover the full library)\n" +
				"3. Browse results include overview when present, so items WITHOUT an 'overview' field in the results genuinely lack one. No need to call jellyfin_get_item to confirm.\n" +
				"4. For each item with a missing overview, use jellyfin_metadata action=\"search\" with search_type set to the item's type (such as Movie or Series) and search_query set to its name to find the correct match\n" +
				"5. Present the items and proposed fixes. " + perItemFix("its overview")
		case "wrong_year":
			instruction += "2. Use jellyfin_browse with the library parent_id to list items\n" +
				"3. Identify items where the year looks wrong (compare with known release years)\n" +
				"4. For each wrong item, use jellyfin_metadata action=\"search\" with search_type set to the item's type, search_query set to its name, and search_year set to the correct year to find the right match\n" +
				"5. Present the corrections. " + perItemFix("its production_year")
		case "missing_genres":
			instruction += "2. Use jellyfin_browse with the library parent_id to list items\n" +
				"3. Browse results do NOT include genres, so use jellyfin_get_item on each item to check if genres are empty\n" +
				"4. For each item missing genres, use jellyfin_metadata action=\"search\" with search_type set to the item's type and search_query set to its name to find the correct match\n" +
				"5. Present the fixes. The genres field replaces the item's whole genre list, as tags and studios replace theirs, so include every genre the item should keep. " + perItemFix("its genres")
		case "wrong_title", "re_identify":
			instruction += "2. Use jellyfin_browse with the library parent_id to list items\n" +
				"3. For each misidentified item, use jellyfin_metadata action=\"search\" with search_query set to the correct title\n" +
				"4. Show the search results and let the user pick the correct match\n" +
				"5. Use jellyfin_metadata action=\"apply\" with provider_name set to a key from the chosen result's provider_ids (such as Tmdb) and provider_id set to its value\n"
		default:
			instruction += "2. Use jellyfin_browse with the library parent_id to scan items\n" +
				"3. Identify any metadata problems\n" +
				"4. Present findings and offer to fix using jellyfin_metadata\n"
		}

		return &mcp.GetPromptResult{
			Description: "Bulk metadata fix",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- subtitle-audit ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "subtitle-audit",
		Title:       "Subtitle Audit",
		Description: "Audit a library for missing subtitles and batch-download them",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to audit, such as Movies or Shows", Required: true},
			{Name: "language", Description: "Subtitle language code (default: en)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]
		language := req.Params.Arguments["language"]
		if language == "" {
			language = "en"
		}

		instruction := fmt.Sprintf("Audit the \"%s\" library for missing subtitles (language: %s). Follow these steps:\n", library, language) +
			fmt.Sprintf("1. Use jellyfin_libraries to find the library ID for \"%s\"\n", library) +
			"2. Use jellyfin_browse with parent_id and has_subtitles=false to find items without subtitles. This finds items with no subtitles at all, so items that have subtitles only in other languages are not included; say so in the report\n" +
			"3. Report the count of items missing subtitles\n" +
			"4. Ask if I want to batch-download subtitles for these items\n" +
			fmt.Sprintf("5. If I want them, use jellyfin_subtitles_lyrics action=\"batch_download_subtitles\" with the item_ids and language=\"%s\", in batches of at most 25 item_ids per call (the tool asks me to confirm)\n", language) +
			"6. Present the results showing which items got subtitles and which failed"

		return &mcp.GetPromptResult{
			Description: "Subtitle audit and download",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- library-report ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "library-report",
		Title:       "Library Report",
		Description: "Comprehensive library analytics report: stats, codecs, unplayed items, recent additions, and duplicates",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to report on (optional, all libraries if omitted)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]

		instruction := "Generate a comprehensive library analytics report. Follow these steps:\n"
		if library != "" {
			instruction += fmt.Sprintf("1. Use jellyfin_libraries to find the library ID for \"%s\"\n", library)
			instruction += "2. Gather all analytics data in parallel using the parent_id:\n"
			instruction += "   - jellyfin_analytics action=\"library_stats\" for item counts by type\n"
			instruction += "   - jellyfin_analytics action=\"codec_report\" for codec/resolution distribution\n"
			instruction += "   - jellyfin_analytics action=\"never_played\" for unplayed items\n"
			instruction += "   - jellyfin_analytics action=\"recently_added\" for recent additions\n"
			instruction += "   - jellyfin_analytics action=\"duplicate_check\" for potential duplicates\n"
		} else {
			instruction += "1. Gather all analytics data in parallel:\n"
			instruction += "   - jellyfin_analytics action=\"library_stats\" for item counts by type\n"
			instruction += "   - jellyfin_analytics action=\"codec_report\" for codec/resolution distribution\n"
			instruction += "   - jellyfin_analytics action=\"never_played\" for unplayed items\n"
			instruction += "   - jellyfin_analytics action=\"recently_added\" for recent additions\n"
			instruction += "   - jellyfin_analytics action=\"duplicate_check\" for potential duplicates\n"
		}
		instruction += "\nPresent a formatted summary report with:\n" +
			"- Item counts by content type\n" +
			"- Top codecs, resolutions, and containers\n" +
			"- Number of never-played items\n" +
			"- Recent additions summary\n" +
			"- Any duplicate items found\n" +
			"- Recommendations, such as codecs that force transcoding and items to clean up"

		return &mcp.GetPromptResult{
			Description: "Library analytics report",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- duplicate-finder ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "duplicate-finder",
		Title:       "Find Duplicates",
		Description: "Find duplicate media items in a library and optionally remove inferior copies",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to scan, such as Movies", Required: true},
			{Name: "type", Description: "Item type to check: Movie or Series (default: Movie)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]
		itemType := req.Params.Arguments["type"]
		if itemType == "" {
			itemType = "Movie"
		}

		instruction := fmt.Sprintf("Find duplicate %s items in the \"%s\" library. Follow these steps:\n", itemType, library) +
			fmt.Sprintf("1. Use jellyfin_libraries to find the library ID for \"%s\"\n", library) +
			fmt.Sprintf("2. Use jellyfin_analytics action=\"duplicate_check\" type=\"%s\" with the parent_id\n", itemType) +
			"3. For each duplicate group, use jellyfin_get_item for each copy to compare quality (resolution, bitrate, codec, file size)\n" +
			"4. Present a comparison table for each duplicate group showing:\n" +
			"   - Resolution, codec, bitrate, file size, file path\n" +
			"   - Which copy is recommended to keep (higher quality)\n" +
			"5. Before offering any deletion, confirm that the copies in each group are the same title and differ only in file path, because duplicate_check groups items by name and year only. " +
			"Leave out any group that fails this check\n" +
			"6. Ask if I want to delete any inferior copies\n" +
			"7. For each copy I choose to remove, use jellyfin_library_manage action=\"delete_item\" (the tool asks me to confirm each deletion)"

		return &mcp.GetPromptResult{
			Description: "Find and remove duplicate media",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- watch-history ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "watch-history",
		Title:       "Watch History",
		Description: "View a user's watch history over a time period: what was played and when",
		Arguments: []*mcp.PromptArgument{
			{Name: "user", Description: "Username to check history for (optional, defaults to current user)"},
			{Name: "days", Description: "Number of days to look back (default: 30)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		user := req.Params.Arguments["user"]
		days := req.Params.Arguments["days"]
		if days == "" {
			days = "30"
		}

		instruction := "Show watch history. Follow these steps:\n"
		step := 1
		forUser := ""
		if user != "" {
			instruction += fmt.Sprintf("1. Use jellyfin_users action=\"list\" to find the user ID for \"%s\"\n", user)
			step = 2
			forUser = " with that user_id and"
		}
		instruction += fmt.Sprintf("%d. Use jellyfin_system_info action=\"playback_history\"%s limit=200. "+
			"It lists items with verified playback, newest first, and each item appears once with its last_played time and play_count\n", step, forUser)
		instruction += fmt.Sprintf("%d. Keep the items whose last_played date falls within the last %s days\n", step+1, days) +
			fmt.Sprintf("%d. Present a chronological watch history showing:\n", step+2) +
			"   - When it was last played\n" +
			"   - What was played (title, type, and series, season, and episode for TV)\n" +
			fmt.Sprintf("%d. Summarize: total items watched and the average per day", step+3)

		return &mcp.GetPromptResult{
			Description: "Watch history report",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- codec-optimize ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "codec-optimize",
		Title:       "Codec Optimization",
		Description: "Analyze media codecs and optimize transcoding settings to reduce server load",
		Arguments: []*mcp.PromptArgument{
			{Name: "library", Description: "Library name to analyze (optional, all libraries if omitted)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		library := req.Params.Arguments["library"]

		instruction := "Analyze media codecs and optimize transcoding settings. Follow these steps:\n"
		if library != "" {
			instruction += fmt.Sprintf("1. Use jellyfin_libraries to find the library ID for \"%s\"\n", library)
			instruction += "2. Gather data in parallel using the parent_id:\n"
		} else {
			instruction += "1. Gather data in parallel:\n"
		}
		instruction += "   - jellyfin_analytics action=\"codec_report\" for codec/resolution distribution\n" +
			"   - jellyfin_server action=\"get_config_section\" key=\"encoding\" for current transcoding settings\n" +
			"   - jellyfin_system_info action=\"info\" for the server version. It does not report hardware, so ask me which CPU or GPU the server uses\n"
		step := 3
		if library != "" {
			step = 4
		}
		instruction += fmt.Sprintf("%d. Analyze the codec distribution and identify:\n", step) +
			"   - Codecs that require transcoding on most clients, such as HEVC on older devices\n" +
			"   - Resolution distribution and bandwidth implications\n" +
			"   - Audio codecs that may need transcoding\n"
		instruction += fmt.Sprintf("%d. Compare with current hardware transcoding settings and suggest optimizations:\n", step+1) +
			"   - Recommend a hardware acceleration method based on the CPU or GPU I describe\n" +
			"   - Suggest enabling/disabling specific codec support\n" +
			"   - Reference jellyfin://guides/transcoding for setup instructions\n"
		instruction += fmt.Sprintf("%d. If changes are needed, offer to apply them with jellyfin_server action=\"update_config_section\" key=\"encoding\"", step+2)

		return &mcp.GetPromptResult{
			Description: "Codec analysis and transcoding optimization",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- parental-controls ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "parental-controls",
		Title:       "Parental Controls",
		Description: "Set up kid-safe access: create a restricted user account with library access controls, and point to the dashboard for rating limits",
		Arguments: []*mcp.PromptArgument{
			{Name: "username", Description: "Username for the child account", Required: true},
			{Name: "max_rating", Description: "Maximum content rating you plan to allow, such as G, PG, PG-13, TV-Y, TV-G, TV-PG, or TV-14 (default: PG). The tools cannot set it; the prompt tells you where to set it in the dashboard"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		username := req.Params.Arguments["username"]
		maxRating := req.Params.Arguments["max_rating"]
		if maxRating == "" {
			maxRating = "PG"
		}

		instruction := fmt.Sprintf("Set up parental controls for a child account \"%s\". Follow these steps:\n", username) +
			"1. Use jellyfin_users action=\"list\" to check if the user already exists\n" +
			fmt.Sprintf("2. If not, use jellyfin_users action=\"create\" username=\"%s\" with a password\n", username) +
			"3. Use jellyfin_libraries to list available libraries and their IDs\n" +
			"4. Ask which libraries the child should have access to (suggest excluding adult-oriented libraries)\n" +
			"5. Use jellyfin_users action=\"update_policy\" with the user_id and:\n" +
			"   - is_admin=false\n" +
			"   - enable_all_folders=false\n" +
			"   - enabled_folder_ids=[selected library IDs]\n" +
			fmt.Sprintf("6. The tools cannot set a content rating limit. Tell me to set the maximum rating to %s in Dashboard > Users > select the user > Parental Control\n", maxRating) +
			"7. Reference jellyfin://guides/users-and-access for other parental control options that are also set in the dashboard:\n" +
			"   - Tag-based blocking for specific content\n" +
			"   - Access schedules to limit viewing times\n" +
			fmt.Sprintf("8. Summarize the restrictions configured for \"%s\" and the dashboard steps that remain", username)

		return &mcp.GetPromptResult{
			Description: "Parental controls setup",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- server-setup ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "server-setup",
		Title:       "Server Setup",
		Description: "Review and optimize server configuration: transcoding, networking, and general settings",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		instruction := "Review and optimize the Jellyfin server configuration. Follow these steps:\n" +
			"1. Gather server state in parallel:\n" +
			"   - jellyfin_system_info action=\"info\" for server version\n" +
			"   - jellyfin_server action=\"get_config\" for full server configuration\n" +
			"   - jellyfin_server action=\"get_config_section\" key=\"encoding\" for transcoding settings\n" +
			"   - jellyfin_server action=\"get_config_section\" key=\"network\" for networking settings such as the base URL and remote access\n" +
			"2. Reference jellyfin://guides/transcoding for hardware transcoding settings\n" +
			"3. Analyze the current configuration and identify:\n" +
			"   - Transcoding: hardware acceleration method, enabled codecs, bitrate limits\n" +
			"   - Networking: base URL, remote access settings\n" +
			"   - General: metadata providers and subtitle settings\n" +
			"4. Present findings and suggest optimizations based on the server's configuration and hardware\n" +
			"5. For changes to a named section such as encoding or network, use jellyfin_server action=\"update_config_section\" with that key and the full modified section (the tool asks me to confirm). " +
			"The tools cannot write the main configuration from get_config, so tell me where in the dashboard to make those changes"

		return &mcp.GetPromptResult{
			Description: "Server configuration review",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- library-health ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "library-health",
		Title:       "Library Health",
		Description: "Comprehensive server health check: status, storage, tasks, plugins, logs, and backups",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		instruction := "Run a comprehensive health check on the Jellyfin server. Follow these steps:\n" +
			"1. Use jellyfin_system_info action=\"health_check\", which checks server status, storage, tasks, plugins, logs, and backups in one call\n" +
			"2. If the health_check returns warnings or errors, drill into specific areas:\n" +
			"   - For storage warnings: jellyfin_analytics action=\"size_report\" to find what's using space\n" +
			"   - For task failures: health_check names the failed tasks, so find each one's id with jellyfin_tasks action=\"list\", then use jellyfin_tasks action=\"get\" with that task_id for details\n" +
			"   - For log errors: jellyfin_system_info action=\"log_file\" with severity=\"warn+error\" for full context\n" +
			"   - For plugin issues: jellyfin_plugins action=\"list\" for version details\n" +
			"3. Summarize: overall status, any action items, and their urgency\n" +
			"4. If codec or transcoding issues are found, suggest reading jellyfin://guides/transcoding"

		return &mcp.GetPromptResult{
			Description: "Server health check",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})

	// --- syncplay-help ---
	server.AddPrompt(&mcp.Prompt{
		Name:        "syncplay-help",
		Title:       "SyncPlay Help",
		Description: "Set up or troubleshoot SyncPlay watch-together groups: client support, user access, media access, network, and server logs",
		Arguments: []*mcp.PromptArgument{
			{Name: "problem", Description: "What is going wrong, if anything, such as no SyncPlay button or the group stays paused"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		problem := req.Params.Arguments["problem"]

		instruction := "Help me set up or troubleshoot SyncPlay, Jellyfin's watch-together feature. " +
			"This server cannot create, join, or control SyncPlay groups, because Jellyfin authorizes SyncPlay by the signed-in user and this server connects with an API key. " +
			"Check what the tools can see, then guide me through the steps in my Jellyfin clients. Follow these steps:\n" +
			"1. Read jellyfin://guides/syncplay for how SyncPlay works, which clients support it, and common fixes\n" +
			"2. Gather data in parallel:\n" +
			"   - jellyfin_sessions action=\"list\" to see who is connected, with which client and device, and what each session is playing\n" +
			"   - jellyfin_devices action=\"list\" for each device's app name and app version\n" +
			"   - jellyfin_users action=\"list\" to find the users who will watch together\n" +
			"3. For each of those users, use jellyfin_users action=\"get\" to confirm that the account is enabled, that it can see the same libraries " +
			"(enable_all_folders or enabled_folders), and that its parental controls (max_parental_rating, blocked_tags) allow the same items, " +
			"because every member must be able to see every item in the group's queue. Allowed tags and the blocking of unrated items do not appear in the tool output, " +
			"so if access still looks wrong, ask me to compare them on each user's Parental Control tab in the dashboard\n" +
			"4. Point out any connected client that does not support SyncPlay according to the guide\n" +
			"5. SyncPlay access cannot be read or changed with these tools, so ask me to check it in Dashboard > Users > select the user > Profile > SyncPlay access: " +
			"\"Allow user to create and join groups\" for whoever starts the group, and at least \"Allow user to join groups\" for everyone else\n"

		if problem != "" {
			instruction += fmt.Sprintf("6. I reported this problem: \"%s\". Match it against the Common Problems table in the guide\n", problem) +
				"7. Use jellyfin_system_info action=\"logs\" to find the most recent server log, then action=\"log_file\" with that name, " +
				"and look for SyncPlay lines such as a refused join, a content access mismatch, or a group that gave up waiting for a member\n" +
				"8. If one member does not follow play, pause, or seek while the others do, ask whether that member connects through a reverse proxy " +
				"and point me to jellyfin://guides/remote-access for its WebSocket settings\n" +
				"9. Summarize the likely cause and the fix, with the exact dashboard or client steps"
		} else {
			instruction += "6. Walk me through creating a group on one client and joining it from the others, using the web client steps in the guide\n" +
				"7. Summarize what is ready and anything that blocks SyncPlay (unsupported clients, missing access, library or parental-control mismatches), with how to fix each"
		}

		return &mcp.GetPromptResult{
			Description: "SyncPlay setup and troubleshooting",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: instruction},
			}},
		}, nil
	})
}
