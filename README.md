# jellyfin-mcp

[![CI](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jaredtrent/jellyfin-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://go.dev)
[![npm](https://img.shields.io/npm/v/@jaredtrent/jellyfin-mcp)](https://www.npmjs.com/package/@jaredtrent/jellyfin-mcp)

MCP server that connects AI assistants to your [Jellyfin](https://jellyfin.org) media server — 31 tools, 13 live resources, and 18 guided workflows. Search your library, control playback, manage metadata, find subtitles, troubleshoot your server, and more.

This project is not associated with the official Jellyfish project or team. It's a fan project.

**[Setup](#setup)** · **[Transport](#transport)** · **[Options](#options)** · **[MCP capabilities](#mcp-capabilities)** · **[Important notes](#important-notes)**

## What can it do?

- **Find and play media** — search your library and start playback on any connected client
- **Browse and filter** — by genre, year, studio, actor, rating, played status, and more
- **Recommendations** — personalized suggestions from your own library, not external sites
- **Control playback** — play, pause, seek, stop, volume, next/previous on any device
- **Playlists and collections** — create, manage, deduplicate playlists and box set collections
- **Music** — browse artists, albums, genres; generate instant mix playlists
- **Subtitles** — search, download, and audit missing subtitles across your library
- **Metadata management** — fix titles, genres, ratings, images; batch updates; re-identify items
- **Troubleshoot your server** — read server logs, check failed tasks, diagnose playback issues
- **Server admin** — user management, library scans, scheduled tasks, plugins, devices, backups
- **Analytics** — watch history, codec reports, duplicate detection, library stats
- **Live TV and DVR** — guide data, channels, recordings, series timers
- **SyncPlay** — synchronized group watching sessions

### Built-in knowledge

Includes 10 reference guides the AI can consult to help with Jellyfin setup and troubleshooting — transcoding, Docker, file naming, remote access, migrating from Plex/Emby, performance tuning, and more.

### Safety controls

- **Read-only mode** — prevent all writes with `--read-only`
- **Disable destructive operations** — block deletes, restarts, and shutdowns with `--disable-destructive`
- **Toolset scoping** — expose only the tool groups you need with `--toolsets`
- **Confirmation required** — destructive operations require explicit `confirm=true`; the AI is instructed to ask the user before any write operation
- **HTTP authentication** — bearer token auth for HTTP transport

### Prompts

18 pre-built workflows like `movie-night`, `binge-watch`, `library-health`, `troubleshoot`, `duplicate-finder`, and `codec-optimize` that guide the AI through multi-step tasks. See [prompts](internal/server/prompts) for the full list.

## Setup

### 1. Get a Jellyfin API key

1. Open your Jellyfin web UI
2. Go to **Dashboard > Advanced > API Keys**
3. Click **+** to create a new key
4. Give it a name (e.g., "MCP") and copy the key

### 2. Install jellyfin-mcp

| Method | Command | Requirements |
|--------|---------|--------------|
| Binary | Download from [Releases](https://github.com/jaredtrent/jellyfin-mcp/releases) | None |
| Go install | `go install github.com/jaredtrent/jellyfin-mcp@latest` | [Go 1.25+](https://go.dev/dl/) |
| Go run | `go run github.com/jaredtrent/jellyfin-mcp@latest` | [Go 1.25+](https://go.dev/dl/) |
| npx | `npx -y @jaredtrent/jellyfin-mcp` | npm (linux/x64 only) |
| Docker | `docker pull ghcr.io/jaredtrent/jellyfin-mcp` | [Docker](https://docs.docker.com/get-docker/) |

<details>
<summary>Detailed instructions for each method</summary>

**Binary** — download and extract:

1. Download the archive for your platform from [Releases](https://github.com/jaredtrent/jellyfin-mcp/releases)
2. Extract it: `tar xzf jellyfin-mcp_*.tar.gz` (or unzip on Windows)
3. Move the binary somewhere on your PATH: `sudo mv jellyfin-mcp /usr/local/bin/`
4. Verify: `jellyfin-mcp --help`

**Go install** — places the binary in `$GOPATH/bin` (usually `~/go/bin`). Make sure that directory is on your PATH, then verify: `jellyfin-mcp --help`. (GUI clients may not see `~/go/bin` — see the Claude Desktop note below.)

**Go run** — no install step, best for a quick try. `go run …@latest` re-resolves the latest version and rebuilds on each launch, adding startup latency that can make MCP clients time out; `go` must also be on the client's PATH. Install the binary for regular use.

**npx** — bundles a pre-compiled linux/x64 binary. Intended for MetaMCP and other Docker-based MCP gateways. For other platforms, use one of the methods above.

**Docker** — a multi-arch image (`linux/amd64`, `linux/arm64`) published to GHCR. Runs the Streamable HTTP transport by default. See the [Docker](#docker) section below.

</details>

### 3. Connect to your MCP client

Pick the client you use and follow the steps below. In every example, replace the URL and API key with yours — use `http://YOUR_SERVER:8096` for a standard install, or `https://YOUR_SERVER:8920` only if you've enabled HTTPS in Jellyfin.

#### Claude Desktop

1. Open your Claude Desktop config file:
   - macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
   - Windows: `%APPDATA%\Claude\claude_desktop_config.json`
   - Linux: `~/.config/Claude/claude_desktop_config.json`
2. Add the following:

**If you installed the binary:**

```json
{
  "mcpServers": {
    "jellyfin": {
      "command": "jellyfin-mcp",
      "env": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

If Claude can't find the binary, use its full path instead of `"jellyfin-mcp"` — GUI apps don't inherit your shell PATH, so a `go install` binary in `~/go/bin` is often invisible to them. Run `which jellyfin-mcp` (macOS/Linux) or `where jellyfin-mcp` (Windows) to get the path.

**Or skip the install — run directly with Go** (recompiles each launch — slower startup, best for a quick try):

```json
{
  "mcpServers": {
    "jellyfin": {
      "command": "go",
      "args": ["run", "github.com/jaredtrent/jellyfin-mcp@latest"],
      "env": {
        "JELLYFIN_URL": "http://YOUR_SERVER:8096",
        "JELLYFIN_API_KEY": "your_api_key"
      }
    }
  }
}
```

Restart Claude Desktop for the changes to take effect.

#### Claude Code

```sh
# If installed:
claude mcp add \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  jellyfin -- jellyfin-mcp

# Or run directly with Go:
claude mcp add \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  jellyfin -- go run github.com/jaredtrent/jellyfin-mcp@latest
```

#### OpenCode

Add to `~/.config/opencode/opencode.json` (or `opencode.json` in your project root):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jellyfin": {
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

MetaMCP runs in Docker with `npx` pre-installed. Use the npm package to run via stdio (no HTTP setup needed):

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

With CLI flags:

```json
{
  "mcpServers": {
    "jellyfin": {
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

Alternatively, connect via HTTP transport:

1. Run `jellyfin-mcp` in HTTP mode on your host:

```sh
JELLYFIN_URL=http://YOUR_SERVER:8096 \
JELLYFIN_API_KEY=your_api_key \
jellyfin-mcp --http --http-token your_secret_token
```

2. In the MetaMCP dashboard, add a new **Streamable HTTP** server:
   - **URL**: `http://host.docker.internal:8080/mcp`
   - **Bearer Token**: `your_secret_token`

`host.docker.internal` reaches the host from inside Docker (macOS/Windows). On Linux, add `--addr 0.0.0.0:8080` and use your host's LAN IP instead of `host.docker.internal`.

## Transport

jellyfin-mcp supports two transport modes. Use whichever your MCP client requires.

| Transport | Flag | When to use |
|-----------|------|-------------|
| **stdio** | *(default)* | Claude Desktop, Claude Code, and most MCP clients that launch a local process |
| **Streamable HTTP** | `--http` | MetaMCP (HTTP mode), or any client that connects to a remote URL |

**stdio** — the server communicates over stdin/stdout. The MCP client starts `jellyfin-mcp` as a subprocess and manages its lifecycle. This is the simplest setup and works with most clients.

**Streamable HTTP** — the server listens on an HTTP endpoint (`/mcp`) that supports bidirectional streaming. Use this when your MCP client can't run a local process or when you want to run the server on a different machine from the client.

```sh
# Start in HTTP mode on localhost
jellyfin-mcp --http

# Listen on all interfaces with auth (required for non-localhost)
jellyfin-mcp --http --addr 0.0.0.0:8080 --http-token your_secret_token
```

The HTTP server also exposes `/health` (returns `{"status":"ok"}`) for monitoring and load balancer health checks. Sessions time out after 30 minutes of inactivity.

## Docker

A multi-arch image (`linux/amd64`, `linux/arm64`) is published to the GitHub Container Registry: **`ghcr.io/jaredtrent/jellyfin-mcp`**. It runs the Streamable HTTP transport by default, serving the MCP endpoint at `/mcp` and a health check at `/health`.

```sh
docker run -d --name jellyfin-mcp -p 8080:8080 \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  ghcr.io/jaredtrent/jellyfin-mcp --http --addr 0.0.0.0:8080 --http-token your_secret_token
```

A bearer token is **required** when binding a non-localhost address, so always pass `--http-token`. Point your MCP client at `http://<host>:8080/mcp` and send `Authorization: Bearer your_secret_token`.

**docker compose** — a ready-to-edit [`docker-compose.yml`](docker-compose.yml) is included. Set your Jellyfin URL/key and token, then `docker compose up -d`.

**stdio in Docker** — for clients that launch the server as a subprocess, override the entrypoint so it runs with no arguments:

```sh
docker run -i --rm \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  --entrypoint /usr/local/bin/jellyfin-mcp \
  ghcr.io/jaredtrent/jellyfin-mcp
```

Image tags: `latest` (latest build from `main`), the full version from release tags (e.g. `2026.603.1`), and `MAJOR.MINOR`.

## Options

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--toolsets` | Comma-separated toolset groups to enable | all |
| `--read-only` | Only register read-only tools — no writes, deletes, or mutations | off |
| `--disable-destructive` | Skip destructive tools (delete, restart, shutdown) while allowing other writes | off |
| `--http` | Run as Streamable HTTP server instead of stdio | off |
| `--addr` | HTTP listen address | `127.0.0.1:8080` |
| `--http-token` | Bearer token for HTTP authentication (required when listening on non-localhost) | none |

### Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `JELLYFIN_API_KEY` | Yes | API key from your Jellyfin dashboard |
| `JELLYFIN_URL` | Yes | Jellyfin server HTTP(S) URL, for example `http://YOUR_SERVER:8096` or `https://YOUR_SERVER:8920`. The `jellyfin_host` placeholder is rejected at startup. |
| `JELLYFIN_USER_ID` | No | User ID, auto-detected from the API key when omitted |
| `JELLYFIN_REQUIRE_USER_ID` | No | When `true` or `1`, require `JELLYFIN_USER_ID` and disable automatic user selection, including admin fallback |

### Toolsets

31 tools organized into 8 groups. Enable specific groups with `--toolsets discovery,media,...` to reduce context size and keep the AI focused. By default, all toolsets are enabled.

| Toolset | Tools | Covers |
|---------|-------|--------|
| `discovery` | 6 | Search, browse, recommendations, item details |
| `media` | 3 | TV shows, music, people |
| `user` | 3 | Favorites, playlists, collections |
| `playback` | 4 | Sessions, playback control, SyncPlay |
| `admin` | 8 | System, users, libraries, tasks, plugins, devices, server config |
| `content` | 4 | Metadata, subtitles, images, video versions |
| `livetv` | 2 | Channels, guide, recordings, DVR |
| `analytics` | 1 | Stats, codec reports, duplicates |

For a casual "search and play" setup, `--toolsets discovery,media,playback` is a good starting point. Add `user` for playlist/collection management or `admin` for server maintenance. See [tools](internal/server/tools) for the full list with descriptions.

### Access control examples

```sh
# Casual use — search, browse, and play only (13 tools: read-only discovery/media + playback)
jellyfin-mcp --toolsets discovery,media,playback

# Shared family server — allow playlists and favorites, block all admin operations
jellyfin-mcp --toolsets discovery,media,user,playback

# Full access, but protect against accidental deletes/restarts
jellyfin-mcp --disable-destructive

# Monitoring/analytics only — no writes at all
jellyfin-mcp --read-only --toolsets discovery,analytics
```

## MCP capabilities

Beyond tools, jellyfin-mcp implements several MCP protocol features that compatible clients can use.

**Resources** — 13 live data endpoints the AI can read without a tool call. These include server info, library lists, active sessions, now-playing, favorites, recently played, and more. Clients that support MCP resources can access these directly for quick lookups. See [resources](internal/server/resources) for the full list.

**Resource subscriptions** — Clients can subscribe to session and content resources for real-time change notifications. Sessions are polled every 10 seconds; content (latest additions, recently played) every 60 seconds. The server only polls when at least one subscription is active.

**Prompts** — 18 pre-built workflows the AI can invoke for multi-step tasks. See [prompts](internal/server/prompts) for the full list.

**Completions** — Prompt arguments and resource template URIs support auto-completion (e.g., genre lists, language codes, item/user/library ID lookups).

**Logging** — Tool calls emit structured MCP log notifications with timing data back to the client, in addition to stderr logging for local debugging.

## Important notes

**API key permissions** — The API key grants full access to whatever Jellyfin permissions are available. For shared or less trusted setups, pair it with `--read-only` or `--toolsets` to limit what the AI can do.

`--read-only` limits the tool surface, but it does not enable strict user selection. To prevent admin fallback, set an explicit `JELLYFIN_USER_ID` and `JELLYFIN_REQUIRE_USER_ID=true`.

With `--read-only`, or when `--toolsets` does not include `admin`, the `jellyfin://users` and `jellyfin://users/{userId}` resources are not exposed, and user ID completion does not call `/Users`.

**Network exposure** — In stdio mode, the server is only accessible to the local MCP client process. In HTTP mode, use `--http-token` whenever the server is reachable beyond localhost. The server refuses to start on a non-localhost address without a token.

**Jellyfin version** — Tested against Jellyfin 10.8 through 10.11. Older versions may be missing some API endpoints (e.g., playback reporting, activity log queries).

**Single binary, no runtime dependencies** — jellyfin-mcp is a statically compiled Go binary. No Node.js, Python, Java, or container runtime is required. The npm package is just a delivery wrapper around the same binary.

## License

[MIT](LICENSE)

## AI Disclosure

This project was made with the help of AI tools, but with a lot of manual effort towards SDK compliance, usability, and minimal slop. 
