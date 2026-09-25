# @jaredtrent/jellyfin-mcp

An MCP server that connects an AI assistant to your [Jellyfin](https://jellyfin.org) media server, with 31 tools, 13 live resources, and 19 guided workflows. The assistant searches your library, controls playback, fixes metadata, finds subtitles, and troubleshoots the server.

This package bundles a compiled native binary. Nothing runs on Node.js; npm is the delivery mechanism.

The [GitHub repository](https://github.com/jaredtrent/jellyfin-mcp) holds the source and the other install methods, such as the binary download and `go install`.

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
<summary>Claude Desktop</summary>

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS), `%APPDATA%\Claude\claude_desktop_config.json` (Windows), or `~/.config/Claude/claude_desktop_config.json` (Linux):

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

Restart Claude Desktop after saving.
</details>

<details>
<summary>Claude Code</summary>

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

Any client that reads the `mcpServers` JSON format, such as Cursor, VS Code Copilot, Windsurf, and OpenCode, takes the configuration above. The client's documentation names its configuration file.
</details>

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JELLYFIN_API_KEY` | Yes | none | The API key from the Jellyfin dashboard |
| `JELLYFIN_URL` | No | a placeholder | The server URL, such as `http://YOUR_SERVER:8096`, or `https://YOUR_SERVER:8920` if HTTPS is turned on |
| `JELLYFIN_USER_ID` | No | the first administrator | The user that user-scoped calls act as: a user ID or a username. Without it, an API key acts as the first administrator. |
| `TZ` | No | system time zone | Time zone, such as `America/New_York`, in which the server states today's date, reads dates such as `2024-01-31` in tool inputs, and gives the dates and times in tool results. Set it in the client's `env` block, because MCP clients usually do not pass your shell's environment to the server. A name the system doesn't know falls back to UTC with a warning at startup. Windows ignores `TZ` and always uses the system time zone. |

## Flags

Append flags after the package name in the `args` array:

```json
"args": ["-y", "@jaredtrent/jellyfin-mcp", "--read-only", "--toolsets", "discovery,media,playback"]
```

| Flag | Description |
|------|-------------|
| `--toolsets` | Comma-separated groups: `discovery`, `media`, `user`, `playback`, `admin`, `content`, `analytics`, `livetv` |
| `--read-only` | Register only the tools that read |
| `--disable-destructive` | Refuse destructive actions (deletes, removals, restores, uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, and password, policy, trigger, and whole-configuration changes) and allow every other write |

## Tools (30)

<details>
<summary><strong>discovery</strong>: search, browse, recommendations</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_libraries` | List all media libraries and their IDs |
| `jellyfin_search` | Search for media by keyword |
| `jellyfin_browse` | Browse and filter by genre, year, studio, person, rating, date added, audio and subtitle language (Jellyfin 12), sort order, and any user's play state |
| `jellyfin_get_item` | Full metadata for a specific item (genres, cast, codecs, ratings, provider IDs, chapters, and on Jellyfin 12 the original language and the collections that include it) |
| `jellyfin_recommendations` | Personalized suggestions, next up, latest additions, similar items |
| `jellyfin_item_extras` | Special features, trailers, theme songs, intro/outro markers |
| `jellyfin_download_link` | Link to download an item: its page in the Jellyfin web app, with the file's path and size |
</details>

<details>
<summary><strong>media</strong>: TV shows, music, people</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_tv_shows` | Navigate series structure: seasons, episodes, next up |
| `jellyfin_music` | Browse artists, albums, genres; generate instant mix playlists |
| `jellyfin_people` | Search actors, directors, writers, and studios (music artists are under `jellyfin_music`) |
</details>

<details>
<summary><strong>user</strong>: playlists, collections, favorites</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_user_data` | Favorites, ratings, played/unplayed status |
| `jellyfin_playlists` | Create, modify, reorder, deduplicate, and delete playlists |
| `jellyfin_collections` | Create and manage box set collections |
</details>

<details>
<summary><strong>playback</strong>: sessions, control, play</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_sessions` | List active client sessions and resumable items |
| `jellyfin_playback_control` | Play, pause, seek, stop, volume, mute, send messages to clients |
| `jellyfin_play` | Start playback of items on a client (play now, play next, add to queue) |
</details>

<details>
<summary><strong>admin</strong>: system, users, library, plugins</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_system_info` | Server info, storage, activity log with filters and sorting, log files, playback history |
| `jellyfin_system_control` | Restart or shut down the server |
| `jellyfin_users` | Create, delete, update users; manage permissions and Quick Connect (deleting, changing a policy or password, and `qc_authorize` ask for confirmation) |
| `jellyfin_library_manage` | Library scans, metadata refresh, folder management, filesystem browsing |
| `jellyfin_tasks` | View and manage scheduled tasks and triggers |
| `jellyfin_plugins` | Install, configure, enable/disable plugins and repositories |
| `jellyfin_devices` | Manage connected devices and API keys |
| `jellyfin_server` | Read/write server configuration; list, inspect, create, and restore backups |
</details>

<details>
<summary><strong>content</strong>: metadata, subtitles, images</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_metadata` | Search and apply metadata from online providers, manual edits, batch updates |
| `jellyfin_subtitles_lyrics` | Search, download, and manage subtitles and lyrics |
| `jellyfin_images` | List, download, and upload item images (`image_type` is one of Jellyfin's image types) |
| `jellyfin_videos` | Merge or split alternate video versions |
</details>

<details>
<summary><strong>livetv</strong>: guide, channels, DVR</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_live_tv` | Channels, program guide, tuner info |
| `jellyfin_recordings` | DVR recordings, timers, and series recording rules |
</details>

<details>
<summary><strong>analytics</strong>: statistics, reports</summary>

| Tool | Description |
|------|-------------|
| `jellyfin_analytics` | Library stats, codec reports, duplicates, unplayed items |
</details>

## Prompts (19)

Multi-step workflows: `find-and-play`, `resume-watching`, `whats-new`, `movie-night`, `music-listen`, `binge-watch`, `fix-subtitles`, `who-is-watching`, `troubleshoot`, `bulk-metadata-fix`, `subtitle-audit`, `library-report`, `duplicate-finder`, `watch-history`, `codec-optimize`, `parental-controls`, `server-setup`, `library-health`, `syncplay-help`.

## Safety

- A destructive action confirms with you before it runs, through a confirmation form in a client that supports MCP elicitation, and otherwise through a warning the assistant presents before it repeats the call with `confirm=true`. On a client older than MCP 2026-07-28, a form left unanswered for 10 minutes expires, and nothing is done.
- `--read-only` and `--disable-destructive` restrict what the assistant can change.
- `--toolsets` exposes only the tool groups you name.

## Platform

This package holds a linux/x64 binary. For macOS, Windows, or ARM, the [GitHub repository](https://github.com/jaredtrent/jellyfin-mcp#2-install-jellyfin-mcp) has binary downloads and `go install`.

## License

[MIT](https://github.com/jaredtrent/jellyfin-mcp/blob/main/LICENSE)
