package jellyfin

type NoInput struct{}

// --- Discovery & Browsing ---

type SearchInput struct {
	Query string `json:"query" jsonschema:"Search query text to find matching media items"`
	Type  string `json:"type,omitempty" jsonschema:"Filter by item type: Movie, Series, Episode, Audio, MusicAlbum, MusicVideo, BoxSet, Playlist, MusicArtist, Person, or Genre (the last three search names through their own endpoints)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 30)"`
}

type BrowseInput struct {
	ParentID          string   `json:"parent_id,omitempty" jsonschema:"Library or folder ID to browse within. Get IDs from jellyfin_libraries"`
	Type              string   `json:"type,omitempty" jsonschema:"Filter by item type: Movie, Series, Episode, Audio, MusicAlbum, BoxSet, Playlist (several types as a comma-separated list)"`
	Genre             string   `json:"genre,omitempty" jsonschema:"Filter by genre name, such as Action, Comedy, Drama, Sci-Fi (several genres separated by |)"`
	Year              *int     `json:"year,omitempty" jsonschema:"Filter by production year"`
	Studio            string   `json:"studio,omitempty" jsonschema:"Filter by studio name (several separated by |)"`
	Person            string   `json:"person,omitempty" jsonschema:"Filter by person name (actor, director, and so on)"`
	Tags              string   `json:"tags,omitempty" jsonschema:"Filter by tag name (several separated by |)"`
	SortBy            string   `json:"sort_by,omitempty" jsonschema:"Sort field: SortName, DateCreated, PremiereDate, CommunityRating, ProductionYear, Runtime, Random, DatePlayed, PlayCount (default SortName)"`
	SortOrder         string   `json:"sort_order,omitempty" jsonschema:"Sort direction: Ascending or Descending (default Ascending)"`
	IsFavorite        *bool    `json:"is_favorite,omitempty" jsonschema:"Filter to favorites only (true) or non-favorites only (false)"`
	IsPlayed          *bool    `json:"is_played,omitempty" jsonschema:"Filter by Jellyfin played flag: true = marked played, false = unplayed. Auto-set when playback reaches the server threshold, or toggled manually"`
	MinRating         *float64 `json:"min_community_rating,omitempty" jsonschema:"Minimum community rating filter (0.0 to 10.0)"`
	OfficialRating    string   `json:"official_rating,omitempty" jsonschema:"Filter by content rating (such as G, PG, PG-13, R, TV-MA)"`
	HasSubtitles      *bool    `json:"has_subtitles,omitempty" jsonschema:"Filter items with subtitles (true) or without (false)"`
	AudioLanguages    string   `json:"audio_languages,omitempty" jsonschema:"Only items with an audio track in one of these languages: comma-separated codes as Jellyfin stores them, such as eng or jpn; und matches tracks with no language. Needs Jellyfin 12 or later. When a code does not occur in the part of the library browsed, the result lists the codes that do"`
	SubtitleLanguages string   `json:"subtitle_languages,omitempty" jsonschema:"Only items with a subtitle track in one of these languages: comma-separated codes as Jellyfin stores them, such as eng or spa; und matches tracks with no language. Needs Jellyfin 12 or later, and cannot be combined with has_subtitles=false. When a code does not occur in the part of the library browsed, the result lists the codes that do"`
	MinDateCreated    string   `json:"min_date_created,omitempty" jsonschema:"Only items added on or after this date, listed newest first. A date such as 2024-01-01 starts at midnight in the server's time zone, the one the server instructions state; an RFC 3339 timestamp such as 2024-01-01T18:00:00Z is exact. With this filter, sort_by and sort_order must be omitted or set to DateCreated and Descending"`
	MaxDateCreated    string   `json:"max_date_created,omitempty" jsonschema:"Only items added on or before this date, listed newest first. A date such as 2024-01-31 includes that whole day in the server's time zone; an RFC 3339 timestamp such as 2024-01-31T18:00:00Z is exact. With this filter, sort_by and sort_order must be omitted or set to DateCreated and Descending"`
	MinPremiereDate   string   `json:"min_premiere_date,omitempty" jsonschema:"Items released on or after this date (YYYY-MM-DD, read by Jellyfin in UTC)"`
	MaxPremiereDate   string   `json:"max_premiere_date,omitempty" jsonschema:"Items released on or before this date (YYYY-MM-DD, read by Jellyfin in UTC)"`
	Limit             int      `json:"limit,omitempty" jsonschema:"Maximum results per page (default 50)"`
	StartIndex        int      `json:"start_index,omitempty" jsonschema:"Starting index for pagination (default 0)"`
	UserID            string   `json:"user_id,omitempty" jsonschema:"User ID from jellyfin_users list (not a username). The results then show that user's played state, favorites, and last_played, and only the libraries that user can see. Defaults to the configured user."`
}

