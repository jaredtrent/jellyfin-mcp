# Docker Deployment

Run the official Jellyfin image with the right user, volumes, ports, and GPU access.

The official image is `jellyfin/jellyfin` on Docker Hub, with a mirror at `ghcr.io/jellyfin/jellyfin`. Run it on a Linux host. Docker on a Windows or macOS host is unsupported: hardware transcoding fails there, and library scans on macOS misbehave.

## Compose File

~~~yaml
services:
  jellyfin:
    image: jellyfin/jellyfin
    container_name: jellyfin
    user: 1000:1000
    ports:
      - 8096:8096/tcp
      - 7359:7359/udp
    volumes:
      - /path/to/config:/config
      - /path/to/cache:/cache
      - type: bind
        source: /path/to/media
        target: /media
        read_only: true
    restart: unless-stopped
    environment:
      - JELLYFIN_PublishedServerUrl=http://jellyfin.example.com
~~~

Run `id` on the host and put its UID and GID in `user`, then make sure that user can read the media. Port 8096 serves HTTP, and 7359/udp answers client discovery on the local network. `JELLYFIN_PublishedServerUrl` is the address discovery hands to clients.

## GPU Access

Hardware transcoding needs the GPU device inside the container, and the method that matches it; jellyfin://guides/transcoding covers both.

### Intel and AMD

Pass the render device and the numeric GID of the host's `render` group. A group name only resolves if the group exists inside the container, so use the number.

~~~yaml
devices:
  - /dev/dri/renderD128:/dev/dri/renderD128
group_add:
  - "122"   # Run `getent group render` on the host for the GID
~~~

### NVIDIA

Install the NVIDIA Container Toolkit on the host. Docker Desktop is not supported.

~~~yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          count: all
          capabilities: [gpu]
environment:
  - NVIDIA_DRIVER_CAPABILITIES=all
  - NVIDIA_VISIBLE_DEVICES=all
~~~

## Volumes

| Container path | Holds |
|----------------|-------|
| `/config` | Configuration, the database, metadata images, and backups |
| `/cache` | Transcodes and other cache |
| `/media` | Media files; mount read-only unless you want Jellyfin to delete files or write beside them |

Keep `/config` on local storage. The database inside it slows down on a network share.

## Networking

Bridge networking with a `ports` mapping works for every client and for a reverse proxy, which forwards to the mapped port. The DLNA plugin needs `network_mode: host`, because DLNA discovery uses multicast that a bridge doesn't pass. Host mode and a `ports` mapping don't combine.

## Upgrade to Jellyfin 12

Back up `/config` before pulling the 12 image. Jellyfin 12 rewrites the database on first start, and the backup is the only way back to 10.11. Remove third-party plugins first; ones built for 10.11 don't load on 12. To run the migration on its own and exit, start the container once with the `--mode MigrateSystem` argument.

## Sources

- https://jellyfin.org/docs/general/installation/container/
- https://jellyfin.org/docs/general/post-install/networking/
- https://jellyfin.org/docs/general/post-install/networking/dlna/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/
- https://jellyfin.org/posts/jellyfin-release-12.0/
