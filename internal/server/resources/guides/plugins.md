# Plugins

Install plugins from the official catalog or a third-party repository, and know which ones to reach for.

Plugins add metadata providers, subtitle sources, reports, and client features that the server doesn't ship. The official catalog, at `https://repo.jellyfin.org/files/plugin/manifest.json`, appears in Dashboard > Plugins > Catalog. A third-party plugin lives in its own repository, which you add by URL in Dashboard > Plugins > Repositories. `jellyfin_plugins action=list_packages` lists what the configured repositories offer, and `action=install package_name=<name> repository_url=<url>` installs from a specific one. Restart the server after installing a plugin.

## Upgrade to Jellyfin 12

Jellyfin 12 runs on a newer .NET, and a plugin built for 10.11 doesn't load on it. The project has updated the official plugins. Remove every third-party plugin before you upgrade the server, and add each one back once its author publishes a build for 12. A plugin's manifest names the version it targets in its `targetAbi` field.

## Plugins by Use Case

Plugins marked third-party need their own repository URL.

### Subtitles

- **Open Subtitles**: Downloads subtitles from OpenSubtitles.com with a free account.
- **SubBuzz** (third-party): Searches several subtitle sites at once. Repository: `https://raw.githubusercontent.com/josdion/subbuzz/master/repo/jellyfin_12.json`.

### Metadata

- **TheTVDB**: Series metadata from TVDB. Jellyfin's built-in providers are TMDb and OMDb, so a Shows library without this plugin gets its metadata from TMDb.
- **TMDb Box Sets**: Creates collections from TMDb's collection data.
- **AniDB** and **AniList**: Anime metadata providers.
- **Google Books**, **Comic Vine**, and **OpenLibrary**: Book and comic metadata providers. Jellyfin 12 reads OPF and ComicInfo metadata itself and replaces the Bookshelf plugin, which is deprecated and has no build for 12.

### Reports and Notifications

- **Playback Reporting**: Records playback and builds reports from it.
- **Webhook**: Sends an HTTP request when something happens, such as playback starting, to services like Discord or Gotify.

### Media Management

- **Shokofin** (third-party): Manages an anime library through Shoko Server. Check its release notes for the build that targets your Jellyfin version.
- **Chapter Segments Provider**: Marks intro and credits segments from chapter names, so clients can offer a skip button.
- **Intro Skipper** (third-party): Detects intros and credits by analyzing the audio. Repository: `https://intro-skipper.org/manifest.json`. It needs Jellyfin 12.

## Plugin Settings

Each plugin's settings appear in Dashboard > Plugins > My Plugins, and `jellyfin_plugins action=get_config plugin_id=<id>` reads them. The catalog shows an update when a newer version exists.

## Sources

- https://jellyfin.org/docs/general/server/plugins/
- https://jellyfin.org/docs/general/server/metadata/
- https://jellyfin.org/docs/general/server/metadata/media-segments/
- https://jellyfin.org/posts/jellyfin-release-12.0/