type GetItemInput struct {
	ItemID string `json:"item_id" jsonschema:"Jellyfin item ID (from search or browse results)"`
}

type RecommendationsInput struct {
	Action   string `json:"action" jsonschema:"Action: next_up (next episodes in progress series), suggestions (personalized picks), latest (recently added to library), similar (items like a given item - requires item_id), movie_recs (movie recommendations), upcoming (upcoming TV episodes), recently_played (items marked played, newest play first, including items marked played by hand; for verified watch history use jellyfin_system_info playback_history)"`
	ItemID   string `json:"item_id,omitempty" jsonschema:"Item ID required for 'similar'. Get from search or browse results"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Library ID to scope 'latest' results. Get from jellyfin_libraries"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum results (default 25; 100 for next_up and upcoming, 500 for recently_played; for movie_recs it is per category)"`
}

// --- Media Navigation ---

type TVShowsInput struct {
	Action       string `json:"action" jsonschema:"Action to perform: seasons (list all seasons of a series), episodes (list episodes in a season), next_up (next unplayed episode for a series or all series)"`
	SeriesID     string `json:"series_id,omitempty" jsonschema:"Series ID (required for seasons and episodes, optional for next_up to scope to one series)"`
	SeasonNumber *int   `json:"season_number,omitempty" jsonschema:"Season number (required for episodes action, such as 1 for Season 1)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum results for next_up (default 100)"`
}

type MusicInput struct {
	Action string `json:"action" jsonschema:"Action: artists (browse all artists), album_artists (browse album artists only), genres (list music genres), instant_mix (generate auto-playlist from a seed item)"`
	Query  string `json:"query,omitempty" jsonschema:"Filter artists or genres by name"`
	ItemID string `json:"item_id,omitempty" jsonschema:"Seed item ID for instant_mix (album, artist, song, or playlist ID)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum results (default 200; 25 for instant_mix)"`
}

type PeopleInput struct {
	Action string `json:"action" jsonschema:"Action: persons (list actors, directors, writers, and other credits; music artists are listed by jellyfin_music) or studios (list production studios)"`
	Query  string `json:"query,omitempty" jsonschema:"Filter by name"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum results (default 200)"`
}

// --- User Media Management ---

type UserDataInput struct {
	Action        string   `json:"action" jsonschema:"Action: favorite (add to favorites), unfavorite (remove), like (thumbs up), dislike (thumbs down), clear_rating (remove rating), mark_played (set played flag), mark_unplayed (clear played flag), rate (set numeric rating 0-10), get_user_data (read play state), set_user_data (write play state; asks the user to confirm)"`
	ItemID        string   `json:"item_id" jsonschema:"Jellyfin item ID to act on"`
	Rating        *float64 `json:"rating,omitempty" jsonschema:"Numeric rating 0.0-10.0 for rate or set_user_data"`
	PlayCount     *int     `json:"play_count,omitempty" jsonschema:"Play count for set_user_data"`
	PositionTicks *int64   `json:"position_ticks,omitempty" jsonschema:"Playback position in ticks for set_user_data (10,000,000 ticks = 1 second)"`
	Played        *bool    `json:"played,omitempty" jsonschema:"Played status for set_user_data"`
	Confirm       *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm set_user_data. Set to true only after the user agreed to the warning this tool returned"`
}

