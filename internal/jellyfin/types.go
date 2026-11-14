package jellyfin

type NoInput struct{}

// --- Discovery & Browsing ---

type SearchInput struct {
	Query string `json:"query,omitempty" jsonschema:"Search text (required unless type is Person, Studio, MusicArtist, or Genre)"`
	Type  string `json:"type,omitempty" jsonschema:"Item type: Movie, Series, Episode, Audio (songs/tracks), MusicAlbum, MusicVideo, BoxSet, Playlist, Book, MusicArtist, Person (excludes music artists), Studio, or Genre"`
	Limit int    `json:"limit,omitempty" jsonschema:"Max results (default 30)"`
}

type BrowseInput struct {
	ParentID          string   `json:"parent_id,omitempty" jsonschema:"Library name or library/series/season/folder ID"`
	Type              string   `json:"type,omitempty" jsonschema:"Item type(s), comma-separated (Movie, Series, Episode, MusicVideo, Video, Audio, MusicAlbum, BoxSet, Playlist)"`
	Genre             string   `json:"genre,omitempty" jsonschema:"Genre(s), pipe-separated"`
	Year              *int     `json:"year,omitempty" jsonschema:"Production year"`
	Studio            string   `json:"studio,omitempty" jsonschema:"Studio(s), pipe-separated"`
	Person            string   `json:"person,omitempty" jsonschema:"Actor, director, or writer name or ID"`
	Tags              string   `json:"tags,omitempty" jsonschema:"Tag(s), pipe-separated"`
	SortBy            string   `json:"sort_by,omitempty" jsonschema:"SortName, DateCreated, PremiereDate, CommunityRating, ProductionYear, Runtime, Random, DatePlayed, PlayCount"`
	SortOrder         string   `json:"sort_order,omitempty" jsonschema:"Ascending or Descending"`
	IsFavorite        *bool    `json:"is_favorite,omitempty" jsonschema:"Filter by favorite"`
	IsPlayed          *bool    `json:"is_played,omitempty" jsonschema:"Filter by played"`
	MinRating         *float64 `json:"min_community_rating,omitempty" jsonschema:"Min rating (0-10)"`
	OfficialRating    string   `json:"official_rating,omitempty" jsonschema:"Content rating (PG-13, R, TV-MA)"`
	HasSubtitles      *bool    `json:"has_subtitles,omitempty" jsonschema:"Filter by subtitles"`
	AudioLanguages    string   `json:"audio_languages,omitempty" jsonschema:"Audio language codes, comma-separated (eng,jpn; und: untagged; Jellyfin 12+)"`
	SubtitleLanguages string   `json:"subtitle_languages,omitempty" jsonschema:"Subtitle language codes, comma-separated (eng,spa; Jellyfin 12+; omit has_subtitles=false)"`
	Resolution        string   `json:"resolution,omitempty" jsonschema:"Preferred resolution tier, handles cropped widescreen and 4:3 (omit is_hd, is_4k, and pixel bounds)"`
	MinHeight         *int     `json:"min_height,omitempty" jsonschema:"Min height (px)"`
	MaxHeight         *int     `json:"max_height,omitempty" jsonschema:"Max height (px)"`
	MinWidth          *int     `json:"min_width,omitempty" jsonschema:"Min width (px)"`
	MaxWidth          *int     `json:"max_width,omitempty" jsonschema:"Max width (px)"`
	IsHD              *bool    `json:"is_hd,omitempty" jsonschema:"Legacy SD/HD bucket (prefer resolution; true = 720p-1440p, add is_4k=true for all HD+4K+8K)"`
	Is4K              *bool    `json:"is_4k,omitempty" jsonschema:"Legacy 4K/8K bucket (prefer resolution; omit for non-4K)"`
	GroupBySeries     *bool    `json:"group_by_series,omitempty" jsonschema:"With type=Episode, list matching series by name (omit sort and created-date filters)"`
	MinDateCreated    string   `json:"min_date_created,omitempty" jsonschema:"Added on/after YYYY-MM-DD (server TZ) or RFC 3339"`
	MaxDateCreated    string   `json:"max_date_created,omitempty" jsonschema:"Added on/before YYYY-MM-DD (server TZ) or RFC 3339"`
	MinPremiereDate   string   `json:"min_premiere_date,omitempty" jsonschema:"Released on/after YYYY-MM-DD (UTC)"`
	MaxPremiereDate   string   `json:"max_premiere_date,omitempty" jsonschema:"Released on/before YYYY-MM-DD (UTC)"`
	Limit             int      `json:"limit,omitempty" jsonschema:"Page size (default 50)"`
	StartIndex        int      `json:"start_index,omitempty" jsonschema:"0-based offset"`
	UserID            string   `json:"user_id,omitempty" jsonschema:"Username or ID to view play state and access as"`
}

