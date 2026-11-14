package jellyfin

// Typed tool output structs. Counts and active action lists are always present
// (even when 0 or []), while inactive action pointer fields are omitted.

type MediaItem struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	Year              int     `json:"year,omitempty"`
	Overview          string  `json:"overview,omitempty"`
	CommunityRating   float64 `json:"community_rating,omitempty"`
	OfficialRating    string  `json:"official_rating,omitempty"`
	RuntimeMinutes    int64   `json:"runtime_minutes,omitempty"`
	Resolution        string  `json:"resolution,omitempty"`
	Played            bool    `json:"played,omitempty"`
	Progress          string  `json:"progress,omitempty" jsonschema:"Saved resume percentage"`
	Favorite          bool    `json:"favorite,omitempty"`
	SeriesName        string  `json:"series_name,omitempty"`
	IndexNumber       int     `json:"index_number,omitempty" jsonschema:"Episode or season number"`
	ParentIndexNumber int     `json:"parent_index_number,omitempty" jsonschema:"Season number"`
	LastPlayed        string  `json:"last_played,omitempty"`
	DateAdded         string  `json:"date_added,omitempty"`
	StartTime         string  `json:"start_time,omitempty"`
	EndTime           string  `json:"end_time,omitempty"`
	ChannelName       string  `json:"channel_name,omitempty"`
	MatchingEpisodes  int     `json:"matching_episodes,omitempty" jsonschema:"Matching episodes (group_by_series)"`
	TotalEpisodes     int     `json:"total_episodes,omitempty" jsonschema:"Total episodes on disk (group_by_series)"`
}

type ItemListOutput struct {
	TotalCount        int         `json:"total_count"`
	TotalIsLowerBound bool        `json:"total_is_lower_bound,omitempty" jsonschema:"True when scan stopped early"`
	Shown             int         `json:"shown"`
	NextStartIndex    *int        `json:"next_start_index,omitempty"`
	UndatedCount      int         `json:"undated_count,omitempty"`
	Items             []MediaItem `json:"items"`
	Notes             []string    `json:"notes,omitempty"`
}

// --- Libraries ---

type LibraryInfo struct {
	Name           string   `json:"name"`
	CollectionType string   `json:"collection_type"`
	ItemID         string   `json:"item_id"`
	Paths          []string `json:"paths,omitempty"`
}

type LibraryListOutput struct {
	Count     int           `json:"count"`
	Libraries []LibraryInfo `json:"libraries"`
}

// --- Sessions ---

type NowPlayingInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	RuntimeMinutes int64  `json:"runtime_minutes,omitempty"`
}

type PlayStateInfo struct {
	PlayMethod      string `json:"play_method,omitempty" jsonschema:"DirectPlay, DirectStream, or Transcode (check transcoding_info for remux)"`
	IsPaused        bool   `json:"is_paused"`
	PositionSeconds int64  `json:"position_seconds,omitempty"`
	PositionTicks   int64  `json:"position_ticks,omitempty" jsonschema:"Ticks for jellyfin_playback Seek"`
	Volume          int    `json:"volume,omitempty"`
}

type TranscodingInfo struct {
	VideoCodec       string   `json:"video_codec,omitempty"`
	AudioCodec       string   `json:"audio_codec,omitempty"`
	Container        string   `json:"container,omitempty"`
	BitrateKbps      int64    `json:"bitrate_kbps,omitempty"`
	IsVideoDirect    bool     `json:"is_video_direct"`
	IsAudioDirect    bool     `json:"is_audio_direct"`
	TranscodeReasons []string `json:"transcode_reasons,omitempty"`
}

type SessionInfo struct {
	SessionID            string           `json:"session_id"`
	User                 string           `json:"user"`
	Client               string           `json:"client"`
	AppVersion           string           `json:"app_version,omitempty"`
	DeviceName           string           `json:"device_name"`
	RemoteIP             string           `json:"remote_ip,omitempty"`
	SupportsMediaControl bool             `json:"supports_media_control"`
	LastActivity         string           `json:"last_activity"`
	Status               string           `json:"status"`
	NowPlaying           *NowPlayingInfo  `json:"now_playing,omitempty"`
	PlayState            *PlayStateInfo   `json:"play_state,omitempty"`
	TranscodingInfo      *TranscodingInfo `json:"transcoding_info,omitempty"`
}

type SessionsOutput struct {
	Sessions *[]SessionInfo `json:"sessions,omitempty"`
	Resume   *[]MediaItem   `json:"resume,omitempty"`
	Notes    []string       `json:"notes,omitempty"`
}

// --- TV Shows ---

type SeasonInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
}

type EpisodeInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	SeasonNumber    int     `json:"season_number,omitempty"`
	EpisodeNumber   int     `json:"episode_number,omitempty"`
	Overview        string  `json:"overview,omitempty"`
	CommunityRating float64 `json:"community_rating,omitempty"`
	RuntimeMinutes  int64   `json:"runtime_minutes,omitempty"`
	Played          bool    `json:"played,omitempty"`
	Progress        string  `json:"progress,omitempty"`
	PremiereDate    string  `json:"premiere_date,omitempty"`
}

type TVShowsOutput struct {
	Seasons  *[]SeasonInfo  `json:"seasons,omitempty"`
	Episodes *[]EpisodeInfo `json:"episodes,omitempty"`
	NextUp   *[]MediaItem   `json:"next_up,omitempty"`
	Notes    []string       `json:"notes,omitempty"`
}

// --- Recommendations ---

type RecommendationCategory struct {
	Type  string      `json:"recommendation_type"`
	Items []MediaItem `json:"items"`
}

type RecommendationsOutput struct {
	Items      *[]MediaItem              `json:"items,omitempty"`
	Categories *[]RecommendationCategory `json:"categories,omitempty"`
	TotalCount *int                      `json:"total_count,omitempty"`
	Notes      []string                  `json:"notes,omitempty"`
}

// --- Detailed Item ---

type PersonInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Role string `json:"role,omitempty"`
}

type ChapterInfo struct {
	Name       string `json:"name"`
	Start      string `json:"start"`
	StartTicks int64  `json:"start_ticks"`
}

type CollectionRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserDataInfo struct {
	Played           bool    `json:"played"`
	Favorite         bool    `json:"favorite"`
	PlayCount        int     `json:"play_count,omitempty"`
	PlayedPercentage float64 `json:"played_percentage,omitempty"`
	LastPlayed       string  `json:"last_played,omitempty"`
}

type AudioStreamInfo struct {
	Index        int    `json:"index"`
	Codec        string `json:"codec"`
	Channels     int    `json:"channels,omitempty"`
	Language     string `json:"language,omitempty"`
	DisplayTitle string `json:"display_title,omitempty"`
	IsDefault    bool   `json:"is_default,omitempty"`
}

type SubtitleStreamInfo struct {
	Index        int    `json:"index"`
	Codec        string `json:"codec"`
	Language     string `json:"language,omitempty"`
	DisplayTitle string `json:"display_title,omitempty"`
	IsExternal   bool   `json:"is_external,omitempty"`
	IsDefault    bool   `json:"is_default,omitempty"`
	IsForced     bool   `json:"is_forced,omitempty"`
}

type MediaSourceInfo struct {
	Container       string                `json:"container"`
	Path            string                `json:"path,omitempty"`
	BitrateKbps     int64                 `json:"bitrate_kbps,omitempty"`
	SizeMB          int64                 `json:"size_mb,omitempty"`
	VideoCodec      string                `json:"video_codec,omitempty"`
	Resolution      string                `json:"resolution,omitempty"`
	VideoProfile    string                `json:"video_profile,omitempty"`
	VideoBitDepth   int                   `json:"video_bit_depth,omitempty"`
	VideoRange      string                `json:"video_range,omitempty"`
	VideoRangeType  string                `json:"video_range_type,omitempty"`
	AudioStreams    []AudioStreamInfo     `json:"audio_streams,omitempty"`
	SubtitleStreams *[]SubtitleStreamInfo `json:"subtitle_streams,omitempty"`
}

type DetailedItemOutput struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Type                  string            `json:"type"`
	Year                  int               `json:"year,omitempty"`
	Overview              string            `json:"overview,omitempty"`
	CommunityRating       float64           `json:"community_rating,omitempty"`
	OfficialRating        string            `json:"official_rating,omitempty"`
	CriticRating          float64           `json:"critic_rating,omitempty"`
	RuntimeMinutes        int64             `json:"runtime_minutes,omitempty"`
	DateAdded             string            `json:"date_added,omitempty"`
	PremiereDate          string            `json:"premiere_date,omitempty"`
	EndDate               string            `json:"end_date,omitempty"`
	OriginalLanguage      string            `json:"original_language,omitempty"`
	Taglines              []string          `json:"taglines,omitempty"`
	Genres                []string          `json:"genres"`
	Tags                  []string          `json:"tags"`
	Studios               []string          `json:"studios"`
	LockedFields          []string          `json:"locked_fields"`
	People                []PersonInfo      `json:"people,omitempty"`
	ProviderIDs           map[string]string `json:"provider_ids,omitempty"`
	ExternalURLs          map[string]string `json:"external_urls,omitempty"`
	UserData              *UserDataInfo     `json:"user_data,omitempty"`
	FilePath              string            `json:"file_path,omitempty"`
	MediaSources          []MediaSourceInfo `json:"media_sources,omitempty"`
	Chapters              []ChapterInfo     `json:"chapters,omitempty"`
	IncludedInCollections *[]CollectionRef  `json:"included_in_collections,omitempty"`
	Artists               []string          `json:"artists,omitempty"`
	Album                 string            `json:"album,omitempty"`
	Status                string            `json:"status,omitempty"`
	HasSubtitles          *bool             `json:"has_subtitles,omitempty"`
	HasLyrics             *bool             `json:"has_lyrics,omitempty"`
	ChildCount            int               `json:"child_count,omitempty"`
	RecursiveItemCount    int               `json:"recursive_item_count,omitempty"`
	SeriesName            string            `json:"series_name,omitempty"`
	IndexNumber           int               `json:"index_number,omitempty"`
	ParentIndexNumber     int               `json:"parent_index_number,omitempty"`
	Played                bool              `json:"played,omitempty"`
	Progress              string            `json:"progress,omitempty"`
	Favorite              bool              `json:"favorite,omitempty"`
	Notes                 []string          `json:"notes,omitempty"`
}

