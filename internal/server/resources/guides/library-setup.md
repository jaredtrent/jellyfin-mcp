# Library Organization

Give each content type its own library, keep the database on local flash storage, and mount media so that Jellyfin finds it on every start.

## Separate Libraries by Content Type

Each library has one type, and the type chooses the naming parser and the metadata providers Jellyfin uses for it. The mixed movies and shows type exists, but the project discourages it because its metadata results are unreliable. A folder per type keeps every library clean:

~~~
/media/
  movies/          -> Library: Movies (type: movies)
  shows/           -> Library: Shows (type: tvshows)
  music/           -> Library: Music (type: music)
  books/           -> Library: Books (type: books)
  music-videos/    -> Library: Music Videos (type: musicvideos)
~~~

Audiobooks belong in a Books library, in an `Audiobooks` folder beside `Books` and `Comics`.

## Several Libraries of One Type

Library access is granted per library, so a second library of the same type is how you give different people different content:

- **Kids and everyone else**: Put children's titles in their own library and grant a child's account only that library. Parental ratings then act as a second filter.
- **4K and 1080p**: Keep 4K files in their own library and leave it out of the accounts that stream over slow connections, so those clients never start a 4K transcode.
- **Anime and other shows**: Give anime its own library so you can choose AniDB or AniList as its metadata provider without affecting the rest.

Users who lack access to all libraries don't receive access to a library you add later, so grant it when you create the library.

## Storage

Keep the database and the rest of the server data on a local SSD. Network storage and ZFS on mechanical drives both make the database slow, and the database for a moderate library grows to somewhere between 10 and 100 GB. On ZFS, give the dataset that holds the database a record size of 4K or 8K, and give media datasets 1M. A record size change applies only to data written after it.

Mount network shares at the operating system level, so that Jellyfin sees an ordinary folder. Over NFSv3, .NET file locking can stall scans and playback. Use NFSv4, which locks correctly, or mount with `nolock`, or set the `DOTNET_SYSTEM_IO_DISABLEFILELOCKING` environment variable for the server. Mount shares before Jellyfin starts. A scan against an unmounted path finds an empty folder and can remove every item under it.

Jellyfin 12 follows a symbolic link when an item plays rather than when the library scans, so links inside the media folders work.

## Docker Volume Mounts

~~~yaml
volumes:
  - /path/to/config:/config
  - /path/to/cache:/cache
  - /media/movies:/movies:ro
  - /media/shows:/shows:ro
  - /media/music:/music:ro
~~~

Mount media read-only unless you want Jellyfin to delete files or write images and NFO files beside them. Keep the same container paths across recreations, because the library remembers paths, and a changed path looks like missing media.

## Sources

- https://jellyfin.org/docs/general/server/libraries/
- https://jellyfin.org/docs/general/server/media/mixed-movies-and-shows/
- https://jellyfin.org/docs/general/server/media/books/
- https://jellyfin.org/docs/general/administration/storage/
- https://jellyfin.org/docs/general/server/users/adding-managing-users/
