package jellyfin

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MediaItemFrom
// ---------------------------------------------------------------------------

func TestMediaItemFrom(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name:   "nil input returns an item with empty identity fields",
			input:  nil,
			expect: map[string]any{"id": "", "name": "", "type": ""},
		},
		{
			name: "minimal item with only Id, Name, Type",
			input: map[string]any{
				"Id":   "abc123",
				"Name": "Test Movie",
				"Type": "Movie",
			},
			expect: map[string]any{
				"id":   "abc123",
				"name": "Test Movie",
				"type": "Movie",
			},
		},
		{
			name: "full item with all fields populated",
			input: map[string]any{
				"Id":                "movie-001",
				"Name":              "Lucid Horizon",
				"Type":              "Movie",
				"ProductionYear":    float64(2011),
				"Overview":          "A cartographer maps a city that rearranges itself every night.",
				"CommunityRating":   float64(7.3),
				"OfficialRating":    "PG-13",
				"RunTimeTicks":      float64(72600000000), // 121 minutes
				"SeriesName":        "MyShow",
				"IndexNumber":       float64(3),
				"ParentIndexNumber": float64(2),
				"UserData": map[string]any{
					"Played":           true,
					"PlayedPercentage": float64(85.0),
					"IsFavorite":       true,
				},
			},
			expect: map[string]any{
				"id":                  "movie-001",
				"name":                "Lucid Horizon",
				"type":                "Movie",
				"year":                2011,
				"overview":            "A cartographer maps a city that rearranges itself every night.",
				"community_rating":    float64(7.3),
				"official_rating":     "PG-13",
				"runtime_minutes":     int64(72600000000) / TicksPerMinute,
				"played":              true,
				"progress":            "85%",
				"favorite":            true,
				"series_name":         "MyShow",
				"index_number":        3,
				"parent_index_number": 2,
			},
		},
		{
			name: "missing optional fields do not appear in output",
			input: map[string]any{
				"Id":   "x",
				"Name": "X",
				"Type": "Series",
			},
			expect: map[string]any{
				"id":   "x",
				"name": "X",
				"type": "Series",
			},
		},
		{
			name: "overview truncated to OverviewMaxLen",
			input: map[string]any{
				"Id":       "trunc",
				"Name":     "Long Overview",
				"Type":     "Movie",
				"Overview": strings.Repeat("a", 300),
			},
			expect: map[string]any{
				"id":       "trunc",
				"name":     "Long Overview",
				"type":     "Movie",
				"overview": strings.Repeat("a", OverviewMaxLen),
			},
		},
		{
			name: "overview exactly at max length is not truncated",
			input: map[string]any{
				"Id":       "exact",
				"Name":     "Exact Len",
				"Type":     "Movie",
				"Overview": strings.Repeat("b", OverviewMaxLen),
			},
			expect: map[string]any{
				"id":       "exact",
				"name":     "Exact Len",
				"type":     "Movie",
				"overview": strings.Repeat("b", OverviewMaxLen),
			},
		},
		{
			name: "runtime computed from RunTimeTicks divided by TicksPerMinute",
			input: map[string]any{
				"Id":           "rt",
				"Name":         "Runtime Test",
				"Type":         "Movie",
				"RunTimeTicks": float64(5 * int64(TicksPerMinute)), // 5 minutes
			},
			expect: map[string]any{
				"id":              "rt",
				"name":            "Runtime Test",
				"type":            "Movie",
				"runtime_minutes": int64(5),
			},
		},
		{
			name: "zero RunTimeTicks omitted",
			input: map[string]any{
				"Id":           "rt0",
				"Name":         "Zero Runtime",
				"Type":         "Movie",
				"RunTimeTicks": float64(0),
			},
			expect: map[string]any{
				"id":   "rt0",
				"name": "Zero Runtime",
				"type": "Movie",
			},
		},
		{
			name: "UserData with Played only",
			input: map[string]any{
				"Id":   "ud1",
				"Name": "Played Only",
				"Type": "Movie",
				"UserData": map[string]any{
					"Played":     true,
					"IsFavorite": false,
				},
			},
			expect: map[string]any{
				"id":     "ud1",
				"name":   "Played Only",
				"type":   "Movie",
				"played": true,
			},
		},
		{
			name: "UserData with IsFavorite only",
			input: map[string]any{
				"Id":   "ud2",
				"Name": "Fav Only",
				"Type": "Movie",
				"UserData": map[string]any{
					"Played":     false,
					"IsFavorite": true,
				},
			},
			expect: map[string]any{
				"id":       "ud2",
				"name":     "Fav Only",
				"type":     "Movie",
				"favorite": true,
			},
		},
		{
			name: "UserData with PlayedPercentage zero omitted",
			input: map[string]any{
				"Id":   "ud3",
				"Name": "No Progress",
				"Type": "Movie",
				"UserData": map[string]any{
					"PlayedPercentage": float64(0),
				},
			},
			expect: map[string]any{
				"id":   "ud3",
				"name": "No Progress",
				"type": "Movie",
			},
		},
		{
			name: "series context fields",
			input: map[string]any{
				"Id":                "ep1",
				"Name":              "First Light",
				"Type":              "Episode",
				"SeriesName":        "Harbor Lights",
				"IndexNumber":       float64(1),
				"ParentIndexNumber": float64(1),
			},
			expect: map[string]any{
				"id":                  "ep1",
				"name":                "First Light",
				"type":                "Episode",
				"series_name":         "Harbor Lights",
				"index_number":        1,
				"parent_index_number": 1,
			},
		},
		{
			name: "zero CommunityRating omitted",
			input: map[string]any{
				"Id":              "cr0",
				"Name":            "No Rating",
				"Type":            "Movie",
				"CommunityRating": float64(0),
			},
			expect: map[string]any{
				"id":   "cr0",
				"name": "No Rating",
				"type": "Movie",
			},
		},
		{
			name: "empty overview omitted",
			input: map[string]any{
				"Id":       "ov0",
				"Name":     "No Overview",
				"Type":     "Movie",
				"Overview": "",
			},
			expect: map[string]any{
				"id":   "ov0",
				"name": "No Overview",
				"type": "Movie",
			},
		},
		{
			name: "empty OfficialRating omitted",
			input: map[string]any{
				"Id":             "or0",
				"Name":           "No Official Rating",
				"Type":           "Movie",
				"OfficialRating": "",
			},
			expect: map[string]any{
				"id":   "or0",
				"name": "No Official Rating",
				"type": "Movie",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEqual(t, MediaItemFrom(tc.input), tc.expect)
		})
	}
}

