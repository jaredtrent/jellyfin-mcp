# Changelog

## v2026.924.1

Jellyfin 12 support and MCP protocol 2026-07-28. Thanks to @DrewFerg11 (#24) and @cmc0619 (#25), whose fixes this release builds on, and to @Barrow1990 for reporting #26.

### Breaking changes

- **Jellyfin 10.11 or later is required.** All calls use the modern routes, and the server logs a warning at startup, and `health_check` reports the version as unsupported, when Jellyfin is older. `health_check` gives `supported` and `minimum_supported_version` in its server section.
- **The API key travels in the `Authorization` header** (`MediaBrowser Token="…"`), which Jellyfin 12 requires and every supported version accepts. No server setting needs to change. Fixes #26 (every call returned 401 on Jellyfin 12).
- **`jellyfin_syncplay` is removed.** Jellyfin rejects SyncPlay for API keys, and joining a group would stall real viewers. The new `jellyfin://guides/syncplay` guide and `syncplay-help` prompt help set up and troubleshoot watch-together groups instead.
- **Dates and times in tool results are in the server's time zone** (`TZ`). Date-times carry their offset, such as `2024-01-31T00:05:00-05:00`, instead of a zoneless UTC time, and dates such as `date_added` are the local calendar day. `last_played` is a calendar day on items and a date-time in `playback_history` and in `jellyfin_browse` sorted by `DatePlayed`. `premiere_date` and `end_date` are unchanged.
- **Output fields removed because Jellyfin no longer fills them:** `os`, `has_update_available`, and `can_self_restart` (server info and `health_check`), `has_password` (users), and `file_name` (item details).
- **`health_check` reports its status as `ok` instead of `healthy`**, and its storage and backup sections carry an `ok` flag in place of `healthy`. Its `recent_log_issues` replaces `last_issues` with `last_errors` and `last_warnings`, the latest five of each, and an error's line carries its exception message. The one mixed list held the last five issues of any kind, so frequent warnings pushed the errors out.
- **`log_file` returns entries, not lines**, and `limit` counts entries (default 100, at most 500). See Changed.
- **`jellyfin_item_extras` no longer has a `download_url` action. The new `jellyfin_download_link` tool gives an item's download link instead.** It returns the item's page in the Jellyfin web app, with the file path and size, and the signed-in user downloads from there with their own permissions. The old action returned a link carrying the API key, which reached any client, including under `--read-only`. An assistant looking for a download link now finds the tool by its name.
- **`jellyfin_get_item` takes `item_id`** instead of `id`, and **`jellyfin_recommendations` takes `action`** instead of `type`, like every other tool. The values are unchanged.
- **Go 1.26 or later is required to build**, and `go.mod` names the Go release that builds every artifact (`toolchain go1.27.1`), which the `go` command downloads when needed.
- **`--disable-destructive` refuses destructive actions instead of hiding tools.** Every tool stays registered, so the other actions of `jellyfin_users`, `jellyfin_library_manage`, `jellyfin_devices`, and `jellyfin_recordings` become available. Deletes, removals, restores, uninstalls, restarts, shutdowns, revocations, cancellations, and password, policy, trigger, and whole-configuration changes are refused with a message that names the flag. Before, those tools disappeared whole, while `restore_backup`, plugin uninstalls, playlist and collection removals, and subtitle and lyrics deletes stayed available.

### Added

- **MCP 2026-07-28:** the HTTP endpoint serves clients of every supported protocol version. Current clients are served statelessly, and a call whose request is abandoned is cancelled; older clients keep 30-minute sessions. Prompts have titles, the server has a description and website, and lists and resources carry cache hints.
- **`jellyfin_get_item`** and `jellyfin://items/{itemId}` show `chapters`, with `start_ticks` for seeking, and on Jellyfin 12 `original_language` and `included_in_collections`, the collections that include the item.
- **`jellyfin_browse`** filters by `audio_languages` and `subtitle_languages` on Jellyfin 12. A language code that does not occur in the library browsed gets a list of the codes that do. On Jellyfin 10.11 they are refused rather than ignored, and `subtitle_languages` cannot be combined with `has_subtitles=false`.
- **`activity_log`** filters by `item_id`, `max_date`, and `severity`, and sorts with `sort_by` and `sort_order`. Jellyfin 12 applies them itself unless `user_id` is set or `severity` names more than one level; otherwise jellyfin-mcp applies them to the 2,000 most recent entries, and the result says when that limit was reached. The `troubleshoot` prompt checks recent errors and warnings this way.
- **`jellyfin_server backup_manifest`** shows what a backup contains before it is restored.
- **`TZ`** sets the time zone the server states today's date in and reads dates such as `2024-01-31` in. The Docker image includes the time zone database, and the server logs its zone at startup. `docker-compose.yml` passes `TZ` from `.env`, defaulting to UTC.
- **`JELLYFIN_USER_ID`** accepts a username, matched without regard to case, as well as an ID in any form. A value that matches no user is reported as an error instead of being sent to Jellyfin (#25).
- **`jellyfin_playlists` `delete`** deletes a playlist, with confirmation.
- **`jellyfin_get_item`** shows `date_added` and, for HDR video, `video_range_type` such as `HDR10`, `DOVI`, or `HLG`. **`jellyfin_browse`** shows `last_played` on each item when `sort_by` is `DatePlayed`.
- **`activity_log` `severity`** accepts several levels separated by commas, such as `Warning,Error,Critical`.
- **Completions** for a prompt's `library` argument offer the server's library names, and `watch-history`'s `user` offers its usernames. The `duplicate-finder` `type` completes with Movie and Series, the values the prompt describes.
- **`jellyfin_playlists` `remove_items` and `move_item`** accept `dry_run=true` to preview the requests an edit would send. An edit is refused when it would re-add a folder entry, would re-add a repeated item on Jellyfin 10.11, or targets a playlist of more than 10,000 entries. `add_items` reports how many entries the playlist gained.
- **`jellyfin_get_item`** and `jellyfin://items/{itemId}` show `tags` and `locked_fields`, the lists a metadata update replaces whole. `genres`, `tags`, `studios`, and `locked_fields` are empty lists when the item has none, rather than absent.
- **`jellyfin_browse` takes `user_id`** to show another user's view: their played state, favorites, and `last_played`, within the libraries they can see. A note names the user, and an unknown ID is refused with the way to find IDs. The server instructions send questions about another person's viewing to it and to `playback_history`.
- **Live TV program listings** show each program's `channel_name`, `start_time`, and `end_time`, the times in the server's time zone.
- **`jellyfin_sessions`** shows `supports_media_control` for each session, which says whether its client accepts remote playback commands.

### Changed

- **A list cut at its limit says so**, with how many it shows of how many and how to see more: `next_up` in `jellyfin_recommendations` and `jellyfin_tv_shows`, `upcoming`, `resume`, the Live TV channel, program, and recommended-program lists, music genres, and the size report's ranking. `jellyfin_playlists` `list` reads up to 2,000 playlists and says when there are more, where it stopped at 200 without saying so. `jellyfin_get_item` says when a cast of more than 15 is cut, and `playback_history` says when it checked only the most recently played items.
- **`log_file` reads the log as entries**: each entry keeps its exception message and the first three frames of each stack trace, where a severity filter used to keep only the tagged line and drop the reason for the error. `error` includes fatal errors. `contains` searches an entry's whole text, and `min_date` and `max_date` select a stretch of time. A result holds the newest matching entries that fit in about 48,000 characters, and when it leaves earlier entries out, it gives the `max_date` that reaches them. Before, a request for more than 500 lines returned 500 without saying so, and 500 lines could be more text than an MCP client accepts.
- **Release archives have names without a version**, such as `jellyfin-mcp_macOS_apple-silicon.tar.gz`, so `https://github.com/jaredtrent/jellyfin-mcp/releases/latest/download/<archive>` always reaches the newest release. The README installs on macOS and Linux with one Terminal command that downloads from that link, which also keeps macOS from blocking the program, and tells browser downloaders how to clear the block. `make dist` builds every archive and `checksums.txt`.
- **Messages are limited to 32 MiB** on stdio and HTTP, enough for the largest upload. A larger message ends a stdio session and gets 413 over HTTP. Image and subtitle uploads are limited to 20 MiB decoded.
- **Confirmations** reach clients on MCP 2026-07-28 as input requests, and a form is shown only to clients that declare form elicitation. The form has no fields: accepting it confirms and declining it cancels, where before an accepted form with its box unticked cancelled. Its message is a question that names what the action touches, such as the playlist, item, user, task, plugin, or device, rather than an ID, followed by the consequence. The result of a confirmed call begins with "Confirmed by the user.", and a call the user doesn't confirm returns "The user didn't confirm, so nothing changed." A form that is declined, fails, or goes unanswered for 10 minutes on an older client cancels the call, where it used to fall back to the text warning. The server instructions and the warning tell the AI to call without `confirm` first and to repeat the call with `confirm=true` only after the user agrees.
- **The server instructions state today's date as of each connection**, in the server's time zone with its offset, rather than the date the server started.
- **`activity_log` `type`** matches a case-insensitive part of the type, such as `PlaybackStopped` for both video and audio. `min_date` accepts only a date such as `2024-01-01` or an RFC 3339 timestamp with an offset. `severity`, `sort_by`, and `sort_order` must be one of their listed values, and `item_id` a Jellyfin item ID.
- **Playback history and `jellyfin://recently-played`** count audio playback as well as video, match user IDs in any form, and report an unreadable activity log instead of showing no playback.
- **Resource subscriptions** end with the client's session, and the poller reads only the resources someone subscribes to.
- **`jellyfin_metadata` `apply` and `update` ask for confirmation**, as `batch_update` already did, and `update` honours `dry_run=true` with a preview instead of writing.
- **Image and subtitle uploads are streamed to Jellyfin** without decoding the file into memory or copying its base64 text, which lowers the server's peak memory during the largest uploads.
- **`docker-compose.yml` sets `mem_limit: 512m`**, which two concurrent 20 MiB uploads need; a 256 MiB container can be OOM-killed under that load.
- **`jellyfin_users` `update_policy`, `update_password`, and `qc_authorize` ask for confirmation.** So do `update_config` with a raw `config`, which replaces the user's whole configuration, and `jellyfin_library_manage` `update_options`, which replaces all of a library's options; both are refused under `--disable-destructive`.
- **`jellyfin_devices` `revoke_api_key`** takes the key's `app_name`, because `api_keys` shows tokens masked; the full token still works as `key`.
- **`jellyfin_library_manage` `refresh_item`** runs the metadata and image providers. Without a refresh mode Jellyfin only rescanned the file, so the action fetched nothing unless `replace_all_metadata` was set.
- **`jellyfin_search`** with type `Person`, `MusicArtist`, or `Genre` searches names on their own endpoints; `/Items` never lists them.
- **`jellyfin_server` `update_config_section`** refuses a `livetv` section that carries redacted values from `get_config_section`, which would have replaced the real tuner URLs and credentials.
- **`health_check`** reports a section whose request failed as `error` and the status as `unknown` rather than `ok`. `library_stats` and `library_size` name a type whose request failed instead of leaving it out silently. `log_file` and `logs` refuse a `severity` or `log_type` they do not know instead of matching nothing.
- **`jellyfin_metadata`** field descriptions say that `genres`, `tags`, `studios`, and `locked_fields` replace the item's whole list and that `batch_update` writes the same values to every item. Prompts that fix metadata per item use `apply` or `update` on each item, and the duplicate finder describes its `type` as Movie or Series and confirms file paths before any deletion.
- **`jellyfin_play` and `jellyfin_playback_control`** are no longer annotated idempotent; queueing and track changes are not.
- **`size_mb` fields are megabytes**, as labeled; they were mebibytes. **`health_check`** log lines longer than 400 characters are cut at a word boundary, keeping the exception text. **`playback_history`** drops the `completed` flag, which duplicated `played`, and says that `progress` is the position saved by the latest play.
- **The `parental-controls` prompt** no longer promises a rating limit, which no tool can set; it tells the user where to set it in the dashboard.
- **`item_ids`** entries that hold comma-joined IDs are split in every tool that takes the field.
- **A list result never carries more items than the `Limit` asked for**, whatever the server sends.
- **`jellyfin_people` `persons`** leaves out music artists, which Jellyfin 12 would otherwise list among persons; `jellyfin_music` lists them on every version.
- **`resume`** and `jellyfin://resume` use Jellyfin's resume route, which leaves out what a session is playing now.
- **`jellyfin_search`** marks its total as a lower bound on Jellyfin 12 when the ranked candidate window is full.
- **`jellyfin_browse`** says when Jellyfin 12 lists a title once per version or part, which it does whenever `has_subtitles` or a language filter is set.
- **Analytics counts and `never_played`** leave out missing-episode placeholders.
- **The server instructions** say where download links come from, which tools answer questions about library size and disk space, and what `played` and `progress` mean side by side, and the playlist confirmations count one entry or item in the singular and describe a reorder as its last entries being re-added, without positions.
- **The typed tools' text is their structured JSON.** For `jellyfin_libraries`, `jellyfin_search`, `jellyfin_browse`, `jellyfin_get_item`, `jellyfin_recommendations`, `jellyfin_tv_shows`, `jellyfin_sessions`, and `jellyfin_analytics`, the text content is the serialized `structuredContent`, with no summary line, so a client that shows either sees every fact. Paging and scan facts are fields (`next_start_index`, `undated_count`, `unread_types`, `shown`, `days`, session `status`, `movie_recs` `categories`, `recently_played` `total_count`), guidance such as how to see more results is in `notes`, counts and lists are present when zero or empty, and a tool with several actions fills its own action's lists and leaves the other actions' fields out. Error results carry no `structuredContent`. Items embedded in the other tools' text share the same shape, so a zero runtime or rating is left out there as well.
- **TVDB links** for movies point at the movie page.
- **`jellyfin_browse` `min_date_created` and `max_date_created`** list items newest first by date added, with `date_added` on each, and examine at most the 2,000 most recently added matches. With either set, `sort_by` and `sort_order` must be omitted or `DateCreated` and `Descending`, and each date must be a date such as `2024-01-31` or an RFC 3339 timestamp.
- **HTTP authentication** accepts the `Bearer` scheme in any case and answers a refused request with a `WWW-Authenticate` challenge, as RFC 6750 describes. The token may come from the `HTTP_TOKEN` environment variable instead of `--http-token`, which keeps it out of the process list; `docker-compose.yml` passes it that way.
- **HTTP hardening:** a connection must send its headers within 10 seconds and its whole request within 5 minutes, idle connections close after 60 seconds, at most 2 requests of 1 MiB or more are in flight at once and the rest get 503 with `Retry-After`, and at most 256 stateful sessions exist at once. The loopback check for a tokenless bind parses the address instead of matching a prefix.
- **CI** runs `govulncheck`, grants only read permission except to the image push, and the release binaries build with the pinned Go release.
- **Reference guides are checked against the current Jellyfin documentation and rewritten.** The troubleshooting guide no longer tells you to delete `jellyfin.db` for a lost password, which since 10.11 also holds the library, and instead gives the documented password reset, account unlock, database-locked, and backup-restore steps. The transcoding guide lists the hardware each GPU generation supports as the docs state it, adds VideoToolbox and RKMPP, and turns on throttling with `EnableThrottling` rather than a delay that was already the default. The remote access guide carries the official Nginx and Apache configurations and the Known Proxies setting. The other guides drop numbers the docs don't support, name the migration tools the docs link, and cover the Jellyfin 12 plugin upgrade. The guides live as Markdown files under `internal/server/resources/guides`.
- **`jellyfin_collections` `add_items` and `remove_items`** and `jellyfin_videos` `merge_versions` accept IDs joined by commas within an `item_ids` entry.
- **`jellyfin_play` and `jellyfin_playback_control`** look the session up first. They refuse an ID that matches no active session, and a session whose client doesn't accept remote control, instead of sending the command.
- **`upload_subtitle`** waits up to 5 seconds for the server to list the new track, then reports how many external subtitle tracks the item has.
- **`jellyfin_plugins` `enable`, `disable`, and `uninstall`** refuse a plugin built into the server, which Jellyfin can't change and answered with a not-found error. `enable` and `disable` say that the change takes effect after a server restart.
- **`jellyfin_users` `update_policy`** requires at least one field before it asks for confirmation, and the confirmation lists the changes. `jellyfin_user_data` `set_user_data` checks its fields before it asks as well.
- **Results count one item in the singular**, in `jellyfin_playlists` `add_items`, `jellyfin_collections` `add_items` and `remove_items`, `jellyfin_metadata` `batch_update`, and `batch_download_subtitles`.
- **The `troubleshoot` prompt** lists every group of checks for a reported issue, each under the symptoms it fits, and the assistant runs the groups that apply. Before, a keyword in the issue chose one group, so an issue such as "can't play" got only the generic checks.
- Input descriptions state each action's actual default `limit`.
- Tool annotations always include `readOnlyHint` and `idempotentHint`, including when they are false (go-sdk 1.8.0).
- Bump Docker base images to `golang:1.27.1-alpine` (build) and `alpine:3.24` (runtime).
- Bump CI GitHub Actions `checkout` and `setup-go` to v7.

### Fixed

- **`jellyfin_analytics` `codec_report` and `duplicate_check`** read every item of the type, a page at a time. They stopped at 2,000 items without saying so, so a larger library got partial codec counts presented as the whole and missed duplicates past that point. **`jellyfin_devices` `list`** counts every device active in the window, where it counted only the ones shown. **`played_status`** names the users whose play state it could not read, usually because the item is in a library they can't see, instead of leaving them out silently.
- **`jellyfin_metadata` apply** sends the chosen provider ID, where before it sent none and left the item without any; `provider_name` is a `provider_ids` key such as `Tmdb`. Jellyfin still replaces the item's other provider IDs with the match.
- **`jellyfin_images`** `remote_download` and `upload` work; uploads accept JPEG, PNG, WebP, GIF, and BMP up to 20 MiB. **`upload_subtitle`** validates its format and base64.
- **`jellyfin_playlists`** `deduplicate` keeps each item's first entry instead of removing every copy, `move_item` lands where asked, and `add_items` sends the user. `remove_items` removes every entry of each item, as before, and says so. Changes require the configured user to own the playlist or be allowed to edit it. Long edits are sent in several requests, and on Jellyfin 12 an edit that would briefly empty the playlist of a user with a parental rating limit before adding entries back is refused, because Jellyfin would then refuse those entries. A plain removal proceeds and says when the remaining entries may disappear for that user.
- **`jellyfin_browse`** `min_date_created`/`max_date_created` and analytics `recently_added` filter by date added, and `total_is_lower_bound` marks a total that counts only the items examined. **`add_folder`** creates the library with its path. **`create_series_timer`** uses the server's timer defaults. **`SetVolume`** sends the level and refuses one outside 0–100.
- **Backups, task and device details, a new Quick Connect code, the Live TV guide range, channel details, and recording timers** give their times in the server's time zone, as every other tool result does. A task result's `StartTimeUtc` and `EndTimeUtc` become `StartTime` and `EndTime`.
- **The README's `claude mcp add` commands** put the server name before `-e`, which takes several values and swallowed the name, so the commands as written failed. The examples name the server `jellyfin-mcp`.
- **CONTRIBUTING's local run** exports the variables from `.env`; `source .env` alone left the server without its API key.
- **`jellyfin_library_manage` `browse_directory`** lists the files and folders at a path. It asked Jellyfin for neither, so it always returned an empty list.
- **`jellyfin_tasks` `stop`** on a task that isn't running says so, instead of passing on Jellyfin's server error.
- **`health_check`** counts fatal log entries as errors.
- **`health_check`** names the plugins that failed to load, with status Malfunctioned or NotSupported, under `failed_to_load`, and reports the status as `warnings`. It counts a plugin marked `Superceded`, the other spelling Jellyfin uses, among those that need a restart.
- **`jellyfin_browse`** under a library's `parent_id` no longer lists the folders of the library's paths as items, unless `type` asks for folders.
- **`jellyfin_devices`** lists the right devices for its `days` window in every time zone, and devices and log files sort correctly across a daylight saving change.
- Music genres come from the music libraries, including music videos; `jellyfin_people` `persons` asks for its whole limit at once, because Jellyfin 10.11's `/Persons` cannot be paged; and a failed page no longer returns a partial list as complete.
- **Subscriptions to `jellyfin://latest`** send change notifications. The poller named no user, so Jellyfin refused every poll (#25).
- **`jellyfin_live_tv` `tuners`** lists the tuner hosts that are set up, with their URLs redacted, instead of the kinds of tuner the server supports.
- **`jellyfin_subtitles_lyrics` `search_subtitles`** gives each result's `language` as a three-letter code such as `eng`; it was always empty.
- **Completions for `jellyfin://items/{itemId}` and `jellyfin://users/{userId}`** return the items and users whose names match the typed text. The matched IDs were then filtered by that text as a prefix, which no ID has, so the item lookup never returned anything and the user lookup only did for an empty value. `jellyfin_playlists` names `delete` among its valid actions.

### Removed

- **`make release`**, which ran the checks, published to npm, and pushed a tag in one command. A release now runs as separate steps, each checked before the next.
- MCP log notifications and the logging capability, which the protocol deprecates. The server still writes each call's timing to stderr.

### Security

- **`jellyfin_images` accepts only Jellyfin's image types for `image_type`.** The value is a path segment of the image routes, and a crafted value could reach other routes, including ones that restart or shut down the server, without confirmation.
- **Tuner URLs are shown only as their scheme and host**, in `tuners` and in `get_config_section`'s `livetv` section, and a URL that cannot be parsed is withheld; a value with no scheme, such as a device address, is shown as is. `get_config_section` also redacts listing-provider user names and passwords, which it used to show.
- An ID of `..` can no longer shorten a request path.

## v2026.604.2

### Security

- Update `github.com/modelcontextprotocol/go-sdk` to 1.6.1 (and `jsonschema-go` to 0.4.3), resolving two high-severity advisories:
  - **GHSA-q382-vc8q-7jhj** — improper handling of null Unicode characters during JSON parsing (pulls patched `segmentio/encoding` v0.5.4).
  - **CVE-2026-33252 / GHSA-89xv-2j6f-qhc8** — cross-site tool execution on the Streamable HTTP transport. 1.6.1 enforces `Content-Type: application/json` on POST and enables DNS-rebinding protection by default.
- Wrap the `/mcp` handler with `net/http` cross-origin protection so Origin / `Sec-Fetch-Site` is verified even in no-token localhost mode. Non-browser clients (MetaMCP, curl) are unaffected; browser cross-site POSTs are rejected with 403.

### Changed

- Bump Docker base images to `golang:1.26.4-alpine` (build) and `alpine:3.23` (runtime).
- Bump CI GitHub Actions to current majors (Node 24 runtime): `checkout` v6, `setup-go` v6, `setup-qemu` v4, `setup-buildx` v4, `login` v4, `build-push` v7, `metadata` v6.

## v2026.604.1

### Added

- **Docker support**: official multi-arch image (`linux/amd64`, `linux/arm64`) published to `ghcr.io/jaredtrent/jellyfin-mcp`, with a hardened `Dockerfile` and `docker-compose.yml`. See the Docker section of the README. Closes #1.

### Fixed

- Corrected `--http` / `--http-token` flag syntax (double dashes) in documentation and startup error messages.

## v2026.603.1

### Fixed

- **`jellyfin_play`**: send playback parameters as query parameters instead of a JSON request body. Jellyfin's `POST /Sessions/{id}/Playing` endpoint requires `playCommand` and `itemIds` as query params, so every play request previously failed with `400 — The playCommand field is required`. Playback now starts correctly. Thanks @perk11 for the fix (#2).

## v2026.318.7 — Initial release

- 31 tools across 8 toolsets: discovery, media, user, playback, admin, content, livetv, analytics
- 13 live MCP resources (server info, sessions, libraries, favorites, recently played, etc.)
- 18 guided prompt workflows (movie-night, troubleshoot, library-health, etc.)
- 10 built-in reference guides (transcoding, Docker, file naming, migration, etc.)
- Two transports: stdio and Streamable HTTP with bearer token auth
- Safety controls: read-only mode, disable-destructive, toolset scoping, confirmation gates
- Resource subscriptions with change-detection polling
- Auto-completion for prompt arguments and resource template URIs
- Structured MCP log notifications with timing data
- Platforms: Linux (x64, arm64), macOS (Apple Silicon, Intel), Windows (x64)
- npm package for MetaMCP and Docker-based gateways
