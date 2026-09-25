# Resources

Thirteen resources that read live data from the Jellyfin server without a tool call, and eleven static reference guides.

## Fixed resources

| URI | Description |
|-----|-------------|
| `jellyfin://server/info` | Server name, version, ID, whether the startup wizard is complete, and the local address |
| `jellyfin://libraries` | All media libraries with IDs, collection types, and paths |
| `jellyfin://sessions` | All connected client sessions with playback status |
| `jellyfin://sessions/now-playing` | Sessions with active playback only |
| `jellyfin://users` | All user accounts with admin status and last activity |
| `jellyfin://resume` | Items with in-progress playback |
| `jellyfin://next-up` | Next episodes in series that are in progress |
| `jellyfin://favorites` | Items marked as favorite by the current user |
| `jellyfin://latest` | Recently added items across all libraries |
| `jellyfin://recently-played` | Recently watched or listened items (verified playback only) |

## Template resources

| URI | Description |
|-----|-------------|
| `jellyfin://items/{itemId}` | Detailed metadata for any media item |
| `jellyfin://users/{userId}` | User account details and policy settings |
| `jellyfin://libraries/{libraryId}/latest` | Recently added items in a specific library |

## Reference guides

Static Markdown guides, one file each under [guides](guides), that the AI reads to help with setup and troubleshooting.

| URI | Description |
|-----|-------------|
| `jellyfin://guides/transcoding` | Hardware transcoding: which method fits which GPU, Docker GPU access, encoding settings |
| `jellyfin://guides/file-naming` | Movie, show, music, and book file naming for metadata matching |
| `jellyfin://guides/remote-access` | Reverse proxy, VPN, port forwarding, and the Known Proxies setting |
| `jellyfin://guides/troubleshooting` | Failed scans, wrong metadata, playback failures, locked accounts, database errors, and reading the log |
| `jellyfin://guides/library-setup` | Library organization by content type, storage placement, network shares, Docker volumes |
| `jellyfin://guides/docker` | Compose file, volumes, GPU access, permissions, networking, and the upgrade to Jellyfin 12 |
| `jellyfin://guides/users-and-access` | Accounts, per-library access, parental controls, and failed-login lockouts |
| `jellyfin://guides/plugins` | Plugin repositories, the Jellyfin 12 plugin upgrade, and plugins by use case |
| `jellyfin://guides/migration` | Migrating from Plex or Emby: library setup, metadata, watch history, plugin equivalents |
| `jellyfin://guides/performance` | Transcoding settings, needless transcodes, database placement, cache tasks, storage |
| `jellyfin://guides/syncplay` | SyncPlay watch-together: supported clients, user access, network requirements, troubleshooting |
