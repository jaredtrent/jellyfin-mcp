# jellyfin-mcp

[![CI](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev)
[![npm](https://img.shields.io/npm/v/@jaredtrent/jellyfin-mcp)](https://www.npmjs.com/package/@jaredtrent/jellyfin-mcp)

An MCP server that connects an AI assistant to your [Jellyfin](https://jellyfin.org) media server, with 31 tools, 13 live resources, and 19 guided workflows.

Once connected, the assistant searches your library, starts playback on your devices, fixes metadata, finds subtitles, and reads the server's logs when something goes wrong. It works from your own library, so a recommendation is always something you can play.

jellyfin-mcp is a fan project. It isn't associated with the Jellyfin project or its team.

**[Setup](#setup)** · **[Transport](#transport)** · **[Options](#options)** · **[MCP capabilities](#mcp-capabilities)** · **[Caveats](#caveats)**

## What It Does

- **Find and play media**: Search the library and start playback on any connected client.
- **Browse and filter**: Narrow by genre, year, studio, actor, rating, played status, and more.
- **Recommend**: Suggest titles from your own library, never from an external site.
- **Control playback**: Play, pause, seek, stop, set the volume, and skip on any device.
- **Manage playlists and collections**: Create, edit, and deduplicate playlists and box sets.
- **Play music**: Browse artists, albums, and genres, and build an instant mix.
- **Find subtitles**: Search, download, and audit a library for missing subtitles.
- **Fix metadata**: Correct titles, genres, ratings, and images, in batches or one item at a time, and re-identify items.
- **Troubleshoot the server**: Read the server log, find failed tasks, and diagnose playback problems.
- **Administer the server**: Manage users, library scans, scheduled tasks, plugins, devices, and backups.
- **Report on the library**: Watch history, codec reports, duplicate detection, and library statistics.
- **Live TV and DVR**: Guide data, channels, recordings, and series timers.
- **SyncPlay help**: A guide and a prompt for setting up and troubleshooting watch-together groups.

### Built-in Knowledge

Eleven reference guides give the assistant checked answers on Jellyfin setup and troubleshooting: transcoding, Docker, file naming, remote access, migrating from Plex or Emby, performance tuning, and more. See [resources](internal/server/resources) for the list.

### Safety Controls

- **Read-only mode**: `--read-only` registers only the tools that read.
- **No destructive actions**: `--disable-destructive` refuses deletes, removals, restores, uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, and password, policy, trigger, and whole-configuration changes, while every other action keeps working.
- **Toolset scoping**: `--toolsets` exposes only the tool groups you name.
- **Confirmation**: A destructive action confirms with you before it runs. A client that supports MCP form elicitation shows a confirmation form. Any other client receives a warning that the assistant presents, and the assistant repeats the call with `confirm=true` only after you agree. On a client older than MCP 2026-07-28, a form left unanswered for 10 minutes expires, and nothing is done. The assistant is also instructed to ask before any other write.
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

**Windows**: Download `jellyfin-mcp_windows_x64.zip` from [Releases](https://github.com/jaredtrent/jellyfin-mcp/releases/latest), extract `jellyfin-mcp.exe`, and move it to a folder you keep, such as `C:\Tools\jellyfin-mcp`. Note the full path; Claude needs it in the next step.

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

### 3. Connect Your MCP Client

Pick the client you use and follow its steps. In every example, replace the URL and API key with yours: `http://YOUR_SERVER:8096` for a standard install, or `https://YOUR_SERVER:8920` only if you turned on HTTPS in Jellyfin.

#### Claude Desktop

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

#### Claude Code

```sh
claude mcp add -s user jellyfin-mcp \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  -- jellyfin-mcp
```

`-s user` adds the server to every project; without it, Claude Code adds it to the current project only. The server name comes before `-e`, which takes several values. `claude mcp list` then shows jellyfin-mcp as connected. If you installed with Go, use the full path, such as `~/go/bin/jellyfin-mcp`, in place of the last `jellyfin-mcp`.

#### OpenCode

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

#### MetaMCP

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
      "args": ["-y", "@jaredtrent/jellyfin-mcp", "--read-only", "--toolsets", "discovery,media,playback"],
      "env": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

To connect over HTTP instead:

1. Run `jellyfin-mcp` in HTTP mode on the host:

   ```sh
   JELLYFIN_URL=http://YOUR_SERVER:8096 \
   JELLYFIN_API_KEY=your_api_key \
   jellyfin-mcp --http --http-token your_secret_token
   ```

2. In the MetaMCP dashboard, add a Streamable HTTP server with the URL `http://host.docker.internal:8080/mcp` and the bearer token `your_secret_token`.

`host.docker.internal` reaches the host from inside Docker on macOS and Windows. On Linux, add `--addr 0.0.0.0:8080` and use the host's LAN address in place of `host.docker.internal`.

## Transport

jellyfin-mcp speaks both MCP transports. Use the one your client requires.

| Transport | Flag | When to use it |
|-----------|------|----------------|
| stdio | none, the default | Claude Desktop, Claude Code, and most clients that launch a local process |
| Streamable HTTP | `--http` | MetaMCP in HTTP mode, or any client that connects to a URL |

Over **stdio**, the client starts `jellyfin-mcp` as a subprocess and talks to it on standard input and output. It needs no network setup and works with most clients.

Over **Streamable HTTP**, the server listens on an HTTP endpoint at `/mcp` that streams in both directions. Use it when the client can't run a local process, or when the server runs on a different machine from the client.

```sh
# Start in HTTP mode on localhost
jellyfin-mcp --http

# Listen on all interfaces with auth (required for non-localhost)
jellyfin-mcp --http --addr 0.0.0.0:8080 --http-token your_secret_token
```

The endpoint serves clients on every supported MCP protocol version. A client on protocol 2026-07-28 or later sends each request on its own, with no session. An older client gets a session, which times out after 30 minutes of inactivity. The HTTP server also answers `/health` with `{"status":"ok"}` for monitoring and load balancer checks. On either transport a message is limited to 32 MiB, which fits an image or subtitle upload of up to 20 MiB decoded.

## Docker

The GitHub Container Registry holds a multi-arch image for `linux/amd64` and `linux/arm64` at `ghcr.io/jaredtrent/jellyfin-mcp`. It runs the Streamable HTTP transport by default, serving the MCP endpoint at `/mcp` and a health check at `/health`.

```sh
docker run -d --name jellyfin-mcp -p 8080:8080 \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  ghcr.io/jaredtrent/jellyfin-mcp --http --addr 0.0.0.0:8080 --http-token your_secret_token
```

A bearer token is required when the server binds an address other than localhost, so always pass `--http-token`. Point the MCP client at `http://<host>:8080/mcp` and send `Authorization: Bearer your_secret_token`.

**Docker Compose**: The repository includes a [`docker-compose.yml`](docker-compose.yml) to edit. Set the Jellyfin URL, the API key, and the token, and `TZ` to give the server your time zone, then run `docker compose up -d`. The compose file passes the token to the server in the `HTTP_TOKEN` environment variable.

**stdio in Docker**: For a client that launches the server as a subprocess, override the entrypoint so it runs with no arguments:

```sh
docker run -i --rm \
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
| `--read-only` | Register only the tools that read; no writes, deletes, or other changes | off |
| `--disable-destructive` | Refuse destructive actions (deletes, removals, restores, uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, and password, policy, trigger, and whole-configuration changes) and allow every other write | off |
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

The 31 tools form eight groups. Registering only the groups you need, with `--toolsets discovery,media,...`, keeps the assistant's context small and its attention on the task. By default every group is registered.

| Toolset | Tools | Covers |
|---------|-------|--------|
| `discovery` | 7 | Search, browse, recommendations, item details, download links |
| `media` | 3 | TV shows, music, people |
| `user` | 3 | Favorites, playlists, collections |
| `playback` | 3 | Sessions, playback control, starting playback |
| `admin` | 8 | System, users, libraries, tasks, plugins, devices, server configuration |
| `content` | 4 | Metadata, subtitles, images, video versions |
| `livetv` | 2 | Channels, guide, recordings, DVR |
| `analytics` | 1 | Statistics, codec reports, duplicates |

For a search-and-play setup, start with `--toolsets discovery,media,playback`. Add `user` for playlists and collections, or `admin` for server maintenance. See [tools](internal/server/tools) for every tool with its description.

### Access Control Examples

```sh
# Casual use: search, browse, and play only (13 tools: discovery, media, and playback)
jellyfin-mcp --toolsets discovery,media,playback

# Shared family server: allow playlists and favorites, block all admin operations
jellyfin-mcp --toolsets discovery,media,user,playback

# Full access, but refuse deletes and restarts
jellyfin-mcp --disable-destructive

# Monitoring and analytics only: no writes at all
jellyfin-mcp --read-only --toolsets discovery,analytics
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

**Jellyfin version**: jellyfin-mcp supports Jellyfin 10.11 and 12.x, and logs a warning at startup when the server is older. A few features need Jellyfin 12: filtering by audio or subtitle language, an item's original language, and listing the collections that include an item. The activity log's filters work on both. Jellyfin 12 applies them itself, and on 10.11 jellyfin-mcp applies them to the 2,000 most recent entries.

**Single binary**: jellyfin-mcp is a statically compiled Go binary with no runtime dependencies. It needs no Node.js, Python, Java, or container runtime. The npm package is a delivery wrapper around the same binary.

## License

[MIT](LICENSE)

## AI Disclosure

AI tools helped write this project. Hard work went into SDK compliance, usability, and keeping the output helpful, accurate, and lean.
