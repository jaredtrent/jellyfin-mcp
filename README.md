# jellyfin-mcp

[![CI](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev)
[![npm](https://img.shields.io/npm/v/@jaredtrent/jellyfin-mcp)](https://www.npmjs.com/package/@jaredtrent/jellyfin-mcp)

jellyfin-mcp connects an AI assistant to your [Jellyfin](https://jellyfin.org) server, so you can ask about your library and manage it in plain language. The assistant answers from what you actually own and acts on your server, whether you're deciding what to watch, keeping the library in order, or looking after the server itself.

Ask for anything you'd otherwise click through Jellyfin to find or do. For example, the assistant can recommend a film you haven't seen for tonight, start the next episode on the living room TV, fix a poster that matched the wrong movie, find subtitles for a new download, or read the logs to work out why a stream keeps buffering. Destructive and high-impact changes—such as deletes, restarts, metadata edits, configuration updates, plugin installs, and API key creation—wait for your confirmation before running, and the assistant asks you before any other write.

It works with Claude Desktop, Claude Code, Open WebUI, and other MCP clients, through 25 tools, 13 live resources, and 19 guided workflows.

jellyfin-mcp is a fan project. It isn't associated with the Jellyfin project or its team.

**[Setup](#setup)** · **[Transport](#transport)** · **[Options](#options)** · **[MCP capabilities](#mcp-capabilities)** · **[Caveats](#caveats)**

## What It Does

- **Find and play media**: Search the library and start playback on any connected client.
- **Browse and filter**: Narrow by genre, year, studio, actor, rating, language, video resolution, played status, and more.
- **Recommend**: Suggest titles from your own library, never from an external site.
- **Control playback**: Play, pause, seek, stop, set the volume, and skip on any device.
- **Manage playlists and collections**: Create, edit, and deduplicate playlists and box sets.
- **Play music**: Browse artists, albums, and genres, and build an instant mix.
- **Find subtitles**: Search, download, and audit a library for missing subtitles.
- **Fix metadata**: Correct titles, genres, ratings, and images, in batches or one item at a time, and re-identify items.
- **Troubleshoot the server**: Read the server log, find failed tasks, and diagnose playback problems.
- **Administer the server**: Manage users, library scans, scheduled tasks, plugins, devices, and backups.
- **Report on the library**: Watch history, codec and resolution reports, duplicate detection, and library statistics.
- **Live TV and DVR**: Guide data, channels, recordings, and series timers.
- **SyncPlay help**: A guide and a prompt for setting up and troubleshooting watch-together groups.

### Built-in Knowledge

Eleven reference guides give the assistant checked answers on Jellyfin setup and troubleshooting: transcoding, Docker, file naming, remote access, migrating from Plex or Emby, performance tuning, and more. See [resources](internal/server/resources) for the list.

### Safety Controls

- **Read-only mode**: `--read-only` exposes only read-only tools and the read-only actions of mixed tools (pure write tools and write actions are omitted).
- **No destructive actions**: `--disable-destructive` refuses deletes, removals, metadata or image replacements, restores, plugin disables and uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, Quick Connect authorizations, and password, policy, trigger, and configuration changes, while every other action keeps working.
- **Toolset scoping**: `--toolsets` (and `/mcp?toolsets=...&read_only=true` over HTTP) exposes only the tool groups you name, with `find-*` and `monitor-*` groups strictly read-only.
- **Confirmation**: Destructive actions and high-impact writes (such as metadata edits, plugin installs, API key creation, play-state overrides, and batch subtitle downloads) confirm with you before running. A client that supports MCP form elicitation always shows a native confirmation form (an assistant cannot bypass it with `confirm=true`). Any other client receives a warning that the assistant presents, and the assistant repeats the call with `confirm=true` only after you agree. On a client older than MCP 2026-07-28, a form left unanswered for 10 minutes expires, and nothing is done. The assistant is also instructed to ask before any other write.
- **HTTP authentication**: The HTTP transport requires a bearer token whenever it listens beyond localhost.

### Prompts

Nineteen workflows, such as `movie-night`, `binge-watch`, `library-health`, `troubleshoot`, `duplicate-finder`, and `codec-optimize`, walk the assistant through a multi-step task. See [prompts](internal/server/prompts) for the full list.

## Setup

### 1. Get a Jellyfin API Key

1. Open the Jellyfin web interface.
2. Go to Dashboard > Advanced > API Keys.
3. Select + to create a key.
4. Name it, such as "MCP", and copy the key.

### 2. Install jellyfin-mcp

If you plan to run `jellyfin-mcp` in Docker (typical for Open WebUI and home servers), skip to [step 3](#3-start-the-http-server-web-and-remote-clients-only).

**macOS**: Open Terminal, paste these lines, and press Return. They download the build for your Mac's chip and put it in `/usr/local/bin`, asking once for your Mac password.

```sh
chip=$([ "$(uname -m)" = arm64 ] && echo apple-silicon || echo intel)
sudo mkdir -p /usr/local/bin
curl -fsSL "https://github.com/jaredtrent/jellyfin-mcp/releases/latest/download/jellyfin-mcp_macOS_$chip.tar.gz" | sudo tar -xz -C /usr/local/bin jellyfin-mcp
jellyfin-mcp --help
```

The last line prints the usage when the install worked. Run the same lines again to update to the newest release, and run `sudo rm /usr/local/bin/jellyfin-mcp` to uninstall.

**If you downloaded the archive in a browser instead**, macOS blocks the program the first time you or Claude run it, and shows a dialog that offers Move to Trash or Done. macOS does this for every downloaded program that Apple hasn't notarized, and jellyfin-mcp isn't notarized. Click Done, then clear the download flag in Terminal, using the path where you put the file:

```sh
xattr -d com.apple.quarantine /usr/local/bin/jellyfin-mcp
```

**Linux**: Paste these lines into a terminal. They download the build for your processor and put it in `/usr/local/bin`.

```sh
arch=$([ "$(uname -m)" = aarch64 ] && echo arm64 || echo x64)
curl -fsSL "https://github.com/jaredtrent/jellyfin-mcp/releases/latest/download/jellyfin-mcp_linux_$arch.tar.gz" | sudo tar -xz -C /usr/local/bin jellyfin-mcp
jellyfin-mcp --help
```

**Windows**: Download `jellyfin-mcp_windows_x64.zip` from [Releases](https://github.com/jaredtrent/jellyfin-mcp/releases/latest), extract `jellyfin-mcp.exe`, and move it to a folder you keep, such as `C:\Tools\jellyfin-mcp`. Note the full path; Claude needs it in [step 4](#4-connect-your-mcp-client).

<details>
<summary>Other ways to run jellyfin-mcp</summary>

| Method | Command | Requirements |
|--------|---------|--------------|
| Go install | `go install github.com/jaredtrent/jellyfin-mcp@latest` | [Go 1.26+](https://go.dev/dl/) |
| Go run | `go run github.com/jaredtrent/jellyfin-mcp@latest` | [Go 1.26+](https://go.dev/dl/) |
| npx | `npx -y @jaredtrent/jellyfin-mcp` | npm, on linux/x64 only |
| Docker | `docker pull ghcr.io/jaredtrent/jellyfin-mcp` | [Docker](https://docs.docker.com/get-docker/) |

**Go install** places the binary in `$GOPATH/bin`, usually `~/go/bin`, and builds the newest commit on the main branch. Use the full path, such as `/Users/you/go/bin/jellyfin-mcp`, in the client configurations below.

**Go run** needs no install step. Each launch resolves the latest version again and rebuilds, and that startup delay can make an MCP client time out. The client also needs `go` on its PATH. Install the binary for regular use.

**npx** bundles a compiled linux/x64 binary for MetaMCP and other Docker-based MCP gateways. On any other platform, use one of the methods above.

**Docker** publishes a multi-arch image for `linux/amd64` and `linux/arm64` to GHCR. It runs the Streamable HTTP transport by default. See the [Docker](#docker) section below.

</details>

### 3. Start the HTTP Server (Web and Remote Clients Only)

*Claude Desktop, Claude Code, OpenCode, and MetaMCP (`npx`) start `jellyfin-mcp` automatically (in `stdio` mode), so skip to [step 4](#4-connect-your-mcp-client) if you use one of those.*

Web apps such as Open WebUI (and clients on another machine) connect to a running `jellyfin-mcp` HTTP service instead:

1. Generate a random secret for `HTTP_TOKEN` (for example with `openssl rand -hex 32`). You will enter this same token in your MCP client.
2. Start `jellyfin-mcp` with **Docker** or the installed **binary**:

   **Docker** (runs in the background; see [Docker](#docker) for Compose):

   ```sh
   docker run -d --name jellyfin-mcp --restart unless-stopped -p 8080:8080 \
     -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
     -e JELLYFIN_API_KEY=your_api_key \
     -e HTTP_TOKEN=your_secret_token \
     ghcr.io/jaredtrent/jellyfin-mcp --http --addr 0.0.0.0:8080
   ```

   Drop `-p 8080:8080` if the client shares the same Docker network.

   **Binary** (runs in the foreground until stopped, or run it under `systemd` or `launchd`):

   ```sh
   JELLYFIN_URL=http://YOUR_SERVER:8096 \
   JELLYFIN_API_KEY=your_api_key \
   HTTP_TOKEN=your_secret_token \
   jellyfin-mcp --http --addr 0.0.0.0:8080
   ```

   On the same machine, you can omit `--addr 0.0.0.0:8080` (and `HTTP_TOKEN`) to listen only on `127.0.0.1:8080`.

3. Point your MCP client at `http://IP:PORT/mcp` (or `http://127.0.0.1:8080/mcp` on the same machine).

### 4. Connect Your MCP Client

Pick your client below and follow its steps. In every example, replace `http://YOUR_SERVER:8096` and `your_api_key` with yours (use `https://YOUR_SERVER:8920` only if you turned on HTTPS in Jellyfin).

<details>
<summary><strong>Claude Desktop</strong></summary>

1. Open the configuration file from Claude Desktop. On macOS, choose Claude > Settings… in the menu bar, open Developer, and click Edit Config.
2. Add the server. If the file is empty or holds only `{}`, replace its contents with this:

   ```json
   {
     "mcpServers": {
       "jellyfin-mcp": {
         "command": "/usr/local/bin/jellyfin-mcp",
         "env": {
           "JELLYFIN_URL": "http://YOUR_SERVER:8096",
           "JELLYFIN_API_KEY": "your_api_key"
         }
       }
     }
   }
   ```

   If the file already has an `mcpServers` section, add the `"jellyfin-mcp": { … }` entry inside it, with a comma between it and the entry before it.

   Claude Desktop doesn't use your terminal's PATH, so `command` is the full path to the program. On Windows, give the path to `jellyfin-mcp.exe` with each backslash doubled, such as `"C:\\Tools\\jellyfin-mcp\\jellyfin-mcp.exe"`.
3. Quit Claude Desktop completely, with Claude > Quit Claude or Command-Q, and open it again. Closing the window leaves it running.
4. Check the connection: click + in the message box, point to Connectors, and look for jellyfin-mcp. Then ask Claude "What libraries do I have?"

If jellyfin-mcp doesn't appear, its log says why: `~/Library/Logs/Claude/mcp-server-jellyfin-mcp.log` on macOS, or the `logs` folder in `%APPDATA%\Claude` on Windows.

</details>

<details>
<summary><strong>Claude Code</strong></summary>

```sh
claude mcp add -s user jellyfin-mcp \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  -- jellyfin-mcp
```

`-s user` adds the server to every project; without it, Claude Code adds it to the current project only. The server name comes before `-e`, which takes several values. `claude mcp list` then shows jellyfin-mcp as connected. If you installed with Go, use the full path, such as `~/go/bin/jellyfin-mcp`, in place of the last `jellyfin-mcp`.

</details>

<details>
<summary><strong>OpenCode</strong></summary>

Add the server to `~/.config/opencode/opencode.json`, or to `opencode.json` in the project root:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jellyfin-mcp": {
      "type": "local",
      "command": ["jellyfin-mcp"],
      "enabled": true,
      "environment": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

To run with Go instead of an installed binary, set `"command": ["go", "run", "github.com/jaredtrent/jellyfin-mcp@latest"]`.

</details>

<details>
<summary><strong>Open WebUI</strong></summary>

Open WebUI 0.6.31+ connects over Streamable HTTP directly, with no `mcpo` proxy; see the [Open WebUI MCP docs](https://docs.openwebui.com/features/extensibility/mcp).

1. Make sure `jellyfin-mcp` is running in HTTP mode somewhere ([step 3](#3-start-the-http-server-web-and-remote-clients-only)).
2. In Open WebUI, open Admin Panel > Settings > Tools > Integrations.
3. Click + next to External Tool Servers.
4. Enter the following, then click Save:
   - **Type**: Click `OpenAPI` to switch it to `MCP Streamable HTTP`
   - **Name**: `jellyfin-mcp` (fills **Connection ID** automatically)
   - **URL**: Your `/mcp` endpoint from [step 3](#3-start-the-http-server-web-and-remote-clients-only), or a scoped URL such as `http://IP:PORT/mcp?toolsets=find-media` (see [HTTP Toolset Endpoints](#http-toolset-endpoints))
   - **Auth**: `Bearer` with your `HTTP_TOKEN` (or `None` on unauthenticated localhost)
   - **Access Control**: Optional; grant access here if non-admin users should see the server
5. Enable `jellyfin-mcp` per chat under Integrations > Tools in the message box, or attach it to a preset in [Workspace > Models](https://docs.openwebui.com/features/workspace/models).
6. Keep [Function Calling](https://docs.openwebui.com/reference/server-side-tool-calling) on `Native` (the default).

**Notes**:

- **MCP feature gaps**: Open WebUI does not yet expose **prompts** or **resources**.
- **Shared instances**: If you set up a single model and connection, everyone using that model acts as the same Jellyfin user and shares the same API key.

**Context usage**:

All 25 tools add **~8,450 tokens** per chat (~7,800 across tool schemas plus ~650 for server instructions; see [Toolsets](#toolsets)). On local runners such as Ollama, raise `num_ctx` to at least `16384` ([context window docs](https://docs.openwebui.com/troubleshooting/context-window)), or load fewer schemas:

- **Keep Tool Search off**: Open WebUI's client-side Tool Search has reliability issues with multi-action MCP tools; use `/mcp?toolsets=...` URL query scoping below so the server sends only the schemas you need.
- **Dedicated [Workspace Models](https://docs.openwebui.com/features/workspace/models) via URL query scoping (0 tokens in normal chats)**: Point separate Open WebUI connections at the same HTTP service with `?toolsets=...` (each connection gets server-side schema projections and read-only instructions automatically, without manual `Function Name Filter List` maintenance):
  - *Browse* (`/mcp?toolsets=find-media`, read-only): ~3,600 tokens
  - *Watch* (`/mcp?toolsets=find-media,control-playback`): ~4,150 tokens (~4,500 with `my-media`)
  - *Curate* (`/mcp?toolsets=find-media,manage-media`): ~4,900 tokens
  - *Monitor* (`/mcp?toolsets=monitor-server,monitor-media`, read-only): ~2,400 tokens
  - *Admin* (`/mcp?toolsets=admin-server,monitor-media`, restricted in `Access Control`): ~3,450 tokens
- **Server-side flags**: Start `jellyfin-mcp` with `--toolsets find-media,control-playback` (~4,150 tokens) or `--read-only` (23 tools, ~5,550 tokens)—URL query parameters can only narrow, never widen, these server flags—or run a separate `--disable-destructive` container with a non-admin `JELLYFIN_USER_ID` for household accounts.

</details>

<details>
<summary><strong>MetaMCP</strong></summary>

MetaMCP runs in Docker with `npx` installed, so the npm package runs the server over stdio with no HTTP setup:

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

With flags:

```json
{
  "mcpServers": {
    "jellyfin-mcp": {
      "command": "npx",
      "args": ["-y", "@jaredtrent/jellyfin-mcp", "--toolsets", "find-media,monitor-media"],
      "env": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

To connect over HTTP instead:

1. Make sure `jellyfin-mcp` is running in HTTP mode ([step 3](#3-start-the-http-server-web-and-remote-clients-only)).
2. In the MetaMCP dashboard, add a `Streamable HTTP` server with your `/mcp` URL (or a scoped `/mcp?toolsets=...` URL; see [HTTP Toolset Endpoints](#http-toolset-endpoints)) and `HTTP_TOKEN` as the bearer token.

</details>

## Transport

jellyfin-mcp speaks both MCP transports. Use the one your client requires.

| Transport | Flag | When to use it |
|-----------|------|----------------|
| stdio | none, the default | Claude Desktop, Claude Code, OpenCode, and most clients that launch a local process |
| Streamable HTTP | `--http` | Open WebUI, MetaMCP in HTTP mode, or any client that connects to a URL |

Over **stdio**, the client starts `jellyfin-mcp` as a subprocess and talks to it on standard input and output. It needs no network setup and works with most desktop and CLI clients.

Over **Streamable HTTP**, the server listens on an HTTP endpoint at `/mcp` that streams in both directions (see [step 3](#3-start-the-http-server-web-and-remote-clients-only)). Clients can append `?toolsets=...` and `?read_only=true` to `/mcp` to scope each connection's exposed tool schemas and instructions on a single running server; see [HTTP Toolset Endpoints](#http-toolset-endpoints).

The HTTP endpoint serves clients on every supported MCP protocol version. A client on protocol 2026-07-28 or later sends each request on its own, with no session. An older client gets a session, which times out after 30 minutes of inactivity. The HTTP server also answers `/health` with `{"status":"ok"}` for monitoring and load balancer checks. On either transport a message is limited to 32 MiB, which fits an image or subtitle upload of up to 20 MiB decoded.

## Docker

The GitHub Container Registry publishes a multi-arch image (`linux/amd64` and `linux/arm64`) at `ghcr.io/jaredtrent/jellyfin-mcp` that serves Streamable HTTP at `/mcp` and `/health` by default; see [step 3](#3-start-the-http-server-web-and-remote-clients-only) for `docker run`.

**Docker Compose**: Edit [`docker-compose.yml`](docker-compose.yml) with `JELLYFIN_URL`, `JELLYFIN_API_KEY`, `HTTP_TOKEN`, and `TZ` in a `.env` file alongside it, then run `docker compose up -d`.

**stdio in Docker**: For a client that launches the server as a subprocess, override the entrypoint so it runs with no arguments:

```sh
docker run -i --rm --no-healthcheck \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  --entrypoint /usr/local/bin/jellyfin-mcp \
  ghcr.io/jaredtrent/jellyfin-mcp
```

Image tags: `latest` is the newest release, a release also gets its full version, such as `2026.603.1`, and its `MAJOR.MINOR`, and `edge` is the latest build from `main`.

## Options

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--toolsets` | Comma-separated toolset groups to register | all |
| `--read-only` | Expose only read-only tools and read-only actions of mixed tools (pure write tools and write actions are omitted) | off |
| `--disable-destructive` | Refuse destructive actions (deletes, removals, metadata or image replacements, restores, plugin disables and uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, Quick Connect authorizations, and password, policy, trigger, and configuration changes) and allow every other write | off |
| `--http` | Serve Streamable HTTP instead of stdio | off |
| `--addr` | HTTP listen address | `127.0.0.1:8080` |
| `--http-token` | Bearer token for HTTP authentication, required when listening beyond localhost. The `HTTP_TOKEN` environment variable supplies it instead. | none |

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `JELLYFIN_API_KEY` | Yes | The API key from the Jellyfin dashboard |
| `JELLYFIN_URL` | No | The server URL, such as `http://YOUR_SERVER:8096`, or `https://YOUR_SERVER:8920` if HTTPS is turned on. The default is a placeholder, so set it. |
| `HTTP_TOKEN` | No | The bearer token for `--http`. It replaces `--http-token` and keeps the token out of the process list. |
| `JELLYFIN_USER_ID` | No | The user that user-scoped calls act as, by user ID or username. Without it, a user token acts as its own user, and an API key acts as the first administrator. |
| `TZ` | No | The time zone, such as `America/New_York`, in which the server states today's date, reads dates such as `2024-01-31` in tool inputs, and gives the dates and times in tool results. It defaults to the system time zone, which is UTC in the Docker image. A name the system doesn't know falls back to UTC with a warning at startup. Windows ignores `TZ` and always uses the system time zone. |

### Toolsets

The 25 tools are organized into eight `<verb>-<noun>` groups (~7,800 schema tokens in total, plus ~650 tokens for server instructions). Groups starting with **`find-`** or **`monitor-`** are strictly read-only (`ReadOnlyHint=true`, exposing read-only projections of mixed tools), while **`my-`**, **`control-`**, **`manage-`**, **`admin-`**, and **`live-tv`** unlock scoped writes. Legacy single-noun names (`discovery`, `media`, `user`, `playback`, `content`, `analytics`, `admin`, `manage-server`, `livetv`) remain accepted as aliases.

| Toolset | Mode | Tools | Schema tokens | Covers |
|---------|------|-------|---------------|--------|
| `find-media` | Read-only | 14 (9 + 5 RO) | ~3,000 | Search, browse, item details, recommendations, TV/music navigation, catalog and size/codec/duplicate reports, plus read-only views of user data, playlists, metadata, subtitles/lyrics, and images |
| `monitor-media` | Read-only | 2 | ~550 | Verified user watch history, per-item household watch status, most-played and unwatched-by-all stats, and active sessions (with client version and IP) |
| `monitor-server` | Read-only | 7 (2 + 5 RO) | ~1,400 | System info, health checks, storage, logs, activity log (with client IPs), sessions, plus read-only views of server settings, tasks, backups, users, devices, plugins, and library folders/drives |
| `my-media` | Read + Write | 2 | ~650 | Personal favorites, ratings, played state, and playlist creation/editing |
| `control-playback` | Read + Write | 2 | ~520 | Active sessions, starting/queueing playback, and remote transport/volume/message control |
| `manage-media` | Read + Write | 5 (4 + 1 Curate) | ~1,800 | Collections, metadata edits and version merging/splitting, subtitles/lyrics, artwork, plus curator library scans and metadata refreshes (`list_folders`, `scan`, `refresh_item`) |
| `admin-server` | Read + Write | 6 | ~2,250 | System diagnostics, user accounts and policies, virtual library folder/path configuration and item deletion, plugins, devices/API keys, scheduled tasks, backups, server settings, and power |
| `live-tv` | Read + Write | 1 | ~300 | Live TV channels, guide, tuners, recordings, and DVR timers (projected to read-only under `--read-only`) |

Because overlapping groups merge cleanly, combine only what each role needs—for example `find-media,control-playback` to browse and play, `find-media,my-media,control-playback` for a household member's personal account, `find-media,manage-media` for library curation, or `monitor-server,monitor-media` for read-only server and playback observability. See [tools](internal/server/tools) for every tool and projection.

### HTTP Toolset Endpoints

When running in HTTP mode (`--http`), a single `jellyfin-mcp` instance can serve tailored toolsets to different clients, agent roles, or Open WebUI Workspace Models through query parameters on `/mcp`—without running separate containers or maintaining client-side tool filter lists:

- **`toolsets=<group>[,<group>...]`**: Exposes only the named `<verb>-<noun>` groups (repeating `toolsets=` is also accepted, as are legacy aliases). Endpoints composed solely of `find-*` and `monitor-*` groups automatically project mixed tools down to their read-only actions (`ReadOnlyHint=true`) and send read-only server instructions.
- **`read_only=true`** (or `1`): Forces read-only tool projections and read-only server instructions on write-capable groups as well (such as `/mcp?toolsets=live-tv&read_only=true` for TV guide browsing without DVR writes).

Any single group or comma-separated combination from [Toolsets](#toolsets) works on `/mcp`:

| Endpoint | Mode | Tools | Total context | Typical use |
|----------|------|-------|---------------|-------------|
| `/mcp` | Server default | 25 | ~8,450 tokens | Full server capability (or whatever `--toolsets` / `--read-only` the server started with) |
| `/mcp?toolsets=find-media` | Read-only | 14 | ~3,600 tokens | Search, browse, recommendations, and catalog/codec/size reports |
| `/mcp?toolsets=monitor-media` | Read-only | 2 | ~1,150 tokens | User watch history, per-item household play counts, and active sessions |
| `/mcp?toolsets=monitor-server` | Read-only | 7 | ~2,000 tokens | System health, storage, logs, activity log, and read-only config/user/plugin inspection |
| `/mcp?toolsets=monitor-server,monitor-media` | Read-only | 8 | ~2,400 tokens | Full read-only server and playback observability |
| `/mcp?toolsets=find-media,control-playback` | Read + Write | 16 | ~4,150 tokens | Living-room discovery and remote playback control |
| `/mcp?toolsets=find-media,my-media,control-playback` | Read + Write | 16 | ~4,500 tokens | Household preset (adds personal favorites, ratings, and playlists) |
| `/mcp?toolsets=find-media,manage-media` | Read + Write | 16 | ~4,900 tokens | Library curation (collections, metadata, subtitles, artwork, library scans) |
| `/mcp?toolsets=admin-server,monitor-media` | Read + Write | 8 | ~3,450 tokens | Server administration and playback diagnostics |
| `/mcp?toolsets=live-tv` | Read + Write | 1 | ~1,000 tokens | Live TV guide, channels, recordings, and DVR timers (`&read_only=true` for guide-only) |
| `/mcp?read_only=true` | Read-only | 23 | ~5,550 tokens | Every enabled toolset projected to read-only actions |

*(Tool and token counts above assume the server started with all toolsets enabled; total context includes tool schemas plus ~600–650 tokens of server instructions.)*

**Scoping and security rules**:

- **Strict narrowing (never widens)**: Query parameters intersect with the server's startup `--toolsets`, `--read-only`, and `--disable-destructive` flags. Requesting a toolset omitted from `--toolsets` exposes none of that group's tools, and `read_only=false` cannot override `--read-only`.
- **Fail-closed validation**: Unsupported query parameters, unknown toolset names, empty `toolsets=`, invalid `read_only` values, or malformed query strings return `400 Bad Request` instead of falling back to `/mcp`.
- **Shared authentication**: Every `/mcp?...` endpoint requires the same `Authorization: Bearer <HTTP_TOKEN>` header as `/mcp` (`/health` stays unauthenticated for health checks).

### Access Control Examples

```sh
# Read-only discovery and catalog reports (no write actions registered)
jellyfin-mcp --toolsets find-media

# Shared household setup: browse, play, and manage personal favorites/playlists
jellyfin-mcp --toolsets find-media,my-media,control-playback

# Library curation without server or user administration
jellyfin-mcp --toolsets find-media,manage-media

# Read-only server health and playback telemetry
jellyfin-mcp --toolsets monitor-server,monitor-media

# Full access, but refuse deletes, replacements, and restarts
jellyfin-mcp --disable-destructive
```

## MCP Capabilities

Beyond tools, jellyfin-mcp implements the MCP features below for clients that support them.

**Resources**: Thirteen live data endpoints the assistant reads without a tool call, including server info, the library list, active sessions, now playing, favorites, and recently played. See [resources](internal/server/resources) for the full list.

**Resource subscriptions**: A client can subscribe to the session and content resources and receive a notification when they change. The server polls sessions every 10 seconds and content, such as latest additions and recently played, every 60 seconds, and it polls a group only while a client subscribes to one of its resources. A client's subscriptions end with its session. Over stdio, and on MCP 2026-07-28 when its subscription stream closes, that is when the client disconnects. An HTTP client on an older protocol keeps its session until it closes the session or 30 minutes pass without a request, so a client that only listens for updates must send a request, such as a ping, at least every 30 minutes to stay subscribed.

**Prompts**: Nineteen workflows the assistant invokes for multi-step tasks. See [prompts](internal/server/prompts) for the full list.

**Completions**: Prompt arguments and resource template URIs complete as you type. Fixed vocabularies such as genres and language codes complete from a list, and library names, usernames, and item, user, and library IDs complete from the server.

**Logging**: The server writes each tool call and its duration to stderr for local debugging. It sends no MCP log notifications, which the current protocol deprecates, and doesn't advertise the logging capability.

## Caveats

**API key permissions**: The API key carries every permission Jellyfin gives an API key. On a shared or less trusted server, pair it with `--read-only` or `--toolsets` to limit what the assistant can do. jellyfin-mcp sends the key in the `Authorization` header, which every supported Jellyfin version accepts, so no server setting needs to change.

**Network exposure**: Over stdio, only the local MCP client process reaches the server. Over HTTP, pass `--http-token` whenever the server is reachable beyond localhost. The server refuses to start on an address other than localhost without a token.

**Jellyfin version**: jellyfin-mcp supports Jellyfin 10.11 and 12.x, and logs a warning at startup when the server is older. A few features need Jellyfin 12: filtering by audio or subtitle language, filtering series by resolution, showing an item's original language, and showing which collections an item belongs to. Activity log filters work on both: Jellyfin 12 filters on the server, and on 10.11 jellyfin-mcp scans the 2,000 newest entries.

**Single binary**: jellyfin-mcp is a statically compiled Go binary with no runtime dependencies. It needs no Node.js, Python, Java, or container runtime. The npm package is a delivery wrapper around the same binary.

## License

[MIT](LICENSE)

## AI Disclosure

AI tools helped write this project. Hard work went into SDK compliance, usability, and keeping the output helpful, accurate, and lean.
