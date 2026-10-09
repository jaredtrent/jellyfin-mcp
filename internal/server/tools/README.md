# Tools

Thirty-one tools in eight toolsets. `--toolsets discovery,media,...` registers only the toolsets you name, and `--read-only` and `--disable-destructive` restrict what the assistant can change.

## discovery

| Tool | Description |
|------|-------------|
| `jellyfin_libraries` | List all media libraries and their IDs |
| `jellyfin_search` | Search for media by keyword |
| `jellyfin_browse` | Browse and filter by genre, year, studio, person, rating, date added, audio and subtitle language (Jellyfin 12), sort order, and any user's play state |
| `jellyfin_get_item` | Full metadata for a specific item (genres, cast, codecs, ratings, provider IDs, chapters, and on Jellyfin 12 the original language and the collections that include it) |
| `jellyfin_recommendations` | Personalized suggestions, next up, latest additions, similar items |
| `jellyfin_item_extras` | Special features, trailers, theme songs, intro/outro markers |
| `jellyfin_download_link` | Link to download an item: its page in the Jellyfin web app, with the file's path and size |

## media

| Tool | Description |
|------|-------------|
| `jellyfin_tv_shows` | Navigate series structure: seasons, episodes, next up |
| `jellyfin_music` | Browse artists, albums, genres; generate instant mix playlists |
| `jellyfin_people` | Search actors, directors, writers, and studios (music artists are under `jellyfin_music`) |

## user

| Tool | Description |
|------|-------------|
| `jellyfin_user_data` | Favorites, ratings, played/unplayed status |
| `jellyfin_playlists` | Create, modify, reorder, deduplicate, and delete playlists |
| `jellyfin_collections` | Create and manage box set collections |

## playback

| Tool | Description |
|------|-------------|
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_playback_control` | Play, pause, seek, stop, volume, mute, send messages to clients |
| `jellyfin_play` | Start playback of items on a client (play now, play next, add to queue) |

## admin

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, storage, activity log with filters and sorting, log files, playback history |
| `jellyfin_system_control` | Restart or shut down the server |
| `jellyfin_users` | Create, delete, update users; manage permissions and Quick Connect (deleting, changing a policy or password, and `qc_authorize` ask for confirmation) |
| `jellyfin_library_manage` | Library scans (all libraries, or one path), metadata refresh, folder management, filesystem browsing |
| `jellyfin_tasks` | View and manage scheduled tasks and triggers |
| `jellyfin_plugins` | Install, configure, enable/disable plugins and repositories |
| `jellyfin_devices` | Manage connected devices and API keys |
| `jellyfin_server` | Read/write server configuration; list, inspect, create, and restore backups |

## content

| Tool | Description |
|------|-------------|
| `jellyfin_metadata` | Search and apply metadata from online providers, manual edits, batch updates |
| `jellyfin_subtitles_lyrics` | Search, download, and manage subtitles and lyrics |
| `jellyfin_images` | List, download, and upload item images (`image_type` is one of Jellyfin's image types) |
| `jellyfin_videos` | Merge or split alternate video versions |

## livetv

| Tool | Description |
|------|-------------|
| `jellyfin_live_tv` | Channels, program guide, tuner info |
| `jellyfin_recordings` | DVR recordings, timers, and series recording rules |

## analytics

| Tool | Description |
|------|-------------|
| `jellyfin_analytics` | Library stats, codec reports, duplicates, unplayed items |

## Output shapes

Eight tools declare an output type: `jellyfin_libraries`, `jellyfin_search`, `jellyfin_browse`, `jellyfin_get_item`, `jellyfin_recommendations`, `jellyfin_tv_shows`, `jellyfin_sessions`, and `jellyfin_analytics`. For these, the result's `structuredContent` is the whole answer, and its text content is that JSON serialized, so a client that reads either sees the same facts. The structs are in `internal/jellyfin/output_types.go`, and the tool's output schema is derived from them.

- Facts are fields. A page's `total_count`, `shown`, and `next_start_index`; a scan's `total_is_lower_bound` and `undated_count`; analytics' `unread_types`, `days`, and `size_report_type`; a session's `status`; and `movie_recs` `categories` are fields rather than lines of text.
- Guidance is a note. `notes` is a list of strings: a way to see more results, a filter that matched nothing and the values that would match, and what could not be read.
- Empty is not absent. Counts and lists are present when zero or empty, so an empty success never looks like the zero value an error would carry.
- A tool with several actions has one output type. Each action's lists are present, as `[]` when empty, and the other actions' fields are absent.
- An error result carries text and `isError`, and no `structuredContent`.

The other 22 tools return text only, with the same item shape embedded in their JSON where they list items.