type GetItemInput struct {
	ItemID string `json:"item_id" jsonschema:"Item ID"`
}

type RecommendationsInput struct {
	Action   string `json:"action" jsonschema:"Recommendation view (recently_played includes manually marked items; use jellyfin_playback_stats playback_history for verified plays)"`
	ItemID   string `json:"item_id,omitempty" jsonschema:"Seed item ID (required for similar)"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Library name or ID for latest"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max results (default 25; 50 for recently_played; per category for movie_recs)"`
	UserID   string `json:"user_id,omitempty" jsonschema:"Username or user ID to inspect another user's recommendations or history"`
}

// --- Media Navigation ---

type TVShowsInput struct {
	Action       string `json:"action" jsonschema:"Action: seasons, episodes, or next_up"`
	SeriesID     string `json:"series_id,omitempty" jsonschema:"Series name or ID (required for seasons/episodes; optional filter for next_up)"`
	SeasonNumber *int   `json:"season_number,omitempty" jsonschema:"Season number (required for episodes)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max results for next_up (default 25)"`
	UserID       string `json:"user_id,omitempty" jsonschema:"Username or user ID to inspect another user's watch progress"`
}

type MusicInput struct {
	Action string `json:"action" jsonschema:"Action: artists, album_artists, genres, or instant_mix"`
	Query  string `json:"query,omitempty" jsonschema:"Name filter for artists, album_artists, or genres"`
	ItemID string `json:"item_id,omitempty" jsonschema:"Seed album, artist, song, or playlist ID for instant_mix"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max results (default 50; 25 for instant_mix)"`
}

// --- User Media Management ---

type UserDataInput struct {
	Action        string   `json:"action" jsonschema:"User-data action"`
	ItemID        string   `json:"item_id" jsonschema:"Item ID"`
	UserID        string   `json:"user_id,omitempty" jsonschema:"Username or user ID for get_user_data (defaults to current user)"`
	Rating        *float64 `json:"rating,omitempty" jsonschema:"Rating 0-10 for rate or set_user_data"`
	PlayCount     *int     `json:"play_count,omitempty" jsonschema:"Play count for set_user_data"`
	PositionTicks *int64   `json:"position_ticks,omitempty" jsonschema:"Position in ticks for set_user_data (10,000,000 = 1s)"`
	Played        *bool    `json:"played,omitempty" jsonschema:"Played flag for set_user_data"`
	Confirm       *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm set_user_data; never set true before CONFIRMATION REQUIRED"`
}

type PlaylistsInput struct {
	Action     string   `json:"action" jsonschema:"Playlist action"`
	PlaylistID string   `json:"playlist_id,omitempty" jsonschema:"Playlist name or ID (all actions except list/create)"`
	UserID     string   `json:"user_id,omitempty" jsonschema:"Username or user ID for list or get (defaults to current user)"`
	Name       string   `json:"name,omitempty" jsonschema:"Playlist name for create"`
	ItemIDs    []string `json:"item_ids,omitempty" jsonschema:"Item IDs for create, add_items, or remove_items (removes all matching entries)"`
	ItemID     string   `json:"item_id,omitempty" jsonschema:"Item ID for move_item (moves first entry)"`
	NewIndex   *int     `json:"new_index,omitempty" jsonschema:"0-based target index for move_item"`
	MediaType  string   `json:"media_type,omitempty" jsonschema:"Audio or Video for create"`
	DryRun     *bool    `json:"dry_run,omitempty" jsonschema:"Preview remove_items, move_item, or deduplicate without writing (default true for deduplicate; set false to apply)"`
	Confirm    *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm remove_items, delete, or deduplicate (dry_run=false); never set true before CONFIRMATION REQUIRED"`
}

