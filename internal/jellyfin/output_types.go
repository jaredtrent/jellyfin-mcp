package jellyfin

// The structs in this file are the results of the typed tools. The SDK
// derives each tool's output schema from them, sends them as the call's
// structured content, and writes the text content as their JSON, so a field
// here is the one place a fact is stated.
//
// Facts are fields; guidance is a note. Counts and lists are present even when
// they are zero or empty, so an empty result never looks like the zero value
// an error would carry. A struct that serves several actions of one tool
// declares each action's fields as pointers with omitempty: an action's own
// list is present, as [] when empty, and the other actions' fields are absent.

type MediaItem struct {
	ID                string  `json:"id" jsonschema:"Item ID"`
	Name              string  `json:"name" jsonschema:"Item name"`
	Type              string  `json:"type" jsonschema:"Item type, such as Movie, Series, Episode, or Audio"`
	Year              int     `json:"year,omitempty" jsonschema:"Production year"`
	Overview          string  `json:"overview,omitempty" jsonschema:"Overview, truncated to 200 characters"`
	CommunityRating   float64 `json:"community_rating,omitempty" jsonschema:"Community rating out of 10"`
	OfficialRating    string  `json:"official_rating,omitempty" jsonschema:"Content rating, such as PG-13"`
	RuntimeMinutes    int64   `json:"runtime_minutes,omitempty" jsonschema:"Runtime in minutes"`
	Played            bool    `json:"played,omitempty" jsonschema:"True when the user finished the item at least once, or marked it played by hand"`
	Progress          string  `json:"progress,omitempty" jsonschema:"The position saved by the user's latest play, as a percentage; set beside played when a finished item is being watched again"`
	Favorite          bool    `json:"favorite,omitempty" jsonschema:"Whether the user marked the item a favorite"`
	SeriesName        string  `json:"series_name,omitempty" jsonschema:"Series name, for episodes and seasons"`
	IndexNumber       int     `json:"index_number,omitempty" jsonschema:"Episode or season number"`
	ParentIndexNumber int     `json:"parent_index_number,omitempty" jsonschema:"Season number, for episodes"`
	LastPlayed        string  `json:"last_played,omitempty" jsonschema:"When the user last played the item, in the server's time zone; present only in lists ordered by play date"`
	DateAdded         string  `json:"date_added,omitempty" jsonschema:"The day the item was added, in the server's time zone; present only in lists filtered by date added"`
	StartTime         string  `json:"start_time,omitempty" jsonschema:"When a Live TV program or recording starts, in the server's time zone"`
	EndTime           string  `json:"end_time,omitempty" jsonschema:"When a Live TV program or recording ends, in the server's time zone"`
	ChannelName       string  `json:"channel_name,omitempty" jsonschema:"The channel of a Live TV program or recording"`
}

// ItemListOutput is a page of items from jellyfin_search or jellyfin_browse.
type ItemListOutput struct {
	TotalCount int `json:"total_count" jsonschema:"How many items match"`
	// TotalIsLowerBound reports that the list was built from a bounded scan
	// that stopped before the end, so TotalCount counts only what it examined.
	TotalIsLowerBound bool        `json:"total_is_lower_bound,omitempty" jsonschema:"True when total_count is a lower bound because not every item was examined"`
	Shown             int         `json:"shown" jsonschema:"How many items this page holds"`
	NextStartIndex    *int        `json:"next_start_index,omitempty" jsonschema:"The start_index of the next page, present when more items match than are shown and paging is possible"`
	UndatedCount      int         `json:"undated_count,omitempty" jsonschema:"Items left out of a date-added filter because they have no readable creation date"`
	Items             []MediaItem `json:"items" jsonschema:"The items of this page"`
	Notes             []string    `json:"notes,omitempty" jsonschema:"Notes about the result: limits reached, filters that matched nothing, and what could not be read"`
}

// --- Libraries ---

type LibraryInfo struct {
	Name           string   `json:"name" jsonschema:"Library name"`
	CollectionType string   `json:"collection_type" jsonschema:"Collection type, such as movies, tvshows, music, or mixed"`
	ItemID         string   `json:"item_id" jsonschema:"The library's item ID, which parent_id and enabled_folder_ids take"`
	Paths          []string `json:"paths,omitempty" jsonschema:"Folders the library reads"`
}