type PlaylistsInput struct {
	Action     string   `json:"action" jsonschema:"Action: list (all playlists), create (new playlist), get (playlist items), add_items (add to playlist), remove_items (remove from playlist), move_item (reorder), deduplicate (find/remove duplicate entries), delete (delete the playlist itself)"`
	PlaylistID string   `json:"playlist_id,omitempty" jsonschema:"Playlist ID (required for get, add_items, remove_items, move_item, deduplicate, delete)"`
	Name       string   `json:"name,omitempty" jsonschema:"Playlist name (required for create)"`
	ItemIDs    []string `json:"item_ids,omitempty" jsonschema:"Item IDs to create the playlist with, add, or remove; remove_items removes every entry of each item"`
	ItemID     string   `json:"item_id,omitempty" jsonschema:"Item ID for move_item; the item's first entry is moved"`
	NewIndex   *int     `json:"new_index,omitempty" jsonschema:"New 0-based position for move_item, counted in the reordered playlist; an index past the end moves the entry to the end"`
	MediaType  string   `json:"media_type,omitempty" jsonschema:"Media type for create: Audio or Video"`
	DryRun     *bool    `json:"dry_run,omitempty" jsonschema:"Preview the requests remove_items, move_item, or deduplicate would send, without changing the playlist (default true for deduplicate, false otherwise)"`
	Confirm    *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm remove_items, delete, or deduplicate with dry_run=false. Set to true only after the user agreed to the warning this tool returned"`
}

type CollectionsInput struct {
	Action       string   `json:"action" jsonschema:"Action: create (new box set collection), add_items (add items to collection), remove_items (remove items from collection)"`
	CollectionID string   `json:"collection_id,omitempty" jsonschema:"Collection ID (required for add_items, remove_items); find collections with jellyfin_browse type=BoxSet"`
	Name         string   `json:"name,omitempty" jsonschema:"Collection name (required for create)"`
	ItemIDs      []string `json:"item_ids,omitempty" jsonschema:"Item IDs to add or remove"`
	ParentID     string   `json:"parent_id,omitempty" jsonschema:"Parent folder ID for create; usually omit"`
	Confirm      *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm remove_items. Set to true only after the user agreed to the warning this tool returned"`
}

// --- Playback & Sessions ---

type SessionsInput struct {
	Action string `json:"action" jsonschema:"Action: list (all active sessions with now-playing info and session IDs), resume (items with in-progress playback)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum resume items (default 100)"`
}

type PlaybackControlInput struct {
	SessionID string `json:"session_id" jsonschema:"Session ID to control (get from jellyfin_sessions list action)"`
	Command   string `json:"command" jsonschema:"Command: Pause, Unpause, Stop, NextTrack, PreviousTrack, Seek, Mute, Unmute, ToggleMute, SetVolume, SendMessage, GoHome, GoToSettings, ChannelUp, ChannelDown, DisplayContent"`
	SeekTicks *int64 `json:"seek_position_ticks,omitempty" jsonschema:"Position in ticks for Seek (10,000,000 ticks = 1 second)"`
	Volume    *int   `json:"volume,omitempty" jsonschema:"Volume level 0-100 (required for SetVolume)"`
	Message   string `json:"message,omitempty" jsonschema:"Text message for SendMessage"`
	ItemID    string `json:"item_id,omitempty" jsonschema:"Item ID for DisplayContent (navigate client to show an item)"`
}

type PlayInput struct {
	SessionID   string   `json:"session_id" jsonschema:"Target session ID (get from jellyfin_sessions list action)"`
	ItemIDs     []string `json:"item_ids" jsonschema:"Ordered list of item IDs to play"`
	PlayCommand string   `json:"play_command,omitempty" jsonschema:"Queue mode: PlayNow (replace queue and start, default), PlayNext (insert after current), PlayLast (append to end)"`
	StartIndex  *int     `json:"start_index,omitempty" jsonschema:"Index in item_ids to start from (default 0)"`
}

// --- Administration ---