type CollectionsInput struct {
	Action       string   `json:"action" jsonschema:"Collection action"`
	CollectionID string   `json:"collection_id,omitempty" jsonschema:"Collection name or ID for add_items or remove_items"`
	Name         string   `json:"name,omitempty" jsonschema:"Collection name for create"`
	ItemIDs      []string `json:"item_ids,omitempty" jsonschema:"Item IDs to add or remove"`
	ParentID     string   `json:"parent_id,omitempty" jsonschema:"Optional parent library name or folder ID for create"`
	Confirm      *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm remove_items; never set true before CONFIRMATION REQUIRED"`
}

// --- Playback & Sessions ---

type SessionsInput struct {
	Action string `json:"action" jsonschema:"list (active sessions) or resume (in-progress items)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max resume items (default 25)"`
	UserID string `json:"user_id,omitempty" jsonschema:"Username or user ID to filter active sessions or inspect another user's resume list"`
}

type PlaybackInput struct {
	Action     string   `json:"action" jsonschema:"PlayNow/PlayNext/PlayLast queue items; other actions control active playback or client UI"`
	SessionID  string   `json:"session_id" jsonschema:"Target session ID or device name (from jellyfin_sessions list)"`
	ItemIDs    []string `json:"item_ids,omitempty" jsonschema:"Ordered item IDs for PlayNow, PlayNext, or PlayLast"`
	ItemID     string   `json:"item_id,omitempty" jsonschema:"Item ID for DisplayContent (or single-item PlayNow/PlayNext/PlayLast)"`
	StartIndex *int     `json:"start_index,omitempty" jsonschema:"0-based index in item_ids to start at"`
	SeekTicks  *int64   `json:"seek_position_ticks,omitempty" jsonschema:"Position in ticks for Seek (10,000,000 = 1s)"`
	Volume     *int     `json:"volume,omitempty" jsonschema:"Volume 0-100 for SetVolume"`
	Message    string   `json:"message,omitempty" jsonschema:"Message text for SendMessage"`
}

// --- Administration ---

type SystemInfoInput struct {
	Action    string `json:"action" jsonschema:"Diagnostic action (activity_log = auth/playback/task events; log_file = server/FFmpeg logs)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max entries: activity_log (default 50), logs (default 25), log_file (default 100, max 500)"`
	Name      string `json:"name,omitempty" jsonschema:"Required log file name for log_file (list via action=logs)"`
	UserID    string `json:"user_id,omitempty" jsonschema:"Username or user ID for activity_log"`
	ItemID    string `json:"item_id,omitempty" jsonschema:"Item ID filter for activity_log"`
	LogType   string `json:"log_type,omitempty" jsonschema:"Filter for logs: main (default, server logs), ffmpeg (transcode logs), or all"`
	Severity  string `json:"severity,omitempty" jsonschema:"For log_file: all, warn, error, warn+error. For activity_log: comma-separated levels (Information,Warning,Error,Critical)"`
	MinDate   string `json:"min_date,omitempty" jsonschema:"Start YYYY-MM-DD (server TZ) or RFC 3339 for activity_log/log_file"`
	MaxDate   string `json:"max_date,omitempty" jsonschema:"End YYYY-MM-DD (whole day, server TZ) or RFC 3339 for activity_log/log_file"`
	Contains  string `json:"contains,omitempty" jsonschema:"Case-insensitive text/exception filter for log_file"`
	Type      string `json:"type,omitempty" jsonschema:"Case-insensitive event type substring for activity_log (such as PlaybackStopped, AuthenticationFailed)"`
	SortBy    string `json:"sort_by,omitempty" jsonschema:"Sort field for activity_log (default date)"`
	SortOrder string `json:"sort_order,omitempty" jsonschema:"Sort direction for activity_log"`
}

