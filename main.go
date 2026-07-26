package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jaredtrent/jellyfin-mcp/internal/server"
)

func main() {
	var cfg server.Config

	rootCmd := &cobra.Command{
		Use:   "jellyfin-mcp",
		Short: "Jellyfin MCP Server",
		Long:  "Model Context Protocol server for Jellyfin media server integration.",
		Run: func(_ *cobra.Command, _ []string) {
			server.Run(cfg)
		},
	}

	flags := rootCmd.Flags()
	flags.StringVar(&cfg.Toolsets, "toolsets", "", "comma-separated toolset groups to enable (default: all)")
	flags.BoolVar(&cfg.ReadOnly, "read-only", false, "only register read-only tools (no writes, deletes, or mutations)")
	flags.BoolVar(&cfg.DisableDestructive, "disable-destructive", false, "skip destructive tools (delete, restart, shutdown) while allowing other writes")
	flags.BoolVar(&cfg.HTTPMode, "http", false, "run as HTTP server instead of stdio")
	flags.StringVar(&cfg.HTTPAddr, "addr", "127.0.0.1:8080", "HTTP listen address (only used with --http)")
	flags.StringVar(&cfg.HTTPToken, "http-token", "", "bearer token for HTTP authentication (only used with --http)")

	rootCmd.SetUsageTemplate(usageTemplate())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func usageTemplate() string {
	var sb strings.Builder

	sb.WriteString(`Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}

Environment variables:
  JELLYFIN_URL              URL HTTP(S) du serveur (obligatoire)
  JELLYFIN_API_KEY          Clé API (obligatoire)
  JELLYFIN_USER_ID          ID utilisateur (optionnel, détecté automatiquement sinon)
  JELLYFIN_REQUIRE_USER_ID  Exige JELLYFIN_USER_ID et interdit le fallback administrateur

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

Toolsets:
`)

	tsMap := server.ToolsetNames()
	names := make([]string, 0, len(tsMap))
	for name := range tsMap {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&sb, "  %-12s %s\n", name, strings.Join(tsMap[name], ", "))
	}

	sb.WriteString(`
Examples:
  jellyfin-mcp                                          # stdio (default)
  jellyfin-mcp --http                                   # HTTP on 127.0.0.1:8080
  jellyfin-mcp --http --http-token secret               # HTTP with auth
  jellyfin-mcp --http --addr 0.0.0.0:9090               # HTTP on custom address
  jellyfin-mcp --http --read-only --toolsets discovery   # HTTP, limited tools
`)

	return sb.String()
}