type SystemInfoInput struct {
	Action       string `json:"action" jsonschema:"Action: whoami (current user identity and permissions), info (server name, version, and pending restart), storage (disk space), activity_log (events such as sign-ins, playback, user changes, failed scheduled tasks, and plugin updates; server errors are in log_file), ping (check responsiveness), logs (list log files), log_file (read a server log's entries, filtered by severity, time, and text), playback_history (recently played items with play counts, not an event log), health_check (one report on server status, storage, tasks, plugins, logs, and backups)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum entries: activity_log (default 200), playback_history (default 500), logs (default 25), or log_file (the newest matching entries, default 100, at most 500)"`
	Name         string `json:"name,omitempty" jsonschema:"Log file name for log_file action (get names from logs action)"`
	UserID       string `json:"user_id,omitempty" jsonschema:"User ID (not a username; from jellyfin_users list): whose history for playback_history, and only entries by this user for activity_log"`
	ItemID       string `json:"item_id,omitempty" jsonschema:"For activity_log: only entries about this item (a Jellyfin item ID)"`
	VerifiedOnly *bool  `json:"verified_only,omitempty" jsonschema:"For playback_history: only return items with verified playback sessions (default true). Set to false to include items manually marked as watched."`
	LogType      string `json:"log_type,omitempty" jsonschema:"Filter for logs action, exactly one of: main (server logs only, default), ffmpeg (transcode logs only), all (everything)"`
	Severity     string `json:"severity,omitempty" jsonschema:"For log_file, exactly one of: all (default), warn (warnings), error (errors and fatal errors), or warn+error. For activity_log: only entries of these levels, one or several separated by commas, such as Warning,Error,Critical (levels: Trace, Debug, Information, Warning, Error, Critical, None)"`
	MinDate      string `json:"min_date,omitempty" jsonschema:"For activity_log and log_file: only entries on or after this date. A date such as 2024-01-01 starts at midnight in the server's time zone, the one the server instructions state; an RFC 3339 timestamp such as 2024-01-01T18:00:00Z is exact"`
	MaxDate      string `json:"max_date,omitempty" jsonschema:"For activity_log and log_file: only entries on or before this date. A date such as 2024-01-31 includes that whole day in the server's time zone; an RFC 3339 timestamp is exact"`
	Contains     string `json:"contains,omitempty" jsonschema:"For log_file: only entries whose text, including an exception's message and stack trace, contains this, ignoring case (such as SqliteException or a title)"`
	Type         string `json:"type,omitempty" jsonschema:"For activity_log: only entries whose type contains this, ignoring case (such as SessionStarted, PlaybackStopped for video and audio, TaskCompleted, AuthenticationFailed)"`
	SortBy       string `json:"sort_by,omitempty" jsonschema:"For activity_log: date (default), name, type, or severity"`
	SortOrder    string `json:"sort_order,omitempty" jsonschema:"For activity_log: descending (the default for date and severity) or ascending (the default for name and type)"`
}

type SystemControlInput struct {
	Action  string `json:"action" jsonschema:"Action: restart (restart the Jellyfin server), shutdown (stop the server completely)"`
	Confirm *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm restart or shutdown. Set to true only after the user agreed to the warning this tool returned"`
}

type UsersInput struct {
	Action           string   `json:"action" jsonschema:"Action: list, get, create, delete, update_policy (permissions), update_password, update_config (per-user preferences), qc_status (Quick Connect enabled?), qc_initiate (starts Quick Connect for this API client, which is rarely useful; to sign in a device, ask the user for the code the device shows and use qc_authorize), qc_authorize (authorize the Quick Connect code a device shows)"`
	UserID           string   `json:"user_id,omitempty" jsonschema:"User ID (required for get, delete, update_policy, update_password, update_config)"`
	Username         string   `json:"username,omitempty" jsonschema:"Username (required for create)"`
	Password         string   `json:"password,omitempty" jsonschema:"Password (for create or update_password)"`
	IsAdmin          *bool    `json:"is_admin,omitempty" jsonschema:"Grant admin privileges (for update_policy)"`
	IsDisabled       *bool    `json:"is_disabled,omitempty" jsonschema:"Disable user account (for update_policy)"`
	EnableAllFolders *bool    `json:"enable_all_folders,omitempty" jsonschema:"Access all libraries (for update_policy)"`
	EnabledFolderIDs []string `json:"enabled_folder_ids,omitempty" jsonschema:"Library IDs user can access (for update_policy when enable_all_folders is false)"`
	Confirm          *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete, update_policy, update_password, qc_authorize, or update_config with config. Set to true only after the user agreed to the warning this tool returned"`
	Config           any      `json:"config,omitempty" jsonschema:"Advanced, for update_config: the complete raw UserConfiguration object in Jellyfin's PascalCase form. It replaces the user's whole configuration, overrides the other preference fields, and asks the user to confirm. get does not return this object; prefer subtitle_language, audio_language, and play_default_audio"`
	Code             string   `json:"code,omitempty" jsonschema:"Quick Connect code for qc_authorize"`
	SubtitleLanguage string   `json:"subtitle_language,omitempty" jsonschema:"Preferred subtitle language for update_config (ISO 639 code)"`
	AudioLanguage    string   `json:"audio_language,omitempty" jsonschema:"Preferred audio language for update_config (ISO 639 code)"`
	PlayDefaultAudio *bool    `json:"play_default_audio,omitempty" jsonschema:"Play default audio track for update_config"`
}