type LibraryListOutput struct {
	Count     int           `json:"count" jsonschema:"How many libraries there are"`
	Libraries []LibraryInfo `json:"libraries" jsonschema:"The libraries"`
}

// --- Sessions ---

type NowPlayingInfo struct {
	ID             string `json:"id" jsonschema:"Item ID"`
	Name           string `json:"name" jsonschema:"Item name"`
	Type           string `json:"type" jsonschema:"Item type"`
	RuntimeMinutes int64  `json:"runtime_minutes,omitempty" jsonschema:"Runtime in minutes"`
}

type PlayStateInfo struct {
	IsPaused        bool  `json:"is_paused" jsonschema:"Whether playback is paused"`
	PositionSeconds int64 `json:"position_seconds,omitempty" jsonschema:"Playback position in seconds"`
	PositionTicks   int64 `json:"position_ticks,omitempty" jsonschema:"Playback position in ticks, the value jellyfin_playback_control Seek takes"`
	Volume          int   `json:"volume,omitempty" jsonschema:"Volume level from 0 to 100"`
}

type SessionInfo struct {
	SessionID            string          `json:"session_id" jsonschema:"Session ID, which jellyfin_playback_control and jellyfin_play take"`
	User                 string          `json:"user" jsonschema:"The signed-in user"`
	Client               string          `json:"client" jsonschema:"Client application"`
	DeviceName           string          `json:"device_name" jsonschema:"Device name"`
	SupportsMediaControl bool            `json:"supports_media_control" jsonschema:"Whether the client accepts remote playback commands from jellyfin_play and jellyfin_playback_control"`
	LastActivity         string          `json:"last_activity" jsonschema:"Last activity, in the server's time zone"`
	Status               string          `json:"status" jsonschema:"playing when the session is playing an item, otherwise connected"`
	NowPlaying           *NowPlayingInfo `json:"now_playing,omitempty" jsonschema:"The item being played"`
	PlayState            *PlayStateInfo  `json:"play_state,omitempty" jsonschema:"Playback state of the item being played"`
}

// SessionsOutput serves both jellyfin_sessions actions: list fills Sessions
// and resume fills Resume, each present even when empty, and the other absent.
type SessionsOutput struct {
	Sessions *[]SessionInfo `json:"sessions,omitempty" jsonschema:"Connected sessions, for action list"`
	Resume   *[]MediaItem   `json:"resume,omitempty" jsonschema:"Items with playback in progress, most recently played first, for action resume"`
	Notes    []string       `json:"notes,omitempty" jsonschema:"Notes about the result, such as more items matching than are shown"`
}

// --- TV Shows ---

type SeasonInfo struct {
	ID           string `json:"id" jsonschema:"Season ID"`
	Name         string `json:"name" jsonschema:"Season name"`
	SeasonNumber int    `json:"season_number" jsonschema:"Season number"`
	EpisodeCount int    `json:"episode_count" jsonschema:"How many episodes the season holds"`
}

type EpisodeInfo struct {
	ID              string  `json:"id" jsonschema:"Episode ID"`
	Name            string  `json:"name" jsonschema:"Episode name"`
	SeasonNumber    int     `json:"season_number,omitempty" jsonschema:"Season number"`
	EpisodeNumber   int     `json:"episode_number,omitempty" jsonschema:"Episode number within the season"`
	Overview        string  `json:"overview,omitempty" jsonschema:"Overview, truncated to 150 characters"`
	CommunityRating float64 `json:"community_rating,omitempty" jsonschema:"Community rating out of 10"`
	RuntimeMinutes  int64   `json:"runtime_minutes,omitempty" jsonschema:"Runtime in minutes"`
	Played          bool    `json:"played,omitempty" jsonschema:"True when the user finished the episode at least once, or marked it played by hand"`
	Progress        string  `json:"progress,omitempty" jsonschema:"The position saved by the user's latest play, as a percentage; set beside played when a finished episode is being watched again"`
	PremiereDate    string  `json:"premiere_date,omitempty" jsonschema:"Air date, as YYYY-MM-DD"`
}

