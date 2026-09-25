package resources

import (
	"context"
	"embed"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// guideFiles holds the reference guides as Markdown, one file per guide, so
// the prose is edited as prose rather than as Go string literals.
//
//go:embed guides/*.md
var guideFiles embed.FS

// guides lists every reference guide in the order clients see it. Each File
// names the Markdown file under guides/ that holds the guide's text.
var guides = []struct {
	URI, Name, Title, Description, File string
}{
	{
		URI:         "jellyfin://guides/transcoding",
		Name:        "Transcoding Setup Guide",
		Title:       "Transcoding",
		Description: "Hardware transcoding setup: which acceleration method fits which GPU, Docker GPU access, and the encoding settings that matter",
		File:        "transcoding.md",
	},
	{
		URI:         "jellyfin://guides/file-naming",
		Name:        "File Naming Conventions",
		Title:       "File Naming",
		Description: "Movie, show, music, and book file naming that Jellyfin matches to the right metadata",
		File:        "file-naming.md",
	},
	{
		URI:         "jellyfin://guides/remote-access",
		Name:        "Remote Access Setup",
		Title:       "Remote Access",
		Description: "Reverse proxy, VPN, and port forwarding options for reaching Jellyfin from outside the home network, and the Known Proxies setting",
		File:        "remote-access.md",
	},
	{
		URI:         "jellyfin://guides/troubleshooting",
		Name:        "Troubleshooting Guide",
		Title:       "Troubleshooting",
		Description: "Fixes for failed scans, wrong metadata, playback failures, locked accounts, and database errors, and how to read the server log",
		File:        "troubleshooting.md",
	},
	{
		URI:         "jellyfin://guides/library-setup",
		Name:        "Library Setup Guide",
		Title:       "Library Setup",
		Description: "Library organization by content type, storage placement, network shares, and Docker volumes",
		File:        "library-setup.md",
	},
	{
		URI:         "jellyfin://guides/docker",
		Name:        "Docker Deployment Guide",
		Title:       "Docker",
		Description: "Docker compose file, volumes, GPU access, permissions, networking, and the upgrade to Jellyfin 12",
		File:        "docker.md",
	},
	{
		URI:         "jellyfin://guides/users-and-access",
		Name:        "Users and Access Control Guide",
		Title:       "Users & Access",
		Description: "User accounts, per-library access, parental controls, failed-login lockouts, and invitation tools",
		File:        "users-and-access.md",
	},
	{
		URI:         "jellyfin://guides/plugins",
		Name:        "Plugins Guide",
		Title:       "Plugins",
		Description: "Plugin repositories, the Jellyfin 12 plugin upgrade, and which plugin to use for subtitles, metadata, reports, and intro skipping",
		File:        "plugins.md",
	},
	{
		URI:         "jellyfin://guides/migration",
		Name:        "Migration Guide",
		Title:       "Migration",
		Description: "Migrating from Plex or Emby to Jellyfin: library setup, metadata, watch history, and plugin equivalents",
		File:        "migration.md",
	},
	{
		URI:         "jellyfin://guides/performance",
		Name:        "Performance Tuning Guide",
		Title:       "Performance",
		Description: "Transcoding settings, needless transcodes, database placement, cache tasks, and storage for a fast server",
		File:        "performance.md",
	},
	{
		URI:         "jellyfin://guides/syncplay",
		Name:        "SyncPlay Guide",
		Title:       "SyncPlay",
		Description: "SyncPlay watch-together groups: supported clients, user access, network requirements, and troubleshooting",
		File:        "syncplay.md",
	},
}

func registerGuides(server *mcp.Server) {
	for _, g := range guides {
		content, err := guideFiles.ReadFile("guides/" + g.File)
		if err != nil {
			panic(fmt.Sprintf("guide %s: %v", g.URI, err))
		}
		text := string(content)
		uri := g.URI
		server.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        g.Name,
			Title:       g.Title,
			Description: g.Description,
			MIMEType:    "text/markdown",
			Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.2},
		}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      uri,
					MIMEType: "text/markdown",
					Text:     text,
				}},
			}, nil
		})
	}
}
