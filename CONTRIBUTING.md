# Contributing

How to build, test, and change jellyfin-mcp.

## Prerequisites

- [Go 1.26 or later](https://go.dev/dl/). Builds use the Go release named by the `toolchain` line in `go.mod`, which the `go` command downloads when it is newer than the one installed.
- A running Jellyfin server, 10.11 or later, with an API key, for trying changes against real data. The tests need no server.
- [golangci-lint](https://golangci-lint.run/welcome/install/), if you want `make lint` and `make check` to run the linter that CI runs.

## Get Started

```sh
git clone https://github.com/jaredtrent/jellyfin-mcp.git
cd jellyfin-mcp

# Build for your platform
make build-local

# Run tests
make test

# Run vet, lint, and tests
make check
```

## Project Layout

```
main.go                          CLI entry point (cobra)
internal/jellyfin/               Jellyfin API client, types, output structs, helpers
internal/server/                 MCP server setup, middleware, subscriptions
internal/server/tools/           31 tools in 8 toolsets
internal/server/resources/       13 live data resources and the reference guides in guides/
internal/server/prompts/         19 guided workflows
npm/                             npm package for linux/x64 distribution
```

## Run Locally

```sh
cp .env.example .env
# Edit .env with your Jellyfin URL and API key

set -a; source .env; set +a
./build/jellyfin-mcp
```

`set -a` exports the variables that `.env` sets. Without it, the server doesn't see them and stops with "JELLYFIN_API_KEY environment variable must be set".

To serve HTTP instead of stdio:

```sh
set -a; source .env; set +a
./build/jellyfin-mcp --http
# The endpoint is http://127.0.0.1:8080/mcp
```

## Try Your Build in Claude

`go install .` builds the working tree into `~/go/bin/jellyfin-mcp`. Register that build under its own name, so it sits beside an installed release instead of replacing it:

```sh
go install .
claude mcp add -s user jellyfin-mcp-dev \
  -e JELLYFIN_URL=http://YOUR_SERVER:8096 \
  -e JELLYFIN_API_KEY=your_api_key \
  -- ~/go/bin/jellyfin-mcp
```

Claude Code starts the server when a session starts, so after each `go install .`, start a new session or reconnect the server through `/mcp`. A build from `go install` reports its version as `dev`; `make build-local` stamps the version from `npm/VERSION`.

## Test

```sh
make test          # Run all tests
make vet           # Static analysis
make lint          # golangci-lint, if installed
make check         # All of the above
```

The tests use a mock Jellyfin client in `internal/server/tools/mock_client_test.go`, so they run without a server.

## Submit a Change

1. Fork the repository and create a branch from `main`.
2. Make the change.
3. Run `make check` and fix what it reports.
4. Open a pull request that says what changed and why.

Keep a pull request to one feature or fix, so it can be reviewed and reverted on its own. For a large change, open an issue first to agree on the approach before the work.

## Add a Tool

1. Choose the file in `internal/server/tools/` for the toolset the tool belongs to.
2. Write the handler in the pattern of its neighbors: an input struct, the client call, and the result. A typed tool returns a struct from `internal/jellyfin/output_types.go`, which the SDK turns into the tool's output schema and structured content. A text tool returns `jf.TextResult`, and an error returns `jf.ErrResult`.
3. Register it in the file's `Register*Tools` function with the annotation preset that fits: `AnnotReadOnly`, `AnnotWriteOp`, `AnnotWriteCreate`, or `AnnotDestructive`.
4. Add the tool's name to `ToolsetMap` in `tools.go`.
5. Describe it in `internal/server/tools/README.md` and `npm/jellyfin-mcp/README.md`, and update the tool counts in `README.md`.
6. Add tests.

## Code Style

- Follow the standard Go conventions that `gofmt` and `go vet` check.
- Keep each tool handler self-contained, so a reader understands it without the others.
- Use the shared annotation presets in `tools.go` rather than inline annotations.
- State each fact of a result once, as a field of its output struct. Guidance for the assistant goes in a `notes` field.
- Write documentation and guide text in the reader's voice: active, addressed as *you*, with no Latin abbreviations and no callout blocks.