// TVShowsOutput serves the three jellyfin_tv_shows actions: seasons fills
// Seasons, episodes fills Episodes, and next_up fills NextUp, each present
// even when empty, and the others absent.
type TVShowsOutput struct {
	Seasons  *[]SeasonInfo  `json:"seasons,omitempty" jsonschema:"The series' seasons, for action seasons"`
	Episodes *[]EpisodeInfo `json:"episodes,omitempty" jsonschema:"The season's episodes, for action episodes"`
	NextUp   *[]MediaItem   `json:"next_up,omitempty" jsonschema:"The next episodes to watch, for action next_up"`
	Notes    []string       `json:"notes,omitempty" jsonschema:"Notes about the result, such as more items matching than are shown"`
}

// --- Recommendations ---

// RecommendationCategory is one of the groups jellyfin_recommendations
// movie_recs returns, such as movies similar to a recently played one.
type RecommendationCategory struct {
	Type  string      `json:"recommendation_type" jsonschema:"Why these movies are recommended, as Jellyfin names it, such as SimilarToRecentlyPlayed or HasDirectorFromRecentlyPlayed"`
	Items []MediaItem `json:"items" jsonschema:"The recommended movies"`
}

// RecommendationsOutput serves every jellyfin_recommendations action. Items is
// present for every action but movie_recs, which fills Categories instead;
// TotalCount is present for recently_played only.
type RecommendationsOutput struct {
	Items      *[]MediaItem              `json:"items,omitempty" jsonschema:"The recommended items, for every action but movie_recs"`
	Categories *[]RecommendationCategory `json:"categories,omitempty" jsonschema:"Recommended movies grouped by reason, for action movie_recs; a movie appears in the first group that recommends it"`
	TotalCount *int                      `json:"total_count,omitempty" jsonschema:"How many items are marked played, for action recently_played"`
	Notes      []string                  `json:"notes,omitempty" jsonschema:"Notes about the result"`
}

// --- Detailed Item ---

type PersonInfo struct {
	Name string `json:"name" jsonschema:"Person's name"`
	Type string `json:"type" jsonschema:"Role type, such as Actor, Director, or Writer"`
	Role string `json:"role,omitempty" jsonschema:"Character or job"`
}

// ChapterInfo is a chapter of an item: its name, and where it starts as a
// clock time and in ticks.
type ChapterInfo struct {
	Name       string `json:"name" jsonschema:"Chapter name"`
	Start      string `json:"start" jsonschema:"Where the chapter starts, as h:mm:ss"`
	StartTicks int64  `json:"start_ticks" jsonschema:"Where the chapter starts in ticks, the value jellyfin_playback_control Seek takes"`
}

// CollectionRef names a collection that includes an item.
type CollectionRef struct {
	ID   string `json:"id" jsonschema:"Collection ID"`
	Name string `json:"name" jsonschema:"Collection name"`
}

type UserDataInfo struct {
	Played           bool    `json:"played" jsonschema:"True when the user finished the item at least once, or marked it played by hand"`
	Favorite         bool    `json:"favorite" jsonschema:"Whether the user marked the item a favorite"`
	PlayCount        int     `json:"play_count,omitempty" jsonschema:"How many times the user played the item"`
	PlayedPercentage float64 `json:"played_percentage,omitempty" jsonschema:"The position saved by the user's latest play, as a percentage"`
	LastPlayed       string  `json:"last_played,omitempty" jsonschema:"The day the user last played the item, in the server's time zone"`
}

type AudioStreamInfo struct {
	Index        int    `json:"index" jsonschema:"Stream index"`
	Codec        string `json:"codec" jsonschema:"Audio codec"`
	Channels     int    `json:"channels,omitempty" jsonschema:"Channel count"`
	Language     string `json:"language,omitempty" jsonschema:"Language code as Jellyfin stores it, such as eng"`
	DisplayTitle string `json:"display_title,omitempty" jsonschema:"Title Jellyfin shows for the track"`
	IsDefault    bool   `json:"is_default,omitempty" jsonschema:"Whether this is the default track"`
}