type LibraryManageInput struct {
	Action          string `json:"action" jsonschema:"Action: scan (scan all libraries), refresh_item (refresh metadata for one item), delete_item (permanently delete), list_folders (list libraries), add_folder (create library), remove_folder (delete library), add_path (add media path), remove_path (remove media path), rename_folder (rename library), update_options (set library options), browse_drives (list server drives), browse_directory (browse server filesystem)"`
	ItemID          string `json:"item_id,omitempty" jsonschema:"Item ID for refresh_item or delete_item"`
	FolderName      string `json:"folder_name,omitempty" jsonschema:"Library name (not ID) for add_folder, remove_folder, rename_folder (current name), add_path, remove_path, and update_options"`
	NewName         string `json:"new_name,omitempty" jsonschema:"New library name for rename_folder"`
	CollectionType  string `json:"collection_type,omitempty" jsonschema:"Type for add_folder: movies, tvshows, music, musicvideos, homevideos, boxsets, books, mixed"`
	Path            string `json:"path,omitempty" jsonschema:"Filesystem path for add_folder (the library's first media path; a library scan is requested), add_path, remove_path, or browse_directory"`
	LibraryOptions  any    `json:"library_options,omitempty" jsonschema:"For update_options: the complete LibraryOptions object from list_folders, edited. It replaces all of the library's options and asks the user to confirm"`
	ReplaceMetadata *bool  `json:"replace_all_metadata,omitempty" jsonschema:"For refresh_item: replace all metadata, including edits, instead of filling in what is missing (default false)"`
	ReplaceImages   *bool  `json:"replace_all_images,omitempty" jsonschema:"For refresh_item: replace all images instead of adding missing ones (default false)"`
	Confirm         *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete_item, remove_folder, remove_path, or update_options. Set to true only after the user agreed to the warning this tool returned"`
}

type TasksInput struct {
	Action   string `json:"action" jsonschema:"Action: list (all scheduled tasks with status), get (task details), start (run a task now), stop (cancel running task), set_triggers (replace task triggers; asks the user to confirm)"`
	TaskID   string `json:"task_id,omitempty" jsonschema:"Task ID from list, not the task name (required for get, start, stop, set_triggers)"`
	Triggers any    `json:"triggers,omitempty" jsonschema:"Array of trigger objects for set_triggers, replacing all of the task's triggers. Types: DailyTrigger, WeeklyTrigger, IntervalTrigger, StartupTrigger; times and intervals are in ticks (10,000,000 ticks = 1 second). Use 'get' first to see the current format"`
	Confirm  *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm set_triggers. Set to true only after the user agreed to the warning this tool returned"`
}

