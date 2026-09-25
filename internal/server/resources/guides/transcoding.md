# Hardware Transcoding Setup

Choose the hardware acceleration method that matches the server's GPU, give the container access to it, and turn on the encoding settings that keep transcodes fast.

Jellyfin transcodes on the CPU until you choose a hardware method, and a CPU transcode of a 4K stream can stall playback for everyone on the server. Hardware transcoding needs the Jellyfin build of FFmpeg. Jellyfin 12 uses FFmpeg 8, which the official packages and the container image include as `jellyfin-ffmpeg8`.

## Choose a Hardware Acceleration Method

| Method | Platform | Hardware |
|--------|----------|----------|
| Intel QSV | Linux and Windows | Intel GPUs from Broadwell (5th gen Core) on Linux; any GPU with QSV on Windows |
| VA-API | Linux | Nearly all Intel GPUs and all AMD GPUs; the method to choose for AMD on Linux, with full acceleration from Polaris (RX 400 and RX 500) |
| NVENC and NVDEC | Linux and Windows | NVIDIA GPUs from Maxwell; some low-end and mobile models, such as the GT 1030 and MX450, have no encoder |
| AMF | Windows; encoding only on Linux | Any AMD GPU that supports AMF; the RX 6400 and RX 6500 have no encoder |
| Apple VideoToolbox | macOS | Apple GPUs |
| Rockchip RKMPP | Linux | Rockchip SoCs |

Raspberry Pi V4L2 is deprecated. Set the method in Dashboard > Playback > Transcoding, or with `jellyfin_server action=update_config_section key=encoding` and `HardwareAccelerationType` set to `qsv`, `vaapi`, `nvenc`, `amf`, `videotoolbox`, or `rkmpp`.

## Intel

Each generation adds codecs, and every later generation keeps them, so a newer chip than the one named has everything on that line:

- Gen 9 Skylake (6th gen Core) and newer: H.264 and 8-bit HEVC.
- Gen 9.5 Kaby Lake (7th gen Core), Apollo Lake, Gemini Lake, and newer: 10-bit HEVC decode and encode, and tone mapping.
- Gen 12 Tiger Lake (11th gen Core) and newer: AV1 decode.
- Arc A-series, Meteor Lake (Core Ultra), and newer: AV1 encode.

To see whether a stream transcodes at all, and why, open the active sessions on the dashboard, which name the reason, or ask `jellyfin_item_extras action=playback_info` about the item.

Arc A-series GPUs need Linux kernel 6.2 or newer. Arc B-series GPUs need kernel 6.12 or newer and Resizable BAR turned on in the firmware. On Linux, add the `jellyfin` user to the `render` group. QSV VPP tone mapping, which uses less power than the OpenCL path, is available only on Linux.

For Docker, pass the render device and the host's numeric GID for the `render` group:

~~~yaml
devices:
  - /dev/dri/renderD128:/dev/dri/renderD128
group_add:
  - "122"   # Run `getent group render` on the host for the GID
~~~

## NVIDIA

Install driver 522.25 or newer on Windows, or 520.56.06 or newer on Linux. Maxwell 2nd gen (GM206, the GTX 950 and GTX 960) and newer decode 8-bit and 10-bit HEVC. Pascal and newer encode 10-bit HEVC. Ampere and newer decode AV1, and Ada and newer encode it. Tone mapping works on any NVIDIA GPU that decodes 10-bit HEVC.

The driver limits concurrent encoding sessions: 3 sessions before driver 530, 5 for 530 through 546, 8 for 550 through 58x, and 12 for 590 and newer.

For Docker, install the NVIDIA Container Toolkit on the host. Docker Desktop is not supported.

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

## AMD

Use VA-API on Linux and AMF on Windows. Polaris (RX 400 and RX 500) and newer encode HEVC and decode 10-bit HEVC. AMD GPUs have no concurrent session limit. On Linux, add the `jellyfin` user to the `render` and `video` groups, and pass the render device to a container as in the Intel example.

## Encoding Settings

- **Hardware decoding**: Turn on each codec the GPU supports. Anything left off decodes on the CPU.
- **Tone mapping**: Turn on to convert HDR to SDR for clients that can't display HDR. It needs 10-bit HEVC decoding on the GPU.
- **Throttling**: Turn on `EnableThrottling` to slow a transcode once the client's buffer is full. `ThrottleDelaySeconds` already defaults to 180, so setting it on its own changes nothing. Jellyfin 12.1 fixes a race that stopped throttling from starting.
- **Segment deletion**: Turn on `EnableSegmentDeletion` to remove transcoded segments the client has already played, which keeps the transcode folder small.

## Common Problems

- The method doesn't match the GPU, so every transcode falls back to the CPU.
- The host lacks GPU drivers or firmware.
- The container has no GPU device, or its user isn't in the `render` group.
- Docker runs on a Windows or macOS host, where hardware transcoding is unsupported.
- The server runs in Docker Desktop or an LXC container, where GPU access can drop.

## Sources

- https://jellyfin.org/docs/general/post-install/transcoding/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/intel/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/nvidia/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/amd/
- https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/known-issues/