type SubtitleStreamInfo struct {
	Index        int    `json:"index" jsonschema:"Stream index"`
	Codec        string `json:"codec" jsonschema:"Subtitle format"`
	Language     string `json:"language,omitempty" jsonschema:"Language code as Jellyfin stores it, such as eng"`
	DisplayTitle string `json:"display_title,omitempty" jsonschema:"Title Jellyfin shows for the track"`
	IsExternal   bool   `json:"is_external,omitempty" jsonschema:"Whether the track is a separate file"`
	IsDefault    bool   `json:"is_default,omitempty" jsonschema:"Whether this is the default track"`
	IsForced     bool   `json:"is_forced,omitempty" jsonschema:"Whether the track is forced"`
}

type MediaSourceInfo struct {
	Container       string               `json:"container" jsonschema:"Container format"`
	Path            string               `json:"path,omitempty" jsonschema:"File path on the server"`
	BitrateKbps     int64                `json:"bitrate_kbps,omitempty" jsonschema:"Overall bitrate in kbps"`
	SizeMB          int64                `json:"size_mb,omitempty" jsonschema:"File size in megabytes"`
	VideoCodec      string               `json:"video_codec,omitempty" jsonschema:"Video codec"`
	Resolution      string               `json:"resolution,omitempty" jsonschema:"Frame size as width x height"`
	VideoProfile    string               `json:"video_profile,omitempty" jsonschema:"Codec profile"`
	VideoBitDepth   int                  `json:"video_bit_depth,omitempty" jsonschema:"Bit depth"`
	VideoRange      string               `json:"video_range,omitempty" jsonschema:"SDR or HDR"`
	VideoRangeType  string               `json:"video_range_type,omitempty" jsonschema:"HDR format, such as HDR10, DOVI, or HLG, when Jellyfin knows it"`
	AudioStreams    []AudioStreamInfo    `json:"audio_streams,omitempty" jsonschema:"Audio tracks"`
	SubtitleStreams []SubtitleStreamInfo `json:"subtitle_streams,omitempty" jsonschema:"Subtitle tracks"`
}

type DetailedItemOutput struct {
	ID               string            `json:"id" jsonschema:"Item ID"`
	Name             string            `json:"name" jsonschema:"Item name"`
	Type             string            `json:"type" jsonschema:"Item type"`
	Year             int               `json:"year,omitempty" jsonschema:"Production year"`
	Overview         string            `json:"overview,omitempty" jsonschema:"Full overview"`
	CommunityRating  float64           `json:"community_rating,omitempty" jsonschema:"Community rating out of 10"`
	OfficialRating   string            `json:"official_rating,omitempty" jsonschema:"Content rating, such as PG-13"`
	CriticRating     float64           `json:"critic_rating,omitempty" jsonschema:"Critic rating out of 100"`
	RuntimeMinutes   int64             `json:"runtime_minutes,omitempty" jsonschema:"Runtime in minutes"`
	DateAdded        string            `json:"date_added,omitempty" jsonschema:"The day the item was added, in the server's time zone"`
	PremiereDate     string            `json:"premiere_date,omitempty" jsonschema:"Release or air date, as YYYY-MM-DD"`
	EndDate          string            `json:"end_date,omitempty" jsonschema:"End date, as YYYY-MM-DD, for ended series"`
	OriginalLanguage string            `json:"original_language,omitempty" jsonschema:"Original language, on Jellyfin 12 or later"`
	Taglines         []string          `json:"taglines,omitempty" jsonschema:"Taglines"`
	Genres           []string          `json:"genres" jsonschema:"Genres; empty when the item has none"`
	Tags             []string          `json:"tags" jsonschema:"Tags; empty when the item has none"`
	Studios          []string          `json:"studios" jsonschema:"Studios; empty when the item has none"`
	LockedFields     []string          `json:"locked_fields" jsonschema:"Fields locked against automatic metadata updates; empty when none are locked"`
	People           []PersonInfo      `json:"people,omitempty" jsonschema:"Cast and crew, at most 15; notes says when there are more"`
	ProviderIDs      map[string]string `json:"provider_ids,omitempty" jsonschema:"External IDs by provider, such as Imdb and Tmdb"`
	ExternalURLs     map[string]string `json:"external_urls,omitempty" jsonschema:"Links to the item's pages on IMDb, TMDb, and TVDB"`
	UserData         *UserDataInfo     `json:"user_data,omitempty" jsonschema:"The user's play state for the item"`
	FilePath         string            `json:"file_path,omitempty" jsonschema:"File path on the server"`
	MediaSources     []MediaSourceInfo `json:"media_sources,omitempty" jsonschema:"Media files with their streams"`
	Chapters         []ChapterInfo     `json:"chapters,omitempty" jsonschema:"Chapters"`
	// IncludedInCollections is set on Jellyfin 12 and later, as an empty list
	// for an item in no collection; it is absent when it could not be read.
	IncludedInCollections *[]CollectionRef `json:"included_in_collections,omitempty" jsonschema:"Collections that include the item, on Jellyfin 12 or later; absent when they could not be read"`
	Artists               []string         `json:"artists,omitempty" jsonschema:"Artists, for music"`
	Album                 string           `json:"album,omitempty" jsonschema:"Album, for music"`
	Status                string           `json:"status,omitempty" jsonschema:"Continuing or Ended, for series"`
	HasSubtitles          bool             `json:"has_subtitles,omitempty" jsonschema:"Whether the item has subtitles"`
	HasLyrics             bool             `json:"has_lyrics,omitempty" jsonschema:"Whether the item has lyrics"`
	ChildCount            int              `json:"child_count,omitempty" jsonschema:"How many direct children a container item has"`
	RecursiveItemCount    int              `json:"recursive_item_count,omitempty" jsonschema:"How many items a container item holds in total"`
	SeriesName            string           `json:"series_name,omitempty" jsonschema:"Series name, for episodes and seasons"`
	IndexNumber           int              `json:"index_number,omitempty" jsonschema:"Episode or season number"`
	ParentIndexNumber     int              `json:"parent_index_number,omitempty" jsonschema:"Season number, for episodes"`
	Played                bool             `json:"played,omitempty" jsonschema:"True when the user finished the item at least once, or marked it played by hand"`
	Progress              string           `json:"progress,omitempty" jsonschema:"The position saved by the user's latest play, as a percentage; set beside played when a finished item is being watched again"`
	Favorite              bool             `json:"favorite,omitempty" jsonschema:"Whether the user marked the item a favorite"`
	Notes                 []string         `json:"notes,omitempty" jsonschema:"Notes about the result, such as what could not be read"`
}