// ---------------------------------------------------------------------------
// DetailedItemFrom
// ---------------------------------------------------------------------------

func TestDetailedItemFrom(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name: "overview is NOT truncated in detailed view",
			input: map[string]any{
				"Id":       "det1",
				"Name":     "Full Overview",
				"Type":     "Movie",
				"Overview": strings.Repeat("x", 500),
			},
			expect: map[string]any{
				"id":       "det1",
				"name":     "Full Overview",
				"type":     "Movie",
				"overview": strings.Repeat("x", 500),
			},
		},
		{
			name: "dates truncated to DateOnlyLen",
			input: map[string]any{
				"Id":           "det2",
				"Name":         "Dated Item",
				"Type":         "Series",
				"PremiereDate": "2020-05-15T00:00:00.0000000Z",
				"EndDate":      "2023-12-31T23:59:59.0000000Z",
			},
			expect: map[string]any{
				"id":            "det2",
				"name":          "Dated Item",
				"type":          "Series",
				"premiere_date": "2020-05-15",
				"end_date":      "2023-12-31",
			},
		},
		{
			name: "CriticRating included",
			input: map[string]any{
				"Id":           "det3",
				"Name":         "Critic Rated",
				"Type":         "Movie",
				"CriticRating": float64(92),
			},
			expect: map[string]any{
				"id":            "det3",
				"name":          "Critic Rated",
				"type":          "Movie",
				"critic_rating": float64(92),
			},
		},
		{
			name: "zero CriticRating omitted",
			input: map[string]any{
				"Id":           "det3b",
				"Name":         "No Critic",
				"Type":         "Movie",
				"CriticRating": float64(0),
			},
			expect: map[string]any{
				"id":   "det3b",
				"name": "No Critic",
				"type": "Movie",
			},
		},
		{
			name: "taglines, genres, artists extracted via ToStringSlice",
			input: map[string]any{
				"Id":       "det4",
				"Name":     "Sliced",
				"Type":     "Movie",
				"Taglines": []any{"Nothing stays where you left it"},
				"Genres":   []any{"Action", "Sci-Fi"},
				"Artists":  []any{"Elara Voss"},
			},
			expect: map[string]any{
				"id":       "det4",
				"name":     "Sliced",
				"type":     "Movie",
				"taglines": []string{"Nothing stays where you left it"},
				"genres":   []string{"Action", "Sci-Fi"},
				"artists":  []string{"Elara Voss"},
			},
		},
		{
			name: "studios extracted from objects with Name field",
			input: map[string]any{
				"Id":   "det5",
				"Name": "Studio Test",
				"Type": "Movie",
				"Studios": []any{
					map[string]any{"Name": "Ridgeline Studios"},
					map[string]any{"Name": "Emberlight Films"},
				},
			},
			expect: map[string]any{
				"id":      "det5",
				"name":    "Studio Test",
				"type":    "Movie",
				"studios": []string{"Ridgeline Studios", "Emberlight Films"},
			},
		},
		{
			name: "empty studios list serializes as []",
			input: map[string]any{
				"Id":      "det5b",
				"Name":    "No Studios",
				"Type":    "Movie",
				"Studios": []any{},
			},
			expect: map[string]any{
				"id":   "det5b",
				"name": "No Studios",
				"type": "Movie",
			},
		},
		{
			name: "people capped at MaxPeopleInDetail",
			input: func() map[string]any {
				people := make([]any, 20)
				for i := 0; i < 20; i++ {
					people[i] = map[string]any{
						"Name": "Person " + strings.Repeat("A", i),
						"Type": "Actor",
						"Role": "Character",
					}
				}
				return map[string]any{
					"Id":     "det6",
					"Name":   "Many People",
					"Type":   "Movie",
					"People": people,
				}
			}(),
			expect: func() map[string]any {
				pl := make([]map[string]any, MaxPeopleInDetail)
				for i := 0; i < MaxPeopleInDetail; i++ {
					pl[i] = map[string]any{
						"name": "Person " + strings.Repeat("A", i),
						"type": "Actor",
						"role": "Character",
					}
				}
				return map[string]any{
					"id":     "det6",
					"name":   "Many People",
					"type":   "Movie",
					"people": pl,
					"notes":  []any{fmt.Sprintf("people lists the first %d of 20 cast and crew. To check whether someone else appears in this item, use jellyfin_browse with person set to their name.", MaxPeopleInDetail)},
				}
			}(),
		},
		{
			name: "people with fewer than max are all included",
			input: map[string]any{
				"Id":   "det6b",
				"Name": "Few People",
				"Type": "Movie",
				"People": []any{
					map[string]any{"Name": "Alice", "Type": "Actor", "Role": "Hero"},
					map[string]any{"Name": "Bob", "Type": "Director"},
				},
			},
			expect: map[string]any{
				"id":   "det6b",
				"name": "Few People",
				"type": "Movie",
				"people": []map[string]any{
					{"name": "Alice", "type": "Actor", "role": "Hero"},
					{"name": "Bob", "type": "Director"},
				},
			},
		},
		{
			name: "person with empty role omits role field",
			input: map[string]any{
				"Id":   "det6c",
				"Name": "No Role",
				"Type": "Movie",
				"People": []any{
					map[string]any{"Name": "Charlie", "Type": "Writer", "Role": ""},
				},
			},
			expect: map[string]any{
				"id":   "det6c",
				"name": "No Role",
				"type": "Movie",
				"people": []map[string]any{
					{"name": "Charlie", "type": "Writer"},
				},
			},
		},
		{
			name: "provider IDs and external URLs for Movie",
			input: map[string]any{
				"Id":   "det7",
				"Name": "Provider Test",
				"Type": "Movie",
				"ProviderIds": map[string]any{
					"Imdb": "tt1234567",
					"Tmdb": "12345",
					"Tvdb": "67890",
				},
			},
			expect: map[string]any{
				"id":   "det7",
				"name": "Provider Test",
				"type": "Movie",
				"provider_ids": map[string]string{
					"Imdb": "tt1234567",
					"Tmdb": "12345",
					"Tvdb": "67890",
				},
				"external_urls": map[string]string{
					"IMDb": "https://www.imdb.com/title/tt1234567",
					"TMDb": "https://www.themoviedb.org/movie/12345",
					"TVDB": "https://thetvdb.com/?id=67890&tab=movie",
				},
			},
		},
		{
			name: "provider URLs for Series use tv segment",
			input: map[string]any{
				"Id":   "det7b",
				"Name": "Series Providers",
				"Type": "Series",
				"ProviderIds": map[string]any{
					"Tmdb": "999",
				},
			},
			expect: map[string]any{
				"id":   "det7b",
				"name": "Series Providers",
				"type": "Series",
				"provider_ids": map[string]string{
					"Tmdb": "999",
				},
				"external_urls": map[string]string{
					"TMDb": "https://www.themoviedb.org/tv/999",
				},
			},
		},
		{
			name: "empty ProviderIds omitted",
			input: map[string]any{
				"Id":          "det7c",
				"Name":        "No Providers",
				"Type":        "Movie",
				"ProviderIds": map[string]any{},
			},
			expect: map[string]any{
				"id":   "det7c",
				"name": "No Providers",
				"type": "Movie",
			},
		},
		{
			name: "UserData with PlayCount and LastPlayedDate",
			input: map[string]any{
				"Id":   "det8",
				"Name": "User Data Detail",
				"Type": "Movie",
				"UserData": map[string]any{
					"Played":           true,
					"IsFavorite":       false,
					"PlayCount":        float64(3),
					"PlayedPercentage": float64(100),
					"LastPlayedDate":   "2024-06-15T14:30:00.0000000Z",
				},
			},
			expect: map[string]any{
				"id":       "det8",
				"name":     "User Data Detail",
				"type":     "Movie",
				"played":   true,
				"progress": "100%",
				"user_data": map[string]any{
					"played":            true,
					"favorite":          false,
					"play_count":        3,
					"played_percentage": float64(100),
					"last_played":       "2024-06-15",
				},
			},
		},
		{
			name: "UserData with zero PlayCount omits play_count",
			input: map[string]any{
				"Id":   "det8b",
				"Name": "Zero PlayCount",
				"Type": "Movie",
				"UserData": map[string]any{
					"Played":     false,
					"IsFavorite": false,
					"PlayCount":  float64(0),
				},
			},
			expect: map[string]any{
				"id":   "det8b",
				"name": "Zero PlayCount",
				"type": "Movie",
				"user_data": map[string]any{
					"played":   false,
					"favorite": false,
				},
			},
		},
		{
			name: "MediaSources with video, audio, and subtitle streams",
			input: map[string]any{
				"Id":   "det9",
				"Name": "Media Sources Test",
				"Type": "Movie",
				"MediaSources": []any{
					map[string]any{
						"Container": "mkv",
						"Path":      "/movies/test.mkv",
						"Bitrate":   float64(5000000),
						"Size":      float64(4194304000),
						"MediaStreams": []any{
							map[string]any{
								"Type":       "Video",
								"Codec":      "hevc",
								"Width":      float64(3840),
								"Height":     float64(2160),
								"Profile":    "Main 10",
								"BitDepth":   float64(10),
								"VideoRange": "HDR",
							},
							map[string]any{
								"Type":         "Audio",
								"Index":        float64(1),
								"Codec":        "truehd",
								"Channels":     float64(8),
								"Language":     "eng",
								"DisplayTitle": "TrueHD 7.1",
								"IsDefault":    true,
							},
							map[string]any{
								"Type":         "Audio",
								"Index":        float64(2),
								"Codec":        "aac",
								"Channels":     float64(2),
								"Language":     "spa",
								"DisplayTitle": "AAC Stereo",
								"IsDefault":    false,
							},
							map[string]any{
								"Type":         "Subtitle",
								"Index":        float64(3),
								"Codec":        "srt",
								"Language":     "eng",
								"DisplayTitle": "English",
								"IsExternal":   true,
								"IsDefault":    true,
								"IsForced":     false,
							},
							map[string]any{
								"Type":         "Subtitle",
								"Index":        float64(4),
								"Codec":        "ass",
								"Language":     "jpn",
								"DisplayTitle": "Japanese",
								"IsExternal":   false,
								"IsDefault":    false,
								"IsForced":     true,
							},
						},
					},
				},
			},
			expect: map[string]any{
				"id":   "det9",
				"name": "Media Sources Test",
				"type": "Movie",
				"media_sources": []map[string]any{
					{
						"container":       "mkv",
						"path":            "/movies/test.mkv",
						"bitrate_kbps":    int64(5000000) / UnitsPerKilo,
						"size_mb":         int64(4194304000) / BytesPerMB,
						"video_codec":     "hevc",
						"resolution":      "3840x2160",
						"video_profile":   "Main 10",
						"video_bit_depth": 10,
						"video_range":     "HDR",
						"audio_streams": []map[string]any{
							{
								"index":         1,
								"codec":         "truehd",
								"channels":      8,
								"language":      "eng",
								"display_title": "TrueHD 7.1",
								"is_default":    true,
							},
							{
								"index":         2,
								"codec":         "aac",
								"channels":      2,
								"language":      "spa",
								"display_title": "AAC Stereo",
							},
						},
						"subtitle_streams": []map[string]any{
							{
								"index":         3,
								"codec":         "srt",
								"language":      "eng",
								"display_title": "English",
								"is_external":   true,
								"is_default":    true,
							},
							{
								"index":         4,
								"codec":         "ass",
								"language":      "jpn",
								"display_title": "Japanese",
								"is_forced":     true,
							},
						},
					},
				},
			},
		},
		{
			name: "MediaSources with no streams",
			input: map[string]any{
				"Id":   "det9b",
				"Name": "No Streams",
				"Type": "Movie",
				"MediaSources": []any{
					map[string]any{
						"Container": "mp4",
					},
				},
			},
			expect: map[string]any{
				"id":   "det9b",
				"name": "No Streams",
				"type": "Movie",
				"media_sources": []map[string]any{
					{
						"container": "mp4",
					},
				},
			},
		},
		{
			// BaseItemDto has no FileName property, so a stray key of that
			// name is not read.
			name: "file path without file name",
			input: map[string]any{
				"Id":       "det10",
				"Name":     "File Path Test",
				"Type":     "Movie",
				"Path":     "/media/movies/test.mkv",
				"FileName": "test.mkv",
			},
			expect: map[string]any{
				"id":        "det10",
				"name":      "File Path Test",
				"type":      "Movie",
				"file_path": "/media/movies/test.mkv",
			},
		},
		{
			name: "album field",
			input: map[string]any{
				"Id":    "det11",
				"Name":  "Track Name",
				"Type":  "Audio",
				"Album": "My Album",
			},
			expect: map[string]any{
				"id":    "det11",
				"name":  "Track Name",
				"type":  "Audio",
				"album": "My Album",
			},
		},
		{
			name: "status field for series",
			input: map[string]any{
				"Id":     "det12",
				"Name":   "Ongoing Show",
				"Type":   "Series",
				"Status": "Continuing",
			},
			expect: map[string]any{
				"id":     "det12",
				"name":   "Ongoing Show",
				"type":   "Series",
				"status": "Continuing",
			},
		},
		{
			name: "has_subtitles and has_lyrics flags",
			input: map[string]any{
				"Id":           "det13",
				"Name":         "Subtitled",
				"Type":         "Movie",
				"HasSubtitles": true,
				"HasLyrics":    true,
			},
			expect: map[string]any{
				"id":            "det13",
				"name":          "Subtitled",
				"type":          "Movie",
				"has_subtitles": true,
				"has_lyrics":    true,
			},
		},
		{
			name: "false HasSubtitles and HasLyrics omitted",
			input: map[string]any{
				"Id":           "det13b",
				"Name":         "No Subs",
				"Type":         "Movie",
				"HasSubtitles": false,
				"HasLyrics":    false,
			},
			expect: map[string]any{
				"id":   "det13b",
				"name": "No Subs",
				"type": "Movie",
			},
		},
		{
			name: "child_count and recursive_item_count",
			input: map[string]any{
				"Id":                 "det14",
				"Name":               "Container",
				"Type":               "BoxSet",
				"ChildCount":         float64(5),
				"RecursiveItemCount": float64(42),
			},
			expect: map[string]any{
				"id":                   "det14",
				"name":                 "Container",
				"type":                 "BoxSet",
				"child_count":          5,
				"recursive_item_count": 42,
			},
		},
		{
			name: "zero child_count and recursive_item_count omitted",
			input: map[string]any{
				"Id":                 "det14b",
				"Name":               "Empty Container",
				"Type":               "BoxSet",
				"ChildCount":         float64(0),
				"RecursiveItemCount": float64(0),
			},
			expect: map[string]any{
				"id":   "det14b",
				"name": "Empty Container",
				"type": "BoxSet",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The list fields are always present, as [] when the item has none.
			for _, k := range []string{"genres", "tags", "studios", "locked_fields"} {
				if _, ok := tc.expect[k]; !ok {
					tc.expect[k] = []any{}
				}
			}
			assertJSONEqual(t, DetailedItemFrom(tc.input), tc.expect)
		})
	}
}