type UsersInput struct {
	Action           string   `json:"action" jsonschema:"User or Quick Connect action (use qc_authorize with device code to sign in a device)"`
	UserID           string   `json:"user_id,omitempty" jsonschema:"Username or user ID (required for get, delete, update_*; optional target for qc_authorize)"`
	Username         string   `json:"username,omitempty" jsonschema:"New username for create"`
	Password         string   `json:"password,omitempty" jsonschema:"Password for create or update_password"`
	IsAdmin          *bool    `json:"is_admin,omitempty" jsonschema:"Admin privilege for update_policy"`
	IsDisabled       *bool    `json:"is_disabled,omitempty" jsonschema:"Disable account for update_policy"`
	EnableAllFolders *bool    `json:"enable_all_folders,omitempty" jsonschema:"Access all libraries for update_policy"`
	EnabledFolderIDs []string `json:"enabled_folder_ids,omitempty" jsonschema:"Allowed library names or IDs for update_policy (implies enable_all_folders=false)"`
	Confirm          *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete, update_policy, update_password, qc_authorize, or update_config (with config); never set true before CONFIRMATION REQUIRED"`
	Config           any      `json:"config,omitempty" jsonschema:"PascalCase UserConfiguration fields merged for update_config (prefer audio/subtitle fields)"`
	Code             string   `json:"code,omitempty" jsonschema:"Quick Connect code for qc_authorize"`
	SubtitleLanguage string   `json:"subtitle_language,omitempty" jsonschema:"Preferred subtitle ISO 639 code for update_config"`
	AudioLanguage    string   `json:"audio_language,omitempty" jsonschema:"Preferred audio ISO 639 code for update_config"`
	PlayDefaultAudio *bool    `json:"play_default_audio,omitempty" jsonschema:"Play default audio track for update_config"`
}

type LibraryManageInput struct {
	Action          string `json:"action" jsonschema:"Library, scan, item delete/refresh, or filesystem action"`
	ItemID          string `json:"item_id,omitempty" jsonschema:"Library name or item ID for scan/refresh_item; item ID for delete_item"`
	FolderName      string `json:"folder_name,omitempty" jsonschema:"Library name for add/remove/rename_folder, add/remove_path, update_options"`
	NewName         string `json:"new_name,omitempty" jsonschema:"New library name for rename_folder"`
	CollectionType  string `json:"collection_type,omitempty" jsonschema:"Library type for add_folder"`
	Path            string `json:"path,omitempty" jsonschema:"Server path for scan, add_folder, add_path, remove_path, or browse_directory"`
	LibraryOptions  any    `json:"library_options,omitempty" jsonschema:"LibraryOptions fields merged for update_options"`
	ReplaceMetadata *bool  `json:"replace_all_metadata,omitempty" jsonschema:"Overwrite existing metadata on refresh_item (default false)"`
	ReplaceImages   *bool  `json:"replace_all_images,omitempty" jsonschema:"Overwrite existing images on refresh_item (default false)"`
	Confirm         *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete_item, remove_folder, remove_path, update_options, or refresh_item when replacing metadata/images; never set true before CONFIRMATION REQUIRED"`
}

type PluginsInput struct {
	Action      string `json:"action" jsonschema:"Plugin or repository action"`
	PluginID    string `json:"plugin_id,omitempty" jsonschema:"Plugin ID"`
	Version     string `json:"version,omitempty" jsonschema:"Plugin version (required for enable/disable/uninstall; optional for install)"`
	PackageName string `json:"package_name,omitempty" jsonschema:"Package name for install"`
	RepoURL     string `json:"repository_url,omitempty" jsonschema:"Repository URL for install"`
	Confirm     *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm disable, uninstall, install, update_config, or set_repos; never set true before CONFIRMATION REQUIRED"`
	Config      any    `json:"config,omitempty" jsonschema:"Fields merged into plugin config for update_config"`
	Repos       any    `json:"repos,omitempty" jsonschema:"Full [{Name, Url}] array replacing repositories for set_repos"`
}

type LiveTVInput struct {
	Action      string `json:"action" jsonschema:"Live TV guide, tuner, recording, or timer action"`
	ChannelID   string `json:"channel_id,omitempty" jsonschema:"Channel ID (required for channel; optional filter for programs)"`
	RecordingID string `json:"recording_id,omitempty" jsonschema:"Recording ID for delete_recording"`
	TimerID     string `json:"timer_id,omitempty" jsonschema:"Timer ID for cancel_timer or cancel_series_timer"`
	ProgramID   string `json:"program_id,omitempty" jsonschema:"Program ID for create_timer or create_series_timer"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Max results (default 100 channels, 50 programs, 25 recommended/recordings)"`
	Confirm     *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete_recording, cancel_timer, or cancel_series_timer; never set true before CONFIRMATION REQUIRED"`
}

// --- Metadata & Content ---

