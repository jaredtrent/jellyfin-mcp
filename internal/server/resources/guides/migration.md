# Migrating to Jellyfin

Move from Plex or Emby by pointing Jellyfin at the same media folders, carrying over watch history with a migration tool, and picking the matching plugins.

Jellyfin reads media from the folders it's given, so the files stay where they are. What doesn't carry over by itself is the metadata, the watch history, and the accounts.

## From Plex

Point each Jellyfin library at the folder the Plex library uses, such as `/media/movies` and `/media/shows`, and let it scan. Jellyfin fetches metadata from its own providers: TMDb and OMDb are built in, and the TheTVDB plugin adds TVDB for shows. Jellyfin reads Kodi-style `.nfo` files, as exported by Kodi or a tool such as tinyMediaManager, and picks up images next to the media, such as `poster.jpg`, `folder.jpg`, `backdrop.jpg`, and `fanart.jpg`. Plex itself writes neither.

### Watch History

The official migration page links migrate-plex-to-jellyfin, at `https://github.com/wilmardo/migrate-plex-to-jellyfin`, which copies watched status from Plex into Jellyfin accounts. For a handful of items, `jellyfin_user_data action=mark_played` marks one item played, and `action=set_user_data` sets its play count and played state; both change one item per call.

### Plugin Equivalents

| Plex feature | Jellyfin equivalent |
|--------------|---------------------|
| Plex Pass hardware transcoding | Built in, at no cost |
| Collections | Box sets, managed with `jellyfin_collections` |
| Playlists | Playlists, managed with `jellyfin_playlists` |
| Skip Intro | Intro Skipper, a third-party plugin, or the Chapter Segments Provider plugin |
| Tautulli | Playback Reporting plugin |
| Sub-Zero | Open Subtitles plugin |

Intro Skipper installs from its own repository, so `jellyfin_plugins action=list_packages` doesn't list it until that repository is added. See jellyfin://guides/plugins.

## From Emby

Jellyfin forked from Emby in 2018, but the two have diverged since, and a direct database migration from Emby of any version isn't supported. Point Jellyfin at the same media folders and let it scan. Emby plugins don't load in Jellyfin, and Emby client apps don't connect to it, so each device needs a Jellyfin client. Transcoding, networking, and user settings are configured again from the dashboard.

Create each account with `jellyfin_users action=create` and set its library access with `action=update_policy`. The official migration page links Emby2Jelly, at `https://github.com/CobayeGunther/Emby2Jelly`, which copies watched status from Emby.

## After the Migration

1. Confirm that every library appears with `jellyfin_libraries`.
2. Compare item counts with what the old server had, using `jellyfin_analytics action=library_stats`.
3. Set up hardware transcoding; `jellyfin_server action=get_config_section key=encoding` shows the current settings, and jellyfin://guides/transcoding explains them.
4. Confirm that every account exists with `jellyfin_users action=list`.
5. Play something on each kind of client to test direct play and transcoding.
6. Install plugins from `jellyfin_plugins action=list_packages`, checking that each one targets your Jellyfin version.
7. Set up remote access; see jellyfin://guides/remote-access.

## Sources

- https://jellyfin.org/docs/general/administration/migrate/
- https://jellyfin.org/docs/general/server/metadata/
- https://jellyfin.org/docs/general/server/metadata/nfo/
- https://jellyfin.org/docs/general/server/media/movies/