type PluginsInput struct {
	Action      string `json:"action" jsonschema:"Action: list (installed plugins), enable, disable, uninstall, get_config (plugin configuration), update_config (set plugin config; asks the user to confirm), list_packages (available packages from repositories), install, list_repos (plugin repositories), set_repos (replace all repositories; asks the user to confirm)"`
	PluginID    string `json:"plugin_id,omitempty" jsonschema:"Plugin ID (for enable, disable, uninstall, get_config, update_config)"`
	Version     string `json:"version,omitempty" jsonschema:"Plugin version (required for enable, disable, and uninstall; optional for install)"`
	PackageName string `json:"package_name,omitempty" jsonschema:"Package name for install"`
	RepoURL     string `json:"repository_url,omitempty" jsonschema:"Repository URL for install"`
	Confirm     *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm uninstall, update_config, or set_repos. Set to true only after the user agreed to the warning this tool returned"`
	Config      any    `json:"config,omitempty" jsonschema:"Plugin configuration object for update_config. Use get_config first to see the current format."`
	Repos       any    `json:"repos,omitempty" jsonschema:"Array of {Name, Url} objects for set_repos, replacing all repositories. Use list_repos first, then send back the edited array"`
}

type LiveTVInput struct {
	Action    string `json:"action" jsonschema:"Action: channels (list channels), channel (channel details), programs (current/upcoming), recommended (recommended programs), guide_info (TV guide metadata), tuners (configured tuner hosts)"`
	ChannelID string `json:"channel_id,omitempty" jsonschema:"Channel ID (required for channel action, optional for programs)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum results (default 500 for channels, 200 for programs, 100 for recommended)"`
}

type RecordingsInput struct {
	Action      string `json:"action" jsonschema:"Action: list (recordings), delete, timers (scheduled recordings), create_timer (schedule from program), cancel_timer, series_timers (series rules), create_series_timer, cancel_series_timer"`
	RecordingID string `json:"recording_id,omitempty" jsonschema:"Recording ID for delete"`
	TimerID     string `json:"timer_id,omitempty" jsonschema:"Timer ID for cancel_timer or cancel_series_timer"`
	ProgramID   string `json:"program_id,omitempty" jsonschema:"Program ID for create_timer or create_series_timer"`
	Confirm     *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete, cancel_timer, or cancel_series_timer. Set to true only after the user agreed to the warning this tool returned"`
}

// --- Metadata & Content ---

type MetadataInput struct {
	Action          string   `json:"action" jsonschema:"Action: search (find metadata online), apply (apply search result), update (edit item fields), batch_update (update multiple items), editor_info (available metadata fields), external_ids (provider ID types)"`
	ItemID          string   `json:"item_id,omitempty" jsonschema:"Item ID (required for apply, update, editor_info, external_ids)"`
	ItemIDs         []string `json:"item_ids,omitempty" jsonschema:"Multiple item IDs for batch_update (max 50); batch_update writes the same values to every item"`
	SearchType      string   `json:"search_type,omitempty" jsonschema:"Type for search (required): Movie, Series, Person, Book, BoxSet, MusicAlbum, MusicArtist"`
	SearchQuery     string   `json:"search_query,omitempty" jsonschema:"Search text (required for search)"`
	SearchYear      *int     `json:"search_year,omitempty" jsonschema:"Year to narrow search results"`
	ProviderName    string   `json:"provider_name,omitempty" jsonschema:"Provider ID key for apply, taken from a search result's provider_ids (such as Tmdb, Imdb, Tvdb), not a provider display name such as TheMovieDb"`
	ProviderID      string   `json:"provider_id,omitempty" jsonschema:"Provider-specific ID for apply"`
	Name            string   `json:"name,omitempty" jsonschema:"New name for update or batch_update"`
	Overview        string   `json:"overview,omitempty" jsonschema:"New overview text for update or batch_update"`
	Genres          []string `json:"genres,omitempty" jsonschema:"Genres for update or batch_update. Replaces the item's entire genre list; include the existing genres to keep them"`
	Tags            []string `json:"tags,omitempty" jsonschema:"Tags for update or batch_update. Replaces the item's entire tag list; include the existing tags to keep them"`
	Studios         []string `json:"studios,omitempty" jsonschema:"Studio names for update or batch_update. Replaces the item's entire studio list"`
	Year            *int     `json:"production_year,omitempty" jsonschema:"Production year for update or batch_update"`
	CommunityRating *float64 `json:"community_rating,omitempty" jsonschema:"Community rating for update/batch_update (0.0 to 10.0)"`
	OfficialRating  string   `json:"official_rating,omitempty" jsonschema:"Content rating for update/batch_update (such as G, PG, PG-13, R, TV-MA)"`
	SortName        string   `json:"sort_name,omitempty" jsonschema:"Custom sort name for update/batch_update"`
	LockedFields    []string `json:"locked_fields,omitempty" jsonschema:"Fields to lock from automatic updates, for update or batch_update (such as Name, Overview, Genres). Replaces the item's entire locked list"`
	DryRun          *bool    `json:"dry_run,omitempty" jsonschema:"Preview the fields update or batch_update would write without applying them. Defaults to true for batch_update and false for update; set to false to apply, and the user is asked to confirm."`
	Confirm         *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm apply, update, or batch_update. Set to true only after the user agreed to the warning this tool returned"`
}

