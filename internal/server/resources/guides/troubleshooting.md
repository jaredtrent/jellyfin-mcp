# Troubleshooting Common Issues

Diagnose library scans that find nothing, wrong metadata, playback that fails, locked accounts, and database errors, and read the server log to find the cause.

Start with the log. `jellyfin_system_info action=logs` lists the log files, and `action=log_file name=<filename>` reads one. Server logs are named `log_*.log`; the `FFmpeg.*.log` files hold one transcode each.

## Library Scan Finds Nothing

The library shows no items, the scan never finishes, or items are missing after a scan.

1. Check the folder and file names against jellyfin://guides/file-naming.
2. Confirm that the user Jellyfin runs as can read the media folders. In Docker, that is the `user` in the compose file.
3. If the media is on a network share, confirm that the share was mounted before Jellyfin started and that the library's path still points at it. jellyfin://guides/library-setup covers shares and the NFS locking problem.
4. Open Dashboard > Scheduled Tasks and look for a scan that failed, or use `jellyfin_tasks action=list`.
5. Read the newest server log with `jellyfin_system_info action=log_file severity=warn+error`.
6. Refresh one item with `jellyfin_library_manage action=refresh_item` to see whether the problem is one folder or the whole library.

## Metadata Is Wrong

The poster, description, or title belongs to a different movie or show.

1. Find the right match with `jellyfin_metadata action=search`, then apply it with `action=apply` and the match's provider ID.
2. To force a match at scan time, add the provider ID to the folder name, such as `Neon Cascade (1997) [tmdbid-12345]`, or place a Kodi-style `.nfo` file next to the media file.
3. Change the provider order for the library in Dashboard > Libraries when one provider keeps winning with worse data.
4. After correcting an item, lock the fields with `jellyfin_metadata action=update locked_fields=["Name","Overview"]` so the next refresh keeps them.

## Playback Fails

In the web client, open the browser console with F12 and read the errors, then clear the browser cache. In a mobile or TV app, close and reopen the app, check the server address, and lower the maximum streaming bitrate in the app.

When the failure is a transcode, `jellyfin_system_info action=log_file` shows the FFmpeg error. Check the GPU passthrough against jellyfin://guides/transcoding, confirm that the transcode folder has free space, and test the same item on a client that can play it directly.

## Locked Out of the Admin Account

Since Jellyfin 10.11, `jellyfin.db` holds the whole library, every user, and all watch history, so never delete it to reset a password. Use these paths instead, in order:

1. Ask another administrator to set a new password in Dashboard > Users > select the user > Password.
2. Select Forgot Password on the login page from the local network. Jellyfin writes a `passwordreset*.json` file with a PIN into its configuration directory, and the PIN completes the reset.
3. If repeated failed logins locked the account, stop Jellyfin, back up the database, and clear the lock:

   ~~~
   cp /path/to/data/jellyfin.db /path/to/data/jellyfin.db.bck
   sqlite3 /path/to/data/jellyfin.db
   UPDATE Users SET InvalidLoginAttemptCount = 0 WHERE Username = 'LockedUserName';
   UPDATE Permissions SET Value = 0 WHERE Kind = 2 AND UserId IN (SELECT Id FROM Users WHERE Username = 'LockedUserName');
   .exit
   ~~~

The database lives in the data directory: `/config/data` in the official container and `/var/lib/jellyfin/data` on Debian and Ubuntu, unless `database.xml` sets another path.

## Database Locked Errors

The log reports that the database is locked, usually during a scan.

1. Open Dashboard > Libraries > Display and lower the parallel scan task limit. If it is 0, set it to half the number of CPU cores.
2. If the errors continue, set `LockingBehavior` in `database.xml` to `Optimistic`, which retries failed writes. `Pessimistic` blocks reads during writes and slows the server. `NoLock` is the default.
3. If the server runs in an LXC container and the errors persist, move it to a virtual machine or Docker. LXC storage is a known cause.

## Corrupted Database

Stop Jellyfin and check the file:

~~~
sqlite3 /path/to/data/jellyfin.db "PRAGMA integrity_check;"
~~~

Anything other than `ok` means restore from a backup. Jellyfin 10.11 and newer keep backups from Dashboard > Backups in the `data/backups` folder, and you restore one from that tab or by starting the server with `--restore-archive <path to the backup zip>`. Leave the `jellyfin.db-wal` and `jellyfin.db-shm` files alone: they hold committed writes that the main file doesn't have yet, and deleting them loses those writes.

Back up before every major upgrade. Jellyfin has no downgrade path, and once a new version has migrated the database, the backup is the only way back.

## Read the Log

Each line carries a level marker. `[ERR]` needs attention, `[WRN]` may explain a symptom, and `[INF]` is normal operation. Common exceptions and what they mean:

- `IOException`: Jellyfin can't read or write a file. Check permissions and mounts.
- `HttpRequestException`: A metadata provider or plugin repository didn't answer.
- `UnauthorizedAccessException`: The Jellyfin user lacks permission on a path.

For more detail, create `logging.json` in the configuration directory with `{"Serilog": {"MinimumLevel": {"Default": "Debug"}}}`. Debug logging writes thousands of lines for one page load, so remove the file when you're done.

## Sources

- https://jellyfin.org/docs/general/administration/troubleshooting/
- https://jellyfin.org/docs/general/administration/backup-and-restore/
- https://jellyfin.org/docs/general/administration/configuration/