// --- Analytics ---

type TypeCount struct {
	Type  string `json:"type" jsonschema:"Item type"`
	Count int    `json:"count" jsonschema:"How many items of the type there are"`
}

type TypeSize struct {
	Type   string `json:"type" jsonschema:"Item type"`
	Files  int    `json:"files" jsonschema:"How many media files were summed"`
	SizeGB string `json:"size_gb" jsonschema:"Total size in gigabytes, to two decimals"`
	SizeMB int64  `json:"size_mb" jsonschema:"Total size in megabytes"`
}

type CodecDistribution struct {
	TotalMediaSources int            `json:"total_media_sources" jsonschema:"How many media files were examined"`
	VideoCodecs       map[string]int `json:"video_codecs" jsonschema:"Files per video codec"`
	AudioCodecs       map[string]int `json:"audio_codecs" jsonschema:"Audio tracks per codec"`
	Containers        map[string]int `json:"containers" jsonschema:"Files per container"`
	Resolutions       map[string]int `json:"resolutions" jsonschema:"Files per resolution class"`
	VideoRanges       map[string]int `json:"video_ranges" jsonschema:"Files per video range or HDR format"`
	BitDepths         map[string]int `json:"bit_depths" jsonschema:"Files per bit depth"`
}

type SizeEntry struct {
	Name   string `json:"name" jsonschema:"Item name"`
	ID     string `json:"id" jsonschema:"Item ID"`
	Type   string `json:"type" jsonschema:"Item type"`
	Files  int    `json:"files" jsonschema:"How many media files were summed"`
	SizeGB string `json:"size_gb" jsonschema:"Total size in gigabytes, to two decimals"`
	SizeMB int64  `json:"size_mb" jsonschema:"Total size in megabytes"`
}

type PlayedStatusUser struct {
	Name           string `json:"name" jsonschema:"User name"`
	Played         bool   `json:"played" jsonschema:"Whether Jellyfin marks the item played for this user"`
	PlayCount      int    `json:"play_count,omitempty" jsonschema:"How many times this user played the item"`
	LastPlayed     string `json:"last_played,omitempty" jsonschema:"The day this user last played the item, in the server's time zone"`
	EpisodesPlayed *int   `json:"episodes_played,omitempty" jsonschema:"How many of the series' episodes this user has played, for a series"`
	TotalEpisodes  int    `json:"total_episodes,omitempty" jsonschema:"How many episodes the series has, for a series"`
}