type MetadataInput struct {
	Action          string   `json:"action" jsonschema:"Metadata edit/lookup or multi-version video merge/split action"`
	ItemID          string   `json:"item_id,omitempty" jsonschema:"Hex item ID from search/browse (apply, update, editor_info, external_ids, split_versions)"`
	ItemIDs         []string `json:"item_ids,omitempty" jsonschema:"Hex item IDs from search/browse for batch_update (max 50, writes identical values) or merge_versions (2+)"`
	SearchType      string   `json:"search_type,omitempty" jsonschema:"Type for search"`
	SearchQuery     string   `json:"search_query,omitempty" jsonschema:"Title query for online provider search"`
	SearchYear      *int     `json:"search_year,omitempty" jsonschema:"Year filter for search"`
	ProviderName    string   `json:"provider_name,omitempty" jsonschema:"Key from search result provider_ids for apply (such as Tmdb, Imdb, Tvdb)"`
	ProviderID      string   `json:"provider_id,omitempty" jsonschema:"Provider ID value for apply"`
	Name            string   `json:"name,omitempty" jsonschema:"Title for update/batch_update"`
	Overview        string   `json:"overview,omitempty" jsonschema:"Overview for update/batch_update"`
	Genres          []string `json:"genres,omitempty" jsonschema:"Replaces full genre list for update/batch_update"`
	Tags            []string `json:"tags,omitempty" jsonschema:"Replaces full tag list for update/batch_update"`
	Studios         []string `json:"studios,omitempty" jsonschema:"Replaces full studio list for update/batch_update"`
	Year            *int     `json:"production_year,omitempty" jsonschema:"Production year for update/batch_update"`
	CommunityRating *float64 `json:"community_rating,omitempty" jsonschema:"Rating 0-10 for update/batch_update"`
	OfficialRating  string   `json:"official_rating,omitempty" jsonschema:"Content rating for update/batch_update"`
	SortName        string   `json:"sort_name,omitempty" jsonschema:"Sort title for update/batch_update"`
	LockedFields    []string `json:"locked_fields,omitempty" jsonschema:"Replaces locked fields list (such as Name, Overview, Genres)"`
	DryRun          *bool    `json:"dry_run,omitempty" jsonschema:"Preview update/batch_update without writing (default true for batch_update; set false to apply)"`
	Confirm         *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm apply, merge_versions, split_versions, or update/batch_update (dry_run=false); never set true before CONFIRMATION REQUIRED"`
}

type SubtitlesLyricsInput struct {
	Action            string   `json:"action" jsonschema:"Subtitle or lyric action"`
	ItemID            string   `json:"item_id,omitempty" jsonschema:"Hex item ID from search/browse (all actions except batch_download_subtitles)"`
	ItemIDs           []string `json:"item_ids,omitempty" jsonschema:"Hex item IDs from search/browse for batch_download_subtitles (max 25)"`
	Language          string   `json:"language,omitempty" jsonschema:"Language code for search/batch_download_subtitles (default en)"`
	SubtitleID        string   `json:"subtitle_id,omitempty" jsonschema:"Subtitle ID for download_subtitle"`
	LyricID           string   `json:"lyric_id,omitempty" jsonschema:"Lyric ID for download_lyrics"`
	SubtitleIndex     *int     `json:"subtitle_index,omitempty" jsonschema:"External subtitle stream index for delete_subtitle"`
	SubtitleData      string   `json:"subtitle_data,omitempty" jsonschema:"Base64 subtitle content for upload_subtitle (at most 20 MiB)"`
	SubtitleFormat    string   `json:"subtitle_format,omitempty" jsonschema:"Format for upload_subtitle"`
	SubtitleLanguage  string   `json:"subtitle_language,omitempty" jsonschema:"3-letter ISO language code for upload_subtitle (default eng)"`
	IsForced          *bool    `json:"is_forced,omitempty" jsonschema:"Forced flag for upload_subtitle"`
	IsHearingImpaired *bool    `json:"is_hearing_impaired,omitempty" jsonschema:"SDH flag for upload_subtitle"`
	Confirm           *bool    `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete_subtitle, delete_lyrics, or batch_download_subtitles; never set true before CONFIRMATION REQUIRED"`
}