type SubtitlesLyricsInput struct {
	Action            string   `json:"action" jsonschema:"Action: search_subtitles, download_subtitle, delete_subtitle, batch_download_subtitles (search and download for multiple items), upload_subtitle (upload subtitle file as base64), get_lyrics, search_lyrics, download_lyrics, delete_lyrics"`
	ItemID            string   `json:"item_id,omitempty" jsonschema:"Media item ID (required for every action except batch_download_subtitles)"`
	ItemIDs           []string `json:"item_ids,omitempty" jsonschema:"Multiple item IDs for batch_download_subtitles (max 25)"`
	Language          string   `json:"language,omitempty" jsonschema:"Language code for search_subtitles and batch_download_subtitles (such as en, es, fr, de; default en). batch_download_subtitles downloads the first search result for each item"`
	SubtitleID        string   `json:"subtitle_id,omitempty" jsonschema:"Subtitle ID from search results (for download_subtitle)"`
	LyricID           string   `json:"lyric_id,omitempty" jsonschema:"Lyric ID from search results (for download_lyrics)"`
	SubtitleIndex     *int     `json:"subtitle_index,omitempty" jsonschema:"For delete_subtitle: the index of an external subtitle track (is_external in jellyfin_get_item subtitle_streams); tracks inside the media file cannot be deleted"`
	SubtitleData      string   `json:"subtitle_data,omitempty" jsonschema:"Base64-encoded subtitle file content for upload_subtitle (at most 20 MiB decoded)"`
	SubtitleFormat    string   `json:"subtitle_format,omitempty" jsonschema:"Subtitle format for upload_subtitle: one of ass, mks, sami, smi, srt, ssa, sub, sup, vtt"`
	SubtitleLanguage  string   `json:"subtitle_language,omitempty" jsonschema:"Three-letter language code for upload_subtitle (such as eng, spa, fre; default eng)"`
	IsForced          *bool    `json:"is_forced,omitempty" jsonschema:"Mark subtitle as forced for upload_subtitle"`
	IsHearingImpaired *bool    `json:"is_hearing_impaired,omitempty" jsonschema:"Mark subtitle as hearing impaired (SDH) for upload_subtitle"`
	Confirm           *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete_subtitle, delete_lyrics, or batch_download_subtitles. Set to true only after the user agreed to the warning this tool returned"`
}

type ImagesInput struct {
	Action     string `json:"action" jsonschema:"Action: list (images for item), get_url (direct image URL), remote_list (browse online images), remote_download (download remote image), upload (upload image from base64 data)"`
	ItemID     string `json:"item_id" jsonschema:"Item ID"`
	ImageType  string `json:"image_type,omitempty" jsonschema:"Image type: Primary, Backdrop, Logo, Thumb, Banner, Art, Disc, Box, Screenshot, Menu, Chapter, BoxRear, or Profile (default Primary)"`
	Provider   string `json:"provider_name,omitempty" jsonschema:"Provider for remote_list (such as TheMovieDb)"`
	ImageURL   string `json:"image_url,omitempty" jsonschema:"Remote image URL for remote_download"`
	ImageIndex *int   `json:"image_index,omitempty" jsonschema:"For get_url: index among several images of the same type (default 0)"`
	ImageData  string `json:"image_data,omitempty" jsonschema:"Base64-encoded image file for upload (JPEG, PNG, WebP, GIF, or BMP; at most 20 MiB decoded)"`
}

