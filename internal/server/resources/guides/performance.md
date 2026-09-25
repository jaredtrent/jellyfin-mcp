# Performance Tuning

Find what slows the server down and fix it: transcodes that run on the CPU, a database on slow storage, a full cache, and clients that transcode when they could play directly.

Most slow Jellyfin servers are slow for one of two reasons. Either transcodes run on the CPU, or the database sits on a network share or a mechanical disk. Check those two before anything else.

## Transcoding

`jellyfin_server action=get_config_section key=encoding` shows the current settings. The ones that matter:

- **HardwareAccelerationType**: One of `qsv`, `vaapi`, `nvenc`, `amf`, `videotoolbox`, or `rkmpp`, matching the GPU. `none` means every transcode runs on the CPU. See jellyfin://guides/transcoding for which method fits which hardware.
- **EnableHardwareEncoding**: On by default. Turn it off only when the GPU decodes well but encodes badly.
- **HardwareDecodingCodecs**: The codecs the GPU decodes. Anything not listed decodes on the CPU.
- **EnableTonemapping**: Converts HDR to SDR for clients that can't display HDR. It needs 10-bit HEVC decoding on the GPU.
- **EnableThrottling**: Slows a transcode once the client has buffered enough, which frees the GPU for other streams. It is off by default. `ThrottleDelaySeconds` already defaults to 180, so changing it alone does nothing.
- **EnableSegmentDeletion**: Removes transcoded segments the client has already played, so a long transcode doesn't fill the cache.
- **TranscodingTempPath**: Where transcodes are written. It defaults to `transcodes` under the cache directory, which is `/cache` in the official container and `/var/cache/jellyfin` on Debian and Ubuntu. Put it on an SSD.

An NVIDIA driver caps concurrent encoding sessions: 3 before driver 530, 5 for 530 through 546, 8 for 550 through 58x, and 12 for 590 and newer. AMD GPUs have no cap.

## Clients That Transcode Needlessly

A transcode happens when the client can't play the file's container, video codec, audio codec, or subtitle format directly. `jellyfin_analytics action=codec_report` shows which codecs the library holds, and `jellyfin_item_extras action=playback_info` shows why one item transcodes for one client. HEVC and AV1 on older TVs and browsers, and image-based subtitles burned into the picture, are the usual causes. Choose clients that play the library's codecs, or set a maximum streaming bitrate in the client for connections that can't carry the file.

## Database

Jellyfin stores everything except media and images in `jellyfin.db`, in the data directory: `/config/data` in the official container and `/var/lib/jellyfin/data` on Debian and Ubuntu. On Jellyfin 12, `database.xml` can place it elsewhere. Keep it on a local SSD, never on a network share or on ZFS over mechanical disks. On ZFS, give its dataset a record size of 4K or 8K.

The database runs in WAL mode. Jellyfin 12 adds an Optimize Database scheduled task that checkpoints the WAL and runs `VACUUM`, so there is no need to run `sqlite3` by hand. Run it from `jellyfin_tasks` after deleting a large amount of media. For database locked errors during scans, see jellyfin://guides/troubleshooting.

## Cache and Metadata

- **Transcodes** live under the cache directory and are removed when a session ends and by the Clear Transcodes Folder task. The folder needs roughly as much space as the media being transcoded.
- **Other cache** is cleared by the Clear Cache Folder task. Both tasks are listed by `jellyfin_tasks action=list`.
- **Metadata images**, such as posters, backdrops, and chapter images, live in `/config/metadata`. Neither task removes them, because they aren't cache. They grow with the library.

## Storage and Network

Media can stay on mechanical disks, because streaming reads sequentially. Over NFSv3, .NET file locking stalls scans and playback; use NFSv4 or the workarounds in jellyfin://guides/library-setup. Set a remote bitrate limit in Dashboard > Playback > Streaming so that remote clients don't ask for more than the upload connection carries.

## Monitoring

- `jellyfin_system_info action=info` reports the server version. It doesn't report hardware, so ask which CPU or GPU the server uses.
- `jellyfin_sessions action=list` shows who is playing what on which client. It doesn't show whether a stream transcodes; the dashboard's active sessions do.
- `jellyfin_analytics action=codec_report` finds the codecs that force transcoding.
- `jellyfin_item_extras action=playback_info` checks whether one item plays directly.
- `jellyfin_system_info action=log_file` shows FFmpeg errors and warnings.

## Sources

- https://jellyfin.org/docs/general/post-install/transcoding/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/known-issues/
- https://jellyfin.org/docs/general/administration/storage/
- https://jellyfin.org/docs/general/administration/configuration/
- https://jellyfin.org/docs/general/server/tasks/