type ImagesInput struct {
	Action     string `json:"action" jsonschema:"Image action"`
	ItemID     string `json:"item_id" jsonschema:"Item ID"`
	ImageType  string `json:"image_type,omitempty" jsonschema:"Image type (default Primary)"`
	Provider   string `json:"provider_name,omitempty" jsonschema:"Provider filter for remote_list"`
	ImageURL   string `json:"image_url,omitempty" jsonschema:"Source URL for remote_download"`
	ImageIndex *int   `json:"image_index,omitempty" jsonschema:"0-based image index for get_url"`
	ImageData  string `json:"image_data,omitempty" jsonschema:"Base64 image data for upload (JPEG/PNG/WebP/GIF/BMP, at most 20 MiB)"`
}

type DevicesInput struct {
	Action   string `json:"action" jsonschema:"Device or API key action"`
	DeviceID string `json:"device_id,omitempty" jsonschema:"Device ID for get or delete"`
	Key      string `json:"key,omitempty" jsonschema:"Full API key token for revoke_api_key (prefer app_name)"`
	AppName  string `json:"app_name,omitempty" jsonschema:"App name for create_api_key or revoke_api_key"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max devices for list (default 50)"`
	Days     int    `json:"days,omitempty" jsonschema:"Active within N days for list (default 30)"`
	Confirm  *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm delete, create_api_key, or revoke_api_key; never set true before CONFIRMATION REQUIRED"`
}

// --- Server Management ---

type ServerManageInput struct {
	Action   string `json:"action" jsonschema:"Config, backup, scheduled task, or server power action"`
	Key      string `json:"key,omitempty" jsonschema:"Section key (such as encoding for transcoding, network, branding, metadata, livetv)"`
	Config   any    `json:"config,omitempty" jsonschema:"Fields merged into section for update_config_section (omit redacted livetv values)"`
	FileName string `json:"file_name,omitempty" jsonschema:"Backup file name or path for backup_manifest/restore_backup"`
	TaskID   string `json:"task_id,omitempty" jsonschema:"Task ID from list_tasks for get_task, start_task, stop_task, set_task_triggers"`
	Triggers any    `json:"triggers,omitempty" jsonschema:"Trigger array replacing task triggers (DailyTrigger/WeeklyTrigger/IntervalTrigger/StartupTrigger; times in ticks)"`
	Confirm  *bool  `json:"confirm,omitempty" jsonschema:"Leave unset so the user is asked to confirm update_config_section, restore_backup, set_task_triggers, restart, or shutdown; never set true before CONFIRMATION REQUIRED"`
}

// --- Item Extras ---

type ItemExtrasInput struct {
	Action string `json:"action" jsonschema:"Extra, playback diagnostic, or web download_link"`
	ItemID string `json:"item_id" jsonschema:"Item ID"`
}

// --- Analytics & Playback Statistics ---

type AnalyticsInput struct {
	Action   string `json:"action" jsonschema:"Library report action"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Library name or ID"`
	Type     string `json:"type,omitempty" jsonschema:"Item type filter (defaults: Movie for codec_report/duplicate_check, Series for size_report, Movie,Episode for never_played)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max results (default 50 for never_played/recently_added, 25 for size_report)"`
	Days     int    `json:"days,omitempty" jsonschema:"Lookback days for recently_added (default 30)"`
	UserID   string `json:"user_id,omitempty" jsonschema:"Username or user ID for never_played (defaults to current user)"`
}

type PlaybackStatsInput struct {
	Action       string `json:"action" jsonschema:"Playback statistics view (playback_history = user watch history; played_status = per-user watch status for one item; most_played = popular items across household users; unwatched_by_all = items unwatched by every enabled user)"`
	UserID       string `json:"user_id,omitempty" jsonschema:"Username or user ID for playback_history (defaults to current user)"`
	ItemID       string `json:"item_id,omitempty" jsonschema:"Item ID or title for played_status"`
	ParentID     string `json:"parent_id,omitempty" jsonschema:"Library name or ID for playback_history, most_played, or unwatched_by_all"`
	Type         string `json:"type,omitempty" jsonschema:"Item type filter (Movie, Series, Episode, Audio, MusicVideo, Video; defaults to Movie for most_played/unwatched_by_all and playable leaf items for playback_history)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max results: playback_history/unwatched_by_all (default 50), most_played (default 25), max 500"`
	VerifiedOnly *bool  `json:"verified_only,omitempty" jsonschema:"For playback_history: require activity-log playback event (default true)"`
}