// ---------------------------------------------------------------------------
// SessionFrom
// ---------------------------------------------------------------------------

func TestSessionFrom(t *testing.T) {
	// Activity times are instants, shown with the server zone's offset.
	inZone(t, time.FixedZone("UTC+9", 9*60*60))
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name: "active session with NowPlayingItem and PlayState",
			input: map[string]any{
				"Id":               "sess-001",
				"UserName":         "alice",
				"Client":           "Jellyfin Web",
				"DeviceName":       "Chrome",
				"Capabilities":     map[string]any{"SupportsMediaControl": true},
				"LastActivityDate": "2024-08-01T12:30:45.1234567Z",
				"NowPlayingItem": map[string]any{
					"Id":           "item-99",
					"Name":         "Distant Orbits",
					"Type":         "Movie",
					"RunTimeTicks": float64(10200000000000), // big number
				},
				"PlayState": map[string]any{
					"IsPaused":      false,
					"PositionTicks": float64(3600 * int64(TicksPerSecond)), // 1 hour
					"VolumeLevel":   float64(75),
				},
			},
			expect: map[string]any{
				"session_id":             "sess-001",
				"user":                   "alice",
				"client":                 "Jellyfin Web",
				"device_name":            "Chrome",
				"supports_media_control": true,
				"last_activity":          "2024-08-01T21:30:45+09:00",
				"status":                 "playing",
				"now_playing": map[string]any{
					"id":              "item-99",
					"name":            "Distant Orbits",
					"type":            "Movie",
					"runtime_minutes": int64(10200000000000) / 600000000,
				},
				"play_state": map[string]any{
					"is_paused":        false,
					"position_seconds": int64(3600*TicksPerSecond) / TicksPerSecond,
					"position_ticks":   int64(3600 * TicksPerSecond),
					"volume":           75,
				},
			},
		},
		{
			name: "idle session without NowPlayingItem",
			input: map[string]any{
				"Id":               "sess-002",
				"UserName":         "bob",
				"Client":           "Android TV",
				"DeviceName":       "Living Room TV",
				"LastActivityDate": "2024-08-01T10:00:00.0000000Z",
			},
			expect: map[string]any{
				"session_id":             "sess-002",
				"user":                   "bob",
				"client":                 "Android TV",
				"device_name":            "Living Room TV",
				"supports_media_control": false,
				"last_activity":          "2024-08-01T19:00:00+09:00",
				"status":                 "connected",
			},
		},
		{
			name: "session with PlayState zero position omits position fields",
			input: map[string]any{
				"Id":               "sess-003",
				"UserName":         "carol",
				"Client":           "iOS",
				"DeviceName":       "iPhone",
				"LastActivityDate": "2024-01-01T00:00:00Z",
				"NowPlayingItem": map[string]any{
					"Id":   "item-50",
					"Name": "Short Film",
					"Type": "Movie",
				},
				"PlayState": map[string]any{
					"IsPaused":      true,
					"PositionTicks": float64(0),
					"VolumeLevel":   float64(0),
				},
			},
			expect: map[string]any{
				"session_id":             "sess-003",
				"user":                   "carol",
				"client":                 "iOS",
				"device_name":            "iPhone",
				"supports_media_control": false,
				"last_activity":          "2024-01-01T09:00:00+09:00",
				"status":                 "playing",
				"now_playing": map[string]any{
					"id":   "item-50",
					"name": "Short Film",
					"type": "Movie",
				},
				"play_state": map[string]any{
					"is_paused": true,
				},
			},
		},
		{
			name: "NowPlayingItem with zero RunTimeTicks omits runtime",
			input: map[string]any{
				"Id":               "sess-004",
				"UserName":         "dave",
				"Client":           "Web",
				"DeviceName":       "Firefox",
				"LastActivityDate": "2024-05-20T08:15:00Z",
				"NowPlayingItem": map[string]any{
					"Id":           "item-live",
					"Name":         "Live Stream",
					"Type":         "TvChannel",
					"RunTimeTicks": float64(0),
				},
			},
			expect: map[string]any{
				"session_id":             "sess-004",
				"user":                   "dave",
				"client":                 "Web",
				"device_name":            "Firefox",
				"supports_media_control": false,
				"last_activity":          "2024-05-20T17:15:00+09:00",
				"status":                 "playing",
				"now_playing": map[string]any{
					"id":   "item-live",
					"name": "Live Stream",
					"type": "TvChannel",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEqual(t, SessionFrom(tc.input), tc.expect)
		})
	}
}