type DuplicateGroup struct {
	Name   string          `json:"name" jsonschema:"Title the copies share"`
	Year   int             `json:"year" jsonschema:"Production year the copies share; 0 when unknown"`
	Count  int             `json:"count" jsonschema:"How many copies there are"`
	Copies []DuplicateCopy `json:"copies" jsonschema:"The copies"`
}

type DuplicateCopy struct {
	ID   string `json:"id" jsonschema:"Item ID"`
	Name string `json:"name" jsonschema:"Item name"`
	Year int    `json:"year,omitempty" jsonschema:"Production year"`
	Path string `json:"path,omitempty" jsonschema:"File path on the server"`
}

type PlayedStatusItem struct {
	ID            string `json:"id" jsonschema:"Item ID"`
	Name          string `json:"name" jsonschema:"Item name"`
	Type          string `json:"type" jsonschema:"Item type"`
	TotalEpisodes int    `json:"total_episodes,omitempty" jsonschema:"How many episodes the series has, for a series"`
}

// AnalyticsOutput serves every jellyfin_analytics action. Each action fills
// its own fields, listed by action below; its lists are present even when
// empty, and the other actions' fields are absent.
type AnalyticsOutput struct {
	// library_stats
	Stats *[]TypeCount `json:"stats,omitempty" jsonschema:"Item counts by type, for action library_stats; a type with no items is left out"`
	// library_stats and library_size: types whose figures could not be read.
	UnreadTypes []string `json:"unread_types,omitempty" jsonschema:"Types whose figures could not be read, each with the error; they are missing from the result, not zero"`
	// library_size
	TotalSizeGB string      `json:"total_size_gb,omitempty" jsonschema:"Total size in gigabytes, to two decimals, for action library_size"`
	TotalSizeMB *int64      `json:"total_size_mb,omitempty" jsonschema:"Total size in megabytes, for action library_size"`
	ByType      *[]TypeSize `json:"by_type,omitempty" jsonschema:"Sizes by type, for action library_size; a type with no media files is left out"`
	// size_report
	SizeReportType string       `json:"size_report_type,omitempty" jsonschema:"The item type the size report ranks, for action size_report"`
	SizeReport     *[]SizeEntry `json:"size_report,omitempty" jsonschema:"The largest items, largest first, for action size_report"`
	// codec_report
	CodecReport *CodecDistribution `json:"codec_report,omitempty" jsonschema:"Codec, container, resolution, and range counts, for action codec_report"`
	// never_played, recently_added
	Items      *[]MediaItem `json:"items,omitempty" jsonschema:"The items found, for actions never_played and recently_added"`
	TotalCount *int         `json:"total_count,omitempty" jsonschema:"How many items match, for actions never_played and recently_added"`
	// TotalIsLowerBound reports that recently_added stopped at its scan bound,
	// so TotalCount counts only the items it examined.
	TotalIsLowerBound bool `json:"total_is_lower_bound,omitempty" jsonschema:"True when total_count is a lower bound because not every item was examined"`
	Shown             *int `json:"shown,omitempty" jsonschema:"How many items are listed, for actions never_played and recently_added"`
	Days              int  `json:"days,omitempty" jsonschema:"The window in days, for action recently_added"`
	UndatedCount      int  `json:"undated_count,omitempty" jsonschema:"Items left out because they have no readable creation date, for action recently_added"`
	// duplicate_check
	Duplicates *[]DuplicateGroup `json:"duplicates,omitempty" jsonschema:"Titles with more than one copy, for action duplicate_check"`
	// played_status
	ItemSummary *PlayedStatusItem   `json:"item_summary,omitempty" jsonschema:"The item asked about, for action played_status"`
	Users       *[]PlayedStatusUser `json:"users,omitempty" jsonschema:"Each enabled user's play state for the item, for action played_status"`
	Notes       []string            `json:"notes,omitempty" jsonschema:"Notes about the result: limits reached and what could not be read"`
}
