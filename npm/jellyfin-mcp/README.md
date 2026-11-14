# @jaredtrent/jellyfin-mcp

jellyfin-mcp connects an AI assistant to your [Jellyfin](https://jellyfin.org) server, so you can ask about your library and manage it in plain language. The assistant answers from what you actually own and acts on your server, whether you're deciding what to watch, keeping the library in order, or looking after the server itself.

It's an MCP server with 25 tools, 13 live resources, and 19 guided workflows.

This package bundles a compiled linux/x64 binary for MetaMCP and other Docker-based MCP gateways, and nothing runs on Node.js. On macOS, Windows, or ARM, npm doesn't install it; use the one-command install or the binary download from the [GitHub repository](https://github.com/jaredtrent/jellyfin-mcp#2-install-jellyfin-mcp) instead.

## Quick Start

Add the server to your MCP client configuration:

```json
{
  "mcpServers": {
    "jellyfin-mcp": {
      "command": "npx",
      "args": ["-y", "@jaredtrent/jellyfin-mcp"],
      "env": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

Replace `JELLYFIN_URL` with your Jellyfin server address and `JELLYFIN_API_KEY` with an API key from your Jellyfin dashboard (Dashboard > Advanced > API Keys).

<details>
<summary>Claude Code on Linux x64</summary>

```sh
claude mcp add -s user jellyfin-mcp \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  -- npx -y @jaredtrent/jellyfin-mcp
```
</details>

<details>
<summary>MetaMCP</summary>

Add as a STDIO server in the MetaMCP dashboard using the JSON config above. Environment variable references (`${VAR_NAME}`) are resolved from the MetaMCP container at runtime.
</details>

<details>
<summary>Other MCP clients</summary>

On Linux x64, any client that reads the `mcpServers` JSON format, such as Cursor, VS Code Copilot, Windsurf, and OpenCode, takes the configuration above. The client's documentation names its configuration file.
</details>

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JELLYFIN_API_KEY` | Yes | none | The API key from the Jellyfin dashboard |
| `JELLYFIN_URL` | No | a placeholder | The server URL, such as `http://YOUR_SERVER:8096`, or `https://YOUR_SERVER:8920` if HTTPS is turned on |
| `JELLYFIN_USER_ID` | No | the first administrator | The user that user-scoped calls act as: a user ID or a username. Without it, a user token acts as its own user, and an API key acts as the first administrator. |
| `HTTP_TOKEN` | No | none | Bearer token for `--http`, required when listening beyond localhost (replaces `--http-token`). |
| `TZ` | No | system time zone | Time zone, such as `America/New_York`, in which the server states today's date, reads dates such as `2024-01-31` in tool inputs, and gives the dates and times in tool results. Set it in the client's `env` block, because MCP clients usually do not pass your shell's environment to the server. A name the system doesn't know falls back to UTC with a warning at startup. Windows ignores `TZ` and always uses the system time zone. |

## Flags

Append flags after the package name in the `args` array:

```json
"args": ["-y", "@jaredtrent/jellyfin-mcp", "--toolsets", "find-media,monitor-media"]
```

| Flag | Description |
|------|-------------|
| `--toolsets` | Comma-separated groups: `find-media`, `monitor-media`, `monitor-server` (read-only), and `my-media`, `control-playback`, `manage-media`, `admin-server`, `live-tv` (read and write). Legacy group names (`discovery`, `media`, `user`, `playback`, `content`, `analytics`, `admin`, `manage-server`, `livetv`) are also accepted. In `--http` mode, `/mcp?toolsets=...&read_only=true` narrows the server's configured toolsets per connection. |
| `--read-only` | Expose only read-only tools and read-only actions of mixed tools (pure write tools and write actions are omitted) |
| `--disable-destructive` | Refuse destructive actions (deletes, removals, metadata or image replacements, restores, plugin disables and uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, Quick Connect authorizations, and password, policy, trigger, and configuration changes) and allow every other write |
| `--http` | Serve Streamable HTTP at `/mcp` and `/health` instead of stdio |
| `--addr` | HTTP listen address (default `127.0.0.1:8080`) |
| `--http-token` | Bearer token for HTTP authentication, required when listening beyond localhost (or set `HTTP_TOKEN`) |

## Tools (25)

<details>
<summary><strong>find-media</strong> (read-only): search, browse, recommendations, catalog reports</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_libraries` | List all media libraries and their IDs |
| `jellyfin_search` | Search media by keyword, or list/search people, studios, music artists, and genres |
| `jellyfin_browse` | Browse and filter by library name or ID, genre, year, studio, series, person, rating, date added, audio and subtitle language, video resolution tier (`sd`–`8k`) or pixel bounds, episode-by-series grouping, sort order, and playback state (with optional `user_id`) |
| `jellyfin_get_item` | Full metadata for an item: genres, cast, codecs, ratings, provider IDs, chapters, original language, and collections |
| `jellyfin_recommendations` | Personalized suggestions, next up, upcoming episodes, latest additions, similar items, movie recommendation groups, and recently marked-played items (for the current user or another `user_id`) |
| `jellyfin_item_extras` | Playback diagnostics, special features, trailers, theme songs/videos, intro/outro markers, and Jellyfin web-app download links |
| `jellyfin_tv_shows` | Navigate series structure: seasons, episodes, and next up (with optional `user_id` watch progress) |
| `jellyfin_music` | Browse artists, album artists, music genres; generate instant mix playlists |
| `jellyfin_analytics` | Library stats, library and series size reports, codec and resolution reports, duplicates, recently added, and unplayed items |
| `jellyfin_user_data`, `jellyfin_playlists`, `jellyfin_metadata`, `jellyfin_subtitles_lyrics`, `jellyfin_images` | Read-only projections (`get_user_data`, playlist `list`/`get`, metadata `search`/`editor_info`/`external_ids`, subtitle/lyric `search_subtitles`/`get_lyrics`/`search_lyrics`, image `list`/`get_url`/`remote_list`) |
</details>

<details>
<summary><strong>monitor-media</strong> (read-only): playback history, household stats, sessions</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_playback_stats` | Verified user watch history (`playback_history`), per-user watch status for an item (`played_status`), household most-played items (`most_played`), and items unwatched by every enabled user (`unwatched_by_all`) |
| `jellyfin_sessions` | List active client sessions (including app version and remote IP) and resumable items |
</details>

<details>
<summary><strong>monitor-server</strong> (read-only): server health, logs, activity, read-only admin views</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, health check, storage, activity log with filters and sorting, and log files |
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_server`, `jellyfin_users`, `jellyfin_devices`, `jellyfin_plugins`, `jellyfin_library_manage` | Read-only projections of server configuration, backups, scheduled tasks, users, devices, API keys, plugins, library folders, and server filesystem browsing |
</details>

<details>
<summary><strong>my-media</strong>: personal favorites, play state, playlists</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_user_data` | Favorites, ratings, played/unplayed status |
| `jellyfin_playlists` | Create, modify, reorder, deduplicate, and delete playlists |
</details>

<details>
<summary><strong>control-playback</strong>: sessions, remote play, transport control</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_playback` | Start playback (`PlayNow`, `PlayNext`, `PlayLast`) and control transport, seek, volume, mute, tracks, UI navigation, or client messages |
</details>

<details>
<summary><strong>manage-media</strong>: collections, metadata, versions, subtitles, images, library scans</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_collections` | Create and manage box set collections |
| `jellyfin_metadata` | Search and apply metadata from online providers, manual edits, batch updates, and merge or split alternate video versions |
| `jellyfin_subtitles_lyrics` | Search, download, upload, and delete subtitles and lyrics |
| `jellyfin_images` | List, fetch URLs for, remote-download, and upload item images (`image_type` is one of Jellyfin's image types) |
| `jellyfin_library_manage` | Curator projection: list library folders (`list_folders`), run library or path scans (`scan`), and refresh item metadata (`refresh_item`) |
</details>

<details>
<summary><strong>admin-server</strong>: system, users, library folders, plugins, devices, server</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, health check, storage, activity log with filters and sorting, and log files |
| `jellyfin_users` | Create, delete, update users (by username or ID); manage permissions and Quick Connect |
| `jellyfin_library_manage` | Library scans (every library, one library by name or ID, or one changed path), metadata refresh, item deletion, folder and option management, and filesystem browsing |
| `jellyfin_plugins` | Install, configure, enable/disable, and uninstall plugins and repositories |
| `jellyfin_devices` | Manage connected devices and API keys |
| `jellyfin_server` | Read and merge server configuration sections; list, inspect, create, and restore backups; manage scheduled tasks and triggers; restart or shut down the server |
</details>

<details>
<summary><strong>live-tv</strong>: guide, channels, DVR</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_live_tv` | Channels, program guide, tuner info, DVR recordings, timers, and series recording rules |
</details>

## Prompts (19)

Multi-step workflows: `find-and-play`, `resume-watching`, `whats-new`, `movie-night`, `music-listen`, `binge-watch`, `fix-subtitles`, `who-is-watching`, `troubleshoot`, `bulk-metadata-fix`, `subtitle-audit`, `library-report`, `duplicate-finder`, `watch-history`, `codec-optimize`, `parental-controls`, `server-setup`, `library-health`, `syncplay-help`.

## Safety

- Destructive actions and high-impact writes confirm with you before running, through a native confirmation form in a client that supports MCP form elicitation (which `confirm=true` cannot bypass), and otherwise through a warning the assistant presents before repeating the call with `confirm=true`. On a client older than MCP 2026-07-28, a form left unanswered for 10 minutes expires, and nothing is done.
- `--read-only` and `--disable-destructive` restrict what the assistant can change.
- `--toolsets` exposes only the tool groups you name.

## License

[MIT](https://github.com/jaredtrent/jellyfin-mcp/blob/main/LICENSE)
