# File Naming Conventions

Name and arrange media files so that Jellyfin matches each movie, episode, and album to the right metadata.

Jellyfin reads the folder and file names to decide what an item is before it asks a metadata provider. A year and a provider ID in the name are optional, but they make the match far more reliable. Never use the reserved characters `< > : " / \ | ? *` in a name.

## Movies

Give each movie its own folder, named with the title and year:

~~~
Movies/
  Neon Cascade (1997)/
    Neon Cascade (1997).mkv
  Lucid Horizon (2011)/
    Lucid Horizon (2011).mp4
~~~

To force a match, add the provider ID to the folder name: `Neon Cascade (1997) [tmdbid-12345]` or `[imdbid-tt1234567]`. To keep several versions of one movie, name each file with the title and a label after a hyphen, such as `Neon Cascade (1997) - 2160p.mkv` and `Neon Cascade (1997) - 1080p.mkv`.

### Extras

Place extras in a subfolder of the movie folder named `behind the scenes`, `deleted scenes`, `interviews`, `scenes`, `samples`, `shorts`, `featurettes`, `clips`, `other`, `extras`, `trailers`, `theme-music`, or `backdrops`:

~~~
Movies/
  Neon Cascade (1997)/
    Neon Cascade (1997).mkv
    featurettes/
      Making Of.mkv
    trailers/
      Teaser.mkv
~~~

## TV Shows

Name season folders `Season NN`. Folders named `S01` or `SE01` don't parse.

~~~
Shows/
  Harbor Lights (2019)/
    Season 01/
      Harbor Lights S01E01.mkv
      Harbor Lights S01E02.mkv
    Season 02/
      Harbor Lights S02E01.mkv
~~~

A file that holds two episodes is named `Harbor Lights S01E01-E02.mkv`, and it appears as one entry. Split such files with a tool like MKVToolNix, so each episode gets its own entry and watched state. Place specials in `Season 00`, such as `Harbor Lights S00E01.mkv`, and give a special that has no provider data a descriptive name. Jellyfin 12 keeps several versions of one episode in the same way as a movie.

## Music

Jellyfin identifies music from the tags embedded in each file, so the file names matter less than the tags. Keep one album per folder:

~~~
Music/
  Mira Okonkwo/
    Tidewater (2015)/
      01 - Salt Road.flac
      02 - Lantern.flac
~~~

Disc numbers come from the tags. Lyrics in a `.lrc` file with the same name as the track appear during playback.

## Books

A Books library holds audiobooks, books, and comics in folders by type:

~~~
Books/
  Audiobooks/
  Books/
  Comics/
~~~

Jellyfin 12 reads OPF, ComicInfo, and ComicBookInfo metadata itself and generates covers.

## Anime

For anime, install the third-party Shokofin plugin and use its AniDB numbering. Without it, name files with the standard TV convention above.

## Common Mistakes

- **Missing year**: `Neon Cascade.mkv` instead of `Neon Cascade (1997).mkv` matches the wrong title more often.
- **Reserved characters**: A colon in a title, such as `Neon Cascade: Rising`, fails to parse. Write `Neon Cascade - Rising`.
- **Mixed content**: A library of the mixed movies and shows type gives unreliable metadata, so keep movies and shows in separate libraries.
- **No folder per movie**: Without its own folder, a movie has nowhere to hold extras and images.

## Sources

- https://jellyfin.org/docs/general/server/media/movies/
- https://jellyfin.org/docs/general/server/media/shows/
- https://jellyfin.org/docs/general/server/media/music/
- https://jellyfin.org/docs/general/server/media/books/
- https://jellyfin.org/docs/general/server/libraries/