type DevicesInput struct {
	Action   string `json:"action" jsonschema:"Action: list (connected devices), get (device info), delete (remove device), api_keys (list API keys), create_api_key, revoke_api_key"`
	DeviceID string `json:"device_id,omitempty" jsonschema:"Device ID for get or delete"`
	Key      string `json:"key,omitempty" jsonschema:"For revoke_api_key: the full API key token, if the user has it; api_keys shows keys masked, so app_name is the usual way"`
	AppName  string `json:"app_name,omitempty" jsonschema:"Application name for create_api_key, or the name of the one key to revoke for revoke_api_key"`
	Limit    int    `json:"limit,omitempty" jsonschema:"For list: maximum results (default 50)"`
	Days     int    `json:"days,omitempty" jsonschema:"For list: only show devices active within N days (default 30)"`
	Confirm  *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete or revoke_api_key. Set to true only after the user agreed to the warning this tool returned"`
}

// --- Server Management ---

type ServerManageInput struct {
	Action   string `json:"action" jsonschema:"Action: get_config (full server config, read-only here), get_config_section (read named section), update_config_section (replace a named section; asks the user to confirm), list_backups (existing backups), backup_manifest (what one backup contains), create_backup (create new backup), restore_backup (restore from backup; asks the user to confirm)"`
	Key      string `json:"key,omitempty" jsonschema:"Config section key for get_config_section and update_config_section: encoding (transcoding), network, branding, metadata, livetv"`
	Config   any    `json:"config,omitempty" jsonschema:"Complete config object for update_config_section; it replaces the entire section. Never build it from get_config_section output for livetv, whose URLs and credentials are redacted"`
	FileName string `json:"file_name,omitempty" jsonschema:"For backup_manifest and restore_backup: the backup's file name, or the Path list_backups shows for it"`
	Confirm  *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm update_config_section or restore_backup. Set to true only after the user agreed to the warning this tool returned"`
}

// --- Item Extras ---

type ItemExtrasInput struct {
	Action string `json:"action" jsonschema:"Action: playback_info (transcoding/direct play diagnostics), special_features (behind-the-scenes, extras), theme_songs, theme_videos, local_trailers, segments (intro/outro markers)"`
	ItemID string `json:"item_id" jsonschema:"Item ID"`
}

// DownloadLinkInput is the input of jellyfin_download_link.
type DownloadLinkInput struct {
	ItemID string `json:"item_id" jsonschema:"Item ID, from jellyfin_search or jellyfin_browse"`
}

// --- Video Management ---

type VideosInput struct {
	Action  string   `json:"action" jsonschema:"Action: merge_versions (combine multiple files as alternate versions), split_versions (undo merge)"`
	ItemIDs []string `json:"item_ids,omitempty" jsonschema:"Item IDs to merge (2+ required for merge_versions)"`
	ItemID  string   `json:"item_id,omitempty" jsonschema:"Item ID with alternate sources (for split_versions)"`
	Confirm *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm merge_versions or split_versions. Set to true only after the user agreed to the warning this tool returned"`
}

// --- Analytics ---

type AnalyticsInput struct {
	Action   string `json:"action" jsonschema:"Action: library_stats (counts by type), library_size (total storage by type), size_report (rank individual items or series by total storage size; defaults to Series), codec_report (codec/resolution distribution), never_played (unplayed items), recently_added (items added within N days), duplicate_check (items whose name and year match; it does not compare files), played_status (per-user played/unplayed status for an item; requires item_id)"`
	ItemID   string `json:"item_id,omitempty" jsonschema:"Item ID for played_status (series, movie, or other item; get it from search or browse)"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Library ID to scope results (get from jellyfin_libraries); ignored by played_status"`
	Type     string `json:"type,omitempty" jsonschema:"Item type filter. Defaults per action: library_stats counts nine types; codec_report and duplicate_check use Movie; never_played uses Movie and Episode; size_report uses Series; library_size uses Movie, Episode, Audio, and MusicVideo. Size and codec actions need file-bearing types (Movie, Episode, Audio, MusicVideo); Series and MusicAlbum have no files of their own"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum results (default 500 for never_played and recently_added, 25 for size_report)"`
	Days     int    `json:"days,omitempty" jsonschema:"Lookback period in days for recently_added (default 30)"`
}
