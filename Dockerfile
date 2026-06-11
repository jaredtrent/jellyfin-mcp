# syntax=docker/dockerfile:1

# ---------- build stage ----------
FROM golang:1.26.4-alpine AS build

# Never silently fetch a different toolchain than the image ships (reproducibility).
ENV GOTOOLCHAIN=local
WORKDIR /src

# Download dependencies first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary. VERSION is injected into the server's version string.
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/jaredtrent/jellyfin-mcp/internal/server.version=${VERSION}" \
      -o /out/jellyfin-mcp .

# ---------- runtime stage ----------
FROM alpine:3.24

# ca-certificates: required for HTTPS connections to your Jellyfin server.
RUN apk add --no-cache ca-certificates \
 && addgroup -S jellyfin \
 && adduser -S -G jellyfin jellyfin

COPY --from=build /out/jellyfin-mcp /usr/local/bin/jellyfin-mcp

# OCI metadata (CI augments these via docker/metadata-action labels).
ARG VERSION=docker
ARG REVISION=unknown
LABEL org.opencontainers.image.title="jellyfin-mcp" \
      org.opencontainers.image.description="Model Context Protocol server for Jellyfin media servers" \
      org.opencontainers.image.source="https://github.com/jaredtrent/jellyfin-mcp" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

USER jellyfin

# Streamable HTTP transport listens here (see CMD).
EXPOSE 8080

# Liveness probe: /health needs no auth and only reports that the HTTP listener is
# up (it does not check Jellyfin connectivity). Valid only in HTTP mode — for stdio
# runs, start the container with `--no-healthcheck`.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health >/dev/null 2>&1 || exit 1

# Defaults to Streamable HTTP on all interfaces. Flags use double dashes. A bearer
# token is REQUIRED for non-localhost binds, so append `--http-token <secret>`:
#
#   docker run -p 8080:8080 \
#     -e JELLYFIN_URL=https://jellyfin_host:8920 \
#     -e JELLYFIN_API_KEY=your_api_key \
#     ghcr.io/jaredtrent/jellyfin-mcp --http --addr 0.0.0.0:8080 --http-token secret
#
# For stdio transport instead, override the entrypoint and disable the healthcheck:
#
#   docker run -i --rm --no-healthcheck -e JELLYFIN_API_KEY=... \
#     --entrypoint /usr/local/bin/jellyfin-mcp ghcr.io/jaredtrent/jellyfin-mcp
#
# Jellyfin config comes from env vars: JELLYFIN_URL, JELLYFIN_API_KEY (required),
# JELLYFIN_USER_ID (optional).
ENTRYPOINT ["/usr/local/bin/jellyfin-mcp"]
CMD ["--http", "--addr", "0.0.0.0:8080"]
