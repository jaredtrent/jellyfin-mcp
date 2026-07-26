# @jaredtrent/jellyfin-mcp

MCP server that connects AI assistants to your [Jellyfin](https://jellyfin.org) media server — 31 tools, 13 live resources, and 18 guided workflows. Search your library, control playback, manage metadata, find subtitles, troubleshoot your server, and more.

This package bundles a pre-compiled native binary. No Node.js runtime is used at execution time — npm is just the delivery mechanism.

For source code and additional install methods (binary download, `go install`), see the [GitHub repo](https://github.com/jaredtrent/jellyfin-mcp).

## Quick start

Add to your MCP client config:

```json
{
  "mcpServers": {
    "jellyfin": {
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

Replace `JELLYFIN_URL` with your Jellyfin server address and `JELLYFIN_API_KEY` with an API key from your Jellyfin dashboard (**Dashboard > Advanced > API Keys**).

<details>
<summary>Claude Desktop</summary>

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS), `%APPDATA%\Claude\claude_desktop_config.json` (Windows), or `~/.config/Claude/claude_desktop_config.json` (Linux):

```json
{
  "mcpServers": {
    "jellyfin": {
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

Restart Claude Desktop after saving.
</details>

<details>
<summary>Claude Code</summary>

```sh
claude mcp add \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  jellyfin -- npx -y @jaredtrent/jellyfin-mcp
```
</details>

<details>
<summary>MetaMCP</summary>

Add as a STDIO server in the MetaMCP dashboard using the JSON config above. Environment variable references (`${VAR_NAME}`) are resolved from the MetaMCP container at runtime.
</details>

<details>
<summary>Other MCP clients</summary>

Any client that supports the `mcpServers` JSON format (Cursor, VS Code Copilot, Windsurf, OpenCode, etc.) can use the config above. Consult your client's documentation for the config file location.
</details>

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JELLYFIN_API_KEY` | Yes | None | API key from your Jellyfin dashboard |
| `JELLYFIN_URL` | Yes | None | Jellyfin server HTTP(S) URL. The `jellyfin_host` placeholder is rejected at startup. |
| `JELLYFIN_USER_ID` | No | auto-detected | User ID for user-scoped operations |
| `JELLYFIN_REQUIRE_USER_ID` | No | `false` | When `true` or `1`, require `JELLYFIN_USER_ID` and disable automatic user selection, including admin fallback. `--read-only` does not enable this strict mode. |

## CLI flags

Append flags after the package name in the `args` array:

```json
"args": ["-y", "@jaredtrent/jellyfin-mcp", "--read-only", "--toolsets", "discovery,media,playback"]
```

| Flag | Description |
|------|-------------|
| `--toolsets` | Comma-separated groups: `discovery`, `media`, `user`, `playback`, `admin`, `content`, `analytics`, `livetv` |
| `--read-only` | Only register read-only tools and hide admin-oriented user resources and completions |
| `--disable-destructive` | Skip destructive tools (delete, restart, shutdown) |

## Tools (31)

<details>
<summary><strong>discovery</strong> — search, browse, recommendations</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_libraries` | List all media libraries and their IDs |
| `jellyfin_search` | Search for media by keyword |
| `jellyfin_browse` | Browse and filter by genre, year, studio, person, rating, sort order |
| `jellyfin_get_item` | Full metadata for a specific item (genres, cast, codecs, ratings, provider IDs) |
| `jellyfin_recommendations` | Personalized suggestions, next up, latest additions, similar items |
| `jellyfin_item_extras` | Special features, trailers, theme songs, intro/outro markers, download URLs |
</details>

<details>
<summary><strong>media</strong> — TV shows, music, people</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_tv_shows` | Navigate series structure: seasons, episodes, next up |
| `jellyfin_music` | Browse artists, albums, genres; generate instant mix playlists |
| `jellyfin_people` | Search actors, directors, writers, and studios |
</details>

<details>
<summary><strong>user</strong> — playlists, collections, favorites</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_user_data` | Favorites, ratings, played/unplayed status |
| `jellyfin_playlists` | Create, modify, reorder, and deduplicate playlists |
| `jellyfin_collections` | Create and manage box set collections |
</details>

<details>
<summary><strong>playback</strong> — sessions, control, SyncPlay</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_playback_control` | Play, pause, seek, stop, volume, mute, send messages to clients |
| `jellyfin_play` | Start playback of items on a client (play now, play next, add to queue) |
| `jellyfin_syncplay` | Synchronized group watching sessions |
</details>

<details>
<summary><strong>admin</strong> — system, users, library, plugins</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, storage, activity logs, log files, playback history |
| `jellyfin_system_control` | Restart or shut down the server |
| `jellyfin_users` | Create, delete, update users; manage permissions and Quick Connect |
| `jellyfin_library_manage` | Library scans, metadata refresh, folder management, filesystem browsing |
| `jellyfin_tasks` | View and manage scheduled tasks and triggers |
| `jellyfin_plugins` | Install, configure, enable/disable plugins and repositories |
| `jellyfin_devices` | Manage connected devices and API keys |
| `jellyfin_server` | Read/write server configuration, manage backups |
</details>

<details>
<summary><strong>content</strong> — metadata, subtitles, images</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_metadata` | Search and apply metadata from online providers, manual edits, batch updates |
| `jellyfin_subtitles_lyrics` | Search, download, and manage subtitles and lyrics |
| `jellyfin_images` | List, download, and upload item images |
| `jellyfin_videos` | Merge or split alternate video versions |
</details>

<details>
<summary><strong>livetv</strong> — guide, channels, DVR</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_live_tv` | Channels, program guide, tuner info |
| `jellyfin_recordings` | DVR recordings, timers, and series recording rules |
</details>

<details>
<summary><strong>analytics</strong> — stats, reports</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_analytics` | Library stats, codec reports, duplicates, unplayed items |
</details>

## Prompts (18)

Pre-built multi-step workflows: `find-and-play`, `resume-watching`, `whats-new`, `movie-night`, `music-listen`, `binge-watch`, `fix-subtitles`, `who-is-watching`, `troubleshoot`, `bulk-metadata-fix`, `subtitle-audit`, `library-report`, `duplicate-finder`, `watch-history`, `codec-optimize`, `parental-controls`, `server-setup`, `library-health`.

## Safety

- Destructive operations require explicit `confirm=true` — the AI is instructed to ask first
- `--read-only` and `--disable-destructive` flags for restricted environments
- `--toolsets` to expose only the tool groups you need

## Platform

This package contains a **linux/x64** binary. For macOS, Windows, or ARM, see the [GitHub repo](https://github.com/jaredtrent/jellyfin-mcp#2-install-jellyfin-mcp) for binary downloads and `go install`.

## License

[MIT](https://github.com/jaredtrent/jellyfin-mcp/blob/main/LICENSE)