// ---------------------------------------------------------------------------
// LibrariesFrom
// ---------------------------------------------------------------------------

func TestLibrariesFrom(t *testing.T) {
	tests := []struct {
		name   string
		input  []map[string]any
		expect []map[string]any
	}{
		{
			name: "normal library with CollectionType and Locations",
			input: []map[string]any{
				{
					"Name":           "Movies",
					"CollectionType": "movies",
					"ItemId":         "lib-001",
					"Locations":      []any{"/media/movies", "/media/movies2"},
				},
			},
			expect: []map[string]any{
				{
					"name":            "Movies",
					"collection_type": "movies",
					"item_id":         "lib-001",
					"paths":           []string{"/media/movies", "/media/movies2"},
				},
			},
		},
		{
			name: "empty CollectionType defaults to mixed",
			input: []map[string]any{
				{
					"Name":           "Mixed Library",
					"CollectionType": "",
					"ItemId":         "lib-002",
					"Locations":      []any{"/media/mixed"},
				},
			},
			expect: []map[string]any{
				{
					"name":            "Mixed Library",
					"collection_type": "mixed",
					"item_id":         "lib-002",
					"paths":           []string{"/media/mixed"},
				},
			},
		},
		{
			name: "missing CollectionType defaults to mixed",
			input: []map[string]any{
				{
					"Name":   "No Type",
					"ItemId": "lib-003",
				},
			},
			expect: []map[string]any{
				{
					"name":            "No Type",
					"collection_type": "mixed",
					"item_id":         "lib-003",
				},
			},
		},
		{
			name: "empty Locations omitted",
			input: []map[string]any{
				{
					"Name":           "Empty Loc",
					"CollectionType": "tvshows",
					"ItemId":         "lib-004",
					"Locations":      []any{},
				},
			},
			expect: []map[string]any{
				{
					"name":            "Empty Loc",
					"collection_type": "tvshows",
					"item_id":         "lib-004",
				},
			},
		},
		{
			name:   "empty input list",
			input:  []map[string]any{},
			expect: []map[string]any{},
		},
		{
			name: "multiple libraries",
			input: []map[string]any{
				{
					"Name":           "Movies",
					"CollectionType": "movies",
					"ItemId":         "lib-a",
					"Locations":      []any{"/movies"},
				},
				{
					"Name":           "TV Shows",
					"CollectionType": "tvshows",
					"ItemId":         "lib-b",
					"Locations":      []any{"/tv"},
				},
			},
			expect: []map[string]any{
				{
					"name":            "Movies",
					"collection_type": "movies",
					"item_id":         "lib-a",
					"paths":           []string{"/movies"},
				},
				{
					"name":            "TV Shows",
					"collection_type": "tvshows",
					"item_id":         "lib-b",
					"paths":           []string{"/tv"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEqual(t, LibrariesFrom(tc.input), tc.expect)
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractUserSummary
// ---------------------------------------------------------------------------

func TestExtractUserSummary(t *testing.T) {
	// Activity times are instants, shown with the server zone's offset.
	inZone(t, time.FixedZone("UTC+9", 9*60*60))
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name: "admin user",
			input: map[string]any{
				"Id":               "user-001",
				"Name":             "admin",
				"LastActivityDate": "2024-08-01T12:00:00.0000000Z",
				"LastLoginDate":    "2024-08-01T11:00:00.0000000Z",
				"Policy": map[string]any{
					"IsAdministrator": true,
					"IsDisabled":      false,
				},
			},
			expect: map[string]any{
				"id":            "user-001",
				"name":          "admin",
				"is_admin":      true,
				"last_activity": "2024-08-01T21:00:00+09:00",
				"last_login":    "2024-08-01T20:00:00+09:00",
			},
		},
		{
			name: "regular user",
			input: map[string]any{
				"Id":   "user-002",
				"Name": "viewer",
				"Policy": map[string]any{
					"IsAdministrator": false,
					"IsDisabled":      false,
				},
			},
			expect: map[string]any{
				"id":       "user-002",
				"name":     "viewer",
				"is_admin": false,
			},
		},
		{
			name: "disabled user",
			input: map[string]any{
				"Id":   "user-003",
				"Name": "blocked",
				"Policy": map[string]any{
					"IsAdministrator": false,
					"IsDisabled":      true,
				},
			},
			expect: map[string]any{
				"id":          "user-003",
				"name":        "blocked",
				"is_admin":    false,
				"is_disabled": true,
			},
		},
		{
			name: "user with no policy",
			input: map[string]any{
				"Id":   "user-004",
				"Name": "nopolicy",
			},
			expect: map[string]any{
				"id":       "user-004",
				"name":     "nopolicy",
				"is_admin": false,
			},
		},
		{
			name: "user with empty dates omits date fields",
			input: map[string]any{
				"Id":               "user-005",
				"Name":             "nodates",
				"LastActivityDate": "",
				"LastLoginDate":    "",
			},
			expect: map[string]any{
				"id":       "user-005",
				"name":     "nodates",
				"is_admin": false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractUserSummary(tc.input)
			if !reflect.DeepEqual(got, tc.expect) {
				t.Errorf("ExtractUserSummary mismatch\n  got:    %v\n  expect: %v", got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractDetailedUser
// ---------------------------------------------------------------------------

func TestExtractDetailedUser(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name: "full user with policy, configuration, parental controls",
			input: map[string]any{
				"Id":          "du-001",
				"Name":        "fulluser",
				"HasPassword": true,
				"Policy": map[string]any{
					"IsAdministrator":          true,
					"IsDisabled":               false,
					"EnableAllFolders":         true,
					"MaxParentalRating":        float64(13),
					"BlockedTags":              []any{"horror", "gore"},
					"IsHidden":                 true,
					"EnableRemoteAccess":       false,
					"EnableContentDeletion":    true,
					"MaxActiveSessions":        float64(3),
					"RemoteClientBitrateLimit": float64(5000000),
					"InvalidLoginAttemptCount": float64(2),
				},
				"Configuration": map[string]any{
					"SubtitleLanguagePreference": "eng",
					"AudioLanguagePreference":    "eng",
					"SubtitleMode":               "Default",
					"PlayDefaultAudioTrack":      false,
					"EnableNextEpisodeAutoPlay":  false,
					"DisplayMissingEpisodes":     true,
				},
			},
			expect: map[string]any{
				"id":                          "du-001",
				"name":                        "fulluser",
				"is_admin":                    true,
				"enable_all_folders":          true,
				"max_parental_rating":         13,
				"blocked_tags":                []string{"horror", "gore"},
				"is_hidden":                   true,
				"remote_access":               false,
				"can_delete_content":          true,
				"max_active_sessions":         3,
				"remote_bitrate_limit":        int64(5000000),
				"invalid_login_attempt_count": 2,
				"preferences": map[string]any{
					"subtitle_language":        "eng",
					"audio_language":           "eng",
					"subtitle_mode":            "Default",
					"play_default_audio":       false,
					"next_episode_auto_play":   false,
					"display_missing_episodes": true,
				},
			},
		},
		{
			name: "user with no policy",
			input: map[string]any{
				"Id":   "du-002",
				"Name": "nopolicy",
			},
			expect: map[string]any{
				"id":       "du-002",
				"name":     "nopolicy",
				"is_admin": false,
			},
		},
		{
			name: "user with EnableAllFolders false shows enabled_folders",
			input: map[string]any{
				"Id":   "du-003",
				"Name": "limited",
				"Policy": map[string]any{
					"IsAdministrator":    false,
					"EnableAllFolders":   false,
					"EnabledFolders":     []any{"lib-001", "lib-002"},
					"EnableRemoteAccess": true,
				},
			},
			expect: map[string]any{
				"id":                 "du-003",
				"name":               "limited",
				"is_admin":           false,
				"enable_all_folders": false,
				"enabled_folders":    []string{"lib-001", "lib-002"},
			},
		},
		{
			name: "user with EnableAllFolders false and no folders",
			input: map[string]any{
				"Id":   "du-004",
				"Name": "noaccess",
				"Policy": map[string]any{
					"IsAdministrator":    false,
					"EnableAllFolders":   false,
					"EnableRemoteAccess": true,
				},
			},
			expect: map[string]any{
				"id":                 "du-004",
				"name":               "noaccess",
				"is_admin":           false,
				"enable_all_folders": false,
			},
		},
		{
			name: "user with EnableAllFolders true does not show enabled_folders",
			input: map[string]any{
				"Id":   "du-005",
				"Name": "allaccess",
				"Policy": map[string]any{
					"IsAdministrator":    false,
					"EnableAllFolders":   true,
					"EnabledFolders":     []any{"lib-001"},
					"EnableRemoteAccess": true,
				},
			},
			expect: map[string]any{
				"id":                 "du-005",
				"name":               "allaccess",
				"is_admin":           false,
				"enable_all_folders": true,
			},
		},
		{
			// Jellyfin 12 sends HasPassword as a constant true.
			name: "HasPassword is not reported",
			input: map[string]any{
				"Id":          "du-006",
				"Name":        "withpass",
				"HasPassword": true,
			},
			expect: map[string]any{
				"id":       "du-006",
				"name":     "withpass",
				"is_admin": false,
			},
		},
		{
			name: "configuration with default values produces no preferences",
			input: map[string]any{
				"Id":   "du-007",
				"Name": "defaults",
				"Configuration": map[string]any{
					"PlayDefaultAudioTrack":     true,
					"EnableNextEpisodeAutoPlay": true,
					"DisplayMissingEpisodes":    false,
				},
			},
			expect: map[string]any{
				"id":       "du-007",
				"name":     "defaults",
				"is_admin": false,
			},
		},
		{
			name: "configuration with partial preferences",
			input: map[string]any{
				"Id":   "du-008",
				"Name": "partial",
				"Configuration": map[string]any{
					"SubtitleLanguagePreference": "jpn",
					"PlayDefaultAudioTrack":      true,
					"EnableNextEpisodeAutoPlay":  true,
				},
			},
			expect: map[string]any{
				"id":       "du-008",
				"name":     "partial",
				"is_admin": false,
				"preferences": map[string]any{
					"subtitle_language": "jpn",
				},
			},
		},
		{
			name: "policy with IsHidden false does not include is_hidden",
			input: map[string]any{
				"Id":   "du-009",
				"Name": "visible",
				"Policy": map[string]any{
					"IsAdministrator":       false,
					"IsHidden":              false,
					"EnableRemoteAccess":    true,
					"EnableContentDeletion": false,
					"EnableAllFolders":      true,
				},
			},
			expect: map[string]any{
				"id":                 "du-009",
				"name":               "visible",
				"is_admin":           false,
				"enable_all_folders": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractDetailedUser(tc.input)
			if !reflect.DeepEqual(got, tc.expect) {
				t.Errorf("ExtractDetailedUser mismatch\n  got:    %v\n  expect: %v", got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ItemsFrom
// ---------------------------------------------------------------------------

func TestItemsFrom(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]any
		expect []map[string]any
	}{
		{
			name: "normal paginated response with Items array",
			input: map[string]any{
				"TotalRecordCount": float64(2),
				"Items": []any{
					map[string]any{
						"Id":   "item-1",
						"Name": "First",
						"Type": "Movie",
					},
					map[string]any{
						"Id":   "item-2",
						"Name": "Second",
						"Type": "Series",
					},
				},
			},
			expect: []map[string]any{
				{
					"id":   "item-1",
					"name": "First",
					"type": "Movie",
				},
				{
					"id":   "item-2",
					"name": "Second",
					"type": "Series",
				},
			},
		},
		{
			name: "empty Items array",
			input: map[string]any{
				"TotalRecordCount": float64(0),
				"Items":            []any{},
			},
			expect: []map[string]any{},
		},
		{
			name:   "missing Items key",
			input:  map[string]any{},
			expect: []map[string]any{},
		},
		{
			name: "Items with nil entries are skipped",
			input: map[string]any{
				"Items": []any{
					map[string]any{
						"Id":   "valid",
						"Name": "Valid Item",
						"Type": "Movie",
					},
					nil,
					"not-a-map",
				},
			},
			expect: []map[string]any{
				{
					"id":   "valid",
					"name": "Valid Item",
					"type": "Movie",
				},
			},
		},
		{
			name: "Items values are processed through MediaItemFrom",
			input: map[string]any{
				"Items": []any{
					map[string]any{
						"Id":             "full-item",
						"Name":           "Full",
						"Type":           "Movie",
						"ProductionYear": float64(2024),
						"RunTimeTicks":   float64(2 * int64(TicksPerMinute)),
					},
				},
			},
			expect: []map[string]any{
				{
					"id":              "full-item",
					"name":            "Full",
					"type":            "Movie",
					"year":            2024,
					"runtime_minutes": int64(2),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONEqual(t, ItemsFrom(tc.input), tc.expect)
		})
	}
}

// assertJSONEqual compares got and want by the JSON they encode to, so a typed
// result can be checked against a map literal of the fields it must show.
func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshaling got: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshaling want: %v", err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatalf("unmarshaling got: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatalf("unmarshaling want: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("JSON mismatch\n  got:  %s\n  want: %s", gotJSON, wantJSON)
	}
}

// The details carry the day the item was added and, for HDR video, the HDR
// format; a format Jellyfin reports as Unknown is left out.
func TestDetailedItemFrom_DateAddedAndVideoRangeType(t *testing.T) {
	inZone(t, time.UTC)
	source := func(rangeType string) map[string]any {
		return map[string]any{
			"Id": "hdr-1", "Name": "HDR Test", "Type": "Movie",
			"DateCreated": "2026-03-10T12:00:00.1234567Z",
			"MediaSources": []any{map[string]any{
				"Container":    "mkv",
				"MediaStreams": []any{map[string]any{"Type": "Video", "Codec": "hevc", "VideoRange": "HDR", "VideoRangeType": rangeType}},
			}},
		}
	}
	item := DetailedItemFrom(source("DOVIWithHDR10"))
	if item.DateAdded != "2026-03-10" {
		t.Errorf("date_added = %q, want 2026-03-10", item.DateAdded)
	}
	if len(item.MediaSources) != 1 || item.MediaSources[0].VideoRangeType != "DOVIWithHDR10" || item.MediaSources[0].VideoRange != "HDR" {
		t.Errorf("media_sources = %+v", item.MediaSources)
	}
	if got := DetailedItemFrom(source("Unknown")).MediaSources[0].VideoRangeType; got != "" {
		t.Errorf("video_range_type = %q for Unknown, want none", got)
	}
}

// A series keeps its EndDate out of the compact item: without a StartDate it
// is a calendar day, not the end of a broadcast.
func TestMediaItemFrom_SeriesEndDateIsNotAnEndTime(t *testing.T) {
	item := MediaItemFrom(map[string]any{"Id": "s1", "Name": "Show", "Type": "Series", "EndDate": "2020-05-01T00:00:00.0000000Z"})
	if item.EndTime != "" || item.StartTime != "" {
		t.Errorf("series item = %+v, want no start_time or end_time", item)
	}
}
