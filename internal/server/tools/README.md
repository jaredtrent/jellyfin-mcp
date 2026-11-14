# Tools

Twenty-five tools in eight `<verb>-<noun>` toolsets. `--toolsets find-media,monitor-media,...` registers only the toolsets you name (`find-media`, `monitor-media`, and `monitor-server` are strictly read-only; legacy names `discovery`, `media`, `user`, `playback`, `content`, `analytics`, `admin`, `manage-server`, and `livetv` remain accepted as aliases). `--read-only` exposes read-only tools plus the read-only actions of mixed tools, `--disable-destructive` refuses destructive actions at runtime, and in `--http` mode `/mcp?toolsets=...&read_only=true` narrows a client connection's toolsets without widening the server's CLI configuration.

## find-media (read-only)

| Tool | Description |
|------|-------------|
| `jellyfin_libraries` | List all media libraries and their IDs |
| `jellyfin_search` | Search media by keyword, or list/search actors, directors, writers (`Person`), studios (`Studio`), music artists (`MusicArtist`), and genres (`Genre`) |
| `jellyfin_browse` | Browse and filter by library name or ID, genre, year, studio, series, person, rating, date added, audio and subtitle language, video resolution tier (`sd`–`8k`) or pixel bounds, episode-by-series grouping, sort order, and playback state (with optional cross-user `user_id`) |
| `jellyfin_get_item` | Full metadata for an item: genres, cast, codecs, ratings, provider IDs, chapters, original language, and collections |
| `jellyfin_recommendations` | Personalized suggestions, next up, upcoming episodes, latest additions (by library name or ID), similar items, movie recommendation groups, and recently marked-played items, with optional cross-user `user_id` |
| `jellyfin_item_extras` | Playback/transcode diagnostics, special features, trailers, theme songs/videos, intro/outro markers, and Jellyfin web-app download links |
| `jellyfin_tv_shows` | Navigate series structure: seasons, episodes, and next up, with optional cross-user `user_id` |
| `jellyfin_music` | Browse artists, album artists, music genres; generate instant mix playlists |
| `jellyfin_analytics` | Library stats, codec and resolution reports, size reports, duplicates, recently added, and unplayed items (by library name or ID, with optional `user_id` on `never_played`) |
| `jellyfin_user_data` *(RO)* | Read-only projection (`get_user_data`, with optional `user_id`) |
| `jellyfin_playlists` *(RO)* | Read-only projection (`list`, `get`, with optional `user_id`) |
| `jellyfin_metadata` *(RO)* | Read-only projection (`search`, `editor_info`, `external_ids`) |
| `jellyfin_subtitles_lyrics` *(RO)* | Read-only projection (`search_subtitles`, `get_lyrics`, `search_lyrics`) |
| `jellyfin_images` *(RO)* | Read-only projection (`list`, `get_url`, `remote_list`) |

## monitor-media (read-only)

| Tool | Description |
|------|-------------|
| `jellyfin_playback_stats` | Verified user watch history (`playback_history`), per-user watch status for an item (`played_status`), household most-played items (`most_played`), and items unwatched by every enabled user (`unwatched_by_all`) |
| `jellyfin_sessions` | List active client sessions (including client version and remote IP) and resumable items, with optional `user_id` |

## monitor-server (read-only)

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, health check, storage, activity log with filters and sorting (including `short_overview` client IPs), and log files |
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_server` *(RO)* | Read-only projection (`get_config`, `get_config_section`, `list_backups`, `backup_manifest`, `list_tasks`, `get_task`) |
| `jellyfin_users` *(RO)* | Read-only projection (`list`, `get`, `qc_status`) |
| `jellyfin_devices` *(RO)* | Read-only projection (`list`, `get`, `api_keys`) |
| `jellyfin_plugins` *(RO)* | Read-only projection (`list`, `get_config`, `list_packages`, `list_repos`) |
| `jellyfin_library_manage` *(RO)* | Read-only projection (`list_folders`, `browse_drives`, `browse_directory`) |

## my-media

| Tool | Description |
|------|-------------|
| `jellyfin_user_data` | Favorites, ratings, played/unplayed status |
| `jellyfin_playlists` | Create, modify, reorder, deduplicate, and delete playlists |

## control-playback

| Tool | Description |
|------|-------------|
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_playback` | Start playback (`PlayNow`, `PlayNext`, `PlayLast`) and control transport, seek, volume, mute, tracks, UI navigation, or client messages |

## manage-media

| Tool | Description |
|------|-------------|
| `jellyfin_collections` | Create and manage box set collections |
| `jellyfin_metadata` | Search and apply metadata from online providers, manual edits, batch updates, and merge or split alternate video versions |
| `jellyfin_subtitles_lyrics` | Search, download, upload, and delete subtitles and lyrics |
| `jellyfin_images` | List, fetch URLs for, remote-download, and upload item images (`image_type` is one of Jellyfin's image types) |
| `jellyfin_library_manage` *(Curate)* | Curator projection (`list_folders`, `scan`, `refresh_item`) |

## admin-server

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, health check, storage, activity log with filters and sorting, and log files |
| `jellyfin_users` | List, inspect, create, delete, and update users (by username or ID); manage library/admin permissions and Quick Connect |
| `jellyfin_library_manage` | Library scans (every library, one library by name or ID, or one changed path), metadata refresh, item deletion, folder and option management, and filesystem browsing |
| `jellyfin_plugins` | Install, configure, enable/disable, and uninstall plugins and repositories |
| `jellyfin_devices` | Manage connected devices and API keys |
| `jellyfin_server` | Read and merge server configuration sections; list, inspect, create, and restore backups; manage scheduled tasks and triggers; restart or shut down the server |

## live-tv

| Tool | Description |
|------|-------------|
| `jellyfin_live_tv` | Channels, program guide, tuner info, DVR recordings, one-time timers, and series recording rules |

## Output shapes

Nine tools declare an output type: `jellyfin_libraries`, `jellyfin_search`, `jellyfin_browse`, `jellyfin_get_item`, `jellyfin_recommendations`, `jellyfin_tv_shows`, `jellyfin_sessions`, `jellyfin_analytics`, and `jellyfin_playback_stats`. For these, the result's `structuredContent` is the whole answer, and its text content is that JSON serialized, so a client that reads either sees the same facts. The structs are in `internal/jellyfin/output_types.go`, and the tool's output schema is derived from them.

- Facts are fields. A page's `total_count`, `shown`, and `next_start_index`; a scan's `total_is_lower_bound` and `undated_count`; analytics' `unread_types`, `days`, and `size_report_type`; a session's `status`; and `movie_recs` `categories` are fields rather than lines of text.
- Guidance is a note. `notes` is a list of strings: a way to see more results, a filter that matched nothing and the values that would match, and what could not be read.
- Empty is not absent. Counts and lists are present when zero or empty, so an empty success never looks like the zero value an error would carry.
- A tool with several actions has one output type. Each action's lists are present, as `[]` when empty, and the other actions' fields are absent.
- An error result carries text and `isError`, and no `structuredContent`.

The other 16 tools return text only, with compact JSON and the same item shape embedded where they list items.