// --- Analytics ---

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type TypeSize struct {
	Type   string `json:"type"`
	Files  int    `json:"files"`
	SizeGB string `json:"size_gb"`
	SizeMB int64  `json:"size_mb"`
}

type CodecDistribution struct {
	TotalMediaSources int            `json:"total_media_sources"`
	VideoCodecs       map[string]int `json:"video_codecs"`
	AudioCodecs       map[string]int `json:"audio_codecs"`
	Containers        map[string]int `json:"containers"`
	Resolutions       map[string]int `json:"resolutions"`
	VideoRanges       map[string]int `json:"video_ranges"`
	BitDepths         map[string]int `json:"bit_depths"`
}

type SizeEntry struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Type   string `json:"type"`
	Files  int    `json:"files"`
	SizeGB string `json:"size_gb"`
	SizeMB int64  `json:"size_mb"`
}

type PlayedStatusUser struct {
	Name           string `json:"name"`
	Played         bool   `json:"played"`
	PlayCount      int    `json:"play_count,omitempty"`
	LastPlayed     string `json:"last_played,omitempty"`
	EpisodesPlayed *int   `json:"episodes_played,omitempty"`
	TotalEpisodes  int    `json:"total_episodes,omitempty"`
}

type DuplicateGroup struct {
	Name   string          `json:"name"`
	Year   int             `json:"year"`
	Count  int             `json:"count"`
	Copies []DuplicateCopy `json:"copies"`
}

type DuplicateCopy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Year int    `json:"year,omitempty"`
	Path string `json:"path,omitempty"`
}

type PlayedStatusItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	TotalEpisodes int    `json:"total_episodes,omitempty"`
}

type AnalyticsOutput struct {
	Stats             *[]TypeCount       `json:"stats,omitempty"`
	UnreadTypes       []string           `json:"unread_types,omitempty" jsonschema:"Types that failed to read (omitted from counts, not zero)"`
	TotalSizeGB       string             `json:"total_size_gb,omitempty"`
	TotalSizeMB       *int64             `json:"total_size_mb,omitempty"`
	ByType            *[]TypeSize        `json:"by_type,omitempty"`
	SizeReportType    string             `json:"size_report_type,omitempty"`
	SizeReport        *[]SizeEntry       `json:"size_report,omitempty"`
	CodecReport       *CodecDistribution `json:"codec_report,omitempty"`
	Items             *[]MediaItem       `json:"items,omitempty"`
	TotalCount        *int               `json:"total_count,omitempty"`
	TotalIsLowerBound bool               `json:"total_is_lower_bound,omitempty"`
	Shown             *int               `json:"shown,omitempty"`
	Days              int                `json:"days,omitempty"`
	UndatedCount      int                `json:"undated_count,omitempty"`
	Duplicates        *[]DuplicateGroup  `json:"duplicates,omitempty"`
	Notes             []string           `json:"notes,omitempty"`
}

// --- Playback Statistics ---

type PlaybackHistoryEntry struct {
	MediaItem
	PlayCount      int   `json:"play_count,omitempty"`
	ActualPlayback *bool `json:"actual_playback,omitempty"`
}

type PopularItemEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Year        int    `json:"year,omitempty"`
	SeriesName  string `json:"series_name,omitempty"`
	UsersPlayed int    `json:"users_played"`
	TotalPlays  int    `json:"total_plays"`
	LastPlayed  string `json:"last_played,omitempty"`
}

type PlaybackStatsOutput struct {
	History     *[]PlaybackHistoryEntry `json:"history,omitempty"`
	ItemSummary *PlayedStatusItem       `json:"item_summary,omitempty"`
	Users       *[]PlayedStatusUser     `json:"users,omitempty"`
	MostPlayed  *[]PopularItemEntry     `json:"most_played,omitempty"`
	Unwatched   *[]MediaItem            `json:"unwatched,omitempty"`
	TotalCount  *int                    `json:"total_count,omitempty"`
	Shown       *int                    `json:"shown,omitempty"`
	Notes       []string                `json:"notes,omitempty"`
}
