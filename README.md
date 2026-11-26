# Popcorn

> Your own movie and TV server: point it at your folders and watch anywhere in the house.

Popcorn is a self-hosted movie and TV media server, a single Go binary with a
web player. It catalogs films and series where they already live, fetches
posters, plots, cast and ratings from TMDB (with IMDb ratings through OMDb),
and streams to any browser, converting with ffmpeg only when the browser needs it.

> **Popcorn catalogs your media *in place*. It never copies, moves, recompresses
> or rewrites your files.** Its catalog, artwork and edits live in a `.popcorn/`
> folder inside each library, so a drive can move to another machine with them.

## What it does

- **Movies and TV.** Movie libraries with filters (genre, year, rating,
  resolution, language, watched) and sorts; shows broken into seasons and
  episodes, with Next Up and Continue Watching.
- **Metadata.** TMDB matching with manual Identify, field locking so your edits
  survive a refresh, artwork upload or replacement, and episode thumbnails taken
  from the video.
- **Player.** Direct play when the browser can handle the file, remux or
  transcode when it can't. It has resume, speed, skip intro and credits, a
  next-episode countdown, quality choice, stream info, picture-in-picture and
  keyboard shortcuts (press `?` in the player).
- **Subtitles and audio.** Embedded and sidecar subtitles, uploads, forced and
  SDH tracks, delay, and styling. Image subtitles (PGS, VobSub) are burned in.
  Audio tracks follow each person's preferred languages.
- **Your stuff.** Watchlist, favourites, history, collections (yours plus
  automatic ones) and playlists with shuffle and repeat. Every account has its
  own progress and preferences.
- **Uploads.** Upload files or whole folders from the browser, with a
  per-file progress list that says why anything failed.
- **Server.** Multiple users and per-library permissions, library stats, live
  streams, duplicate and unplaced-file reports, and encrypted backups.

## Naming your files

```text
Movies/Heat (1995)/Heat (1995).mkv
Movies/Heat (1995)/Heat (1995).en.srt
TV/Dark/Season 01/Dark S01E01.mkv
```

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/Jishnu-Prasad888/popcorn/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/Jishnu-Prasad888/popcorn/main/scripts/install.ps1 | iex
```

The installer picks the build for your OS and CPU, checks it against the
release's `SHA256SUMS`, and installs it. It warns if ffmpeg is missing, then
asks a few yes/no questions about starting Popcorn automatically:

- **Linux:** a systemd service. Either a system service (starts at boot, runs
  as you; settings in `/etc/popcorn/popcorn.env`) or a user service (settings
  in `~/.config/popcorn/popcorn.env`, with the option to start at boot via
  lingering).
- **macOS:** a launchd agent that starts at login.
- **Windows:** a scheduled task. From an Administrator PowerShell it starts at
  boot; otherwise it starts when you sign in. Settings are in
  `popcorn-start.cmd` next to the binary.

Run the same command again to upgrade: it replaces the binary in place, keeps
the old one as `popcorn.previous`, and restarts the service. To skip the
questions, pass `-y` (`… | bash -s -- -y`) or, on Windows, set
`$env:POPCORN_YES = '1'` first. `--version vX.Y.Z` / `$env:POPCORN_VERSION`
installs a specific release.

Then open http://127.0.0.1:8045, create the administrator account, and add a
library.

## Quick start

### From source

```sh
make build          # builds web/, embeds it, compiles bin/popcorn
./bin/popcorn         # serves http://127.0.0.1:8045
```

The server database and state live in `$XDG_DATA_HOME/popcorn`
(`~/.local/share/popcorn`). Override with `POPCORN_DATA_DIR`.

### Requirements

- Go and Node (to build from source).
- **ffmpeg and ffprobe** (strongly recommended). ffprobe reads durations,
  codecs and tracks; ffmpeg remuxes and transcodes files the browser can't play
  directly (most `.mkv`, HEVC, DTS and TrueHD audio, image subtitles) and makes
  episode thumbnails. Without them, only browser-friendly MP4/WebM files play.
  Install it with `sudo apt install ffmpeg` (Debian/Ubuntu),
  `brew install ffmpeg` (macOS) or your platform's package manager, and make
  sure it is on `PATH` for the server. Popcorn warns in the app when it is missing.

### Docker

The image already includes ffmpeg.

```sh
docker build -t popcorn .
docker run -d \
  --name popcorn \
  -p 8045:8045 \
  -v popcorn-data:/data \
  popcorn
```

### Development

```sh
make web-install    # npm install once
make dev            # run the backend (serves web/dist)
make web-dev        # in another terminal: Vite dev server with /api proxy
```

See [docs/development.md](docs/development.md) for details.

### Metadata

Get a free TMDB API key at <https://www.themoviedb.org/settings/api> and, for
IMDb ratings, an OMDb key at <https://www.omdbapi.com/apikey.aspx>. Enter them
under **Server → Metadata providers**, or set `POPCORN_TMDB_API_KEY` and
`POPCORN_OMDB_API_KEY`.

## Configuration

All settings use the `POPCORN_` prefix.

| Variable               | Default                            | Description                             |
| ---------------------- | ---------------------------------- | --------------------------------------- |
| `POPCORN_HTTP_ADDR`      | `127.0.0.1:8045`                   | HTTP listen address                     |
| `POPCORN_DATA_DIR`       | `$XDG_DATA_HOME/popcorn`             | Server-level data directory             |
| `POPCORN_LOG_LEVEL`      | `info`                             | `debug`, `info`, `warn`, or `error`     |
| `POPCORN_WEB_DIST`       | *(embedded frontend)*              | Serve an on-disk build instead (dev)    |
| `POPCORN_COOKIE_SECURE`  | `false`                            | Force `Secure` on session cookies (TLS-terminating proxy) |
| `POPCORN_BACKUP_DIR`     | *(disabled)*                       | Starting local backup folder; the Backups page can set a folder, mounted drive, SFTP, FTP or SMB destination instead |
| `POPCORN_BACKUP_KEEP`    | `4`                                | Completed backups retained; older are pruned |
| `POPCORN_BACKUP_INTERVAL_MIN` | `0`                            | Starting backup interval in minutes (`0` = manual only); the Backups page also offers daily/weekly schedules and size triggers |
| `POPCORN_BACKUP_PASSPHRASE`| *(disabled)*                     | Encrypts backups (needed for restore/verify) |
| `POPCORN_ENCRYPTION_PASSPHRASE` | *(disabled)*            | Encrypts Popcorn's on-disk metadata (.popcorn identity + thumbnails) at rest |
| `POPCORN_MEDIA_WORKERS`  | *half the CPUs, 1–4*               | Files per library probed and matched at once |
| `POPCORN_TMDB_API_KEY`   | *(none)*                           | TMDB key; can also be set in the app |
| `POPCORN_OMDB_API_KEY`   | *(none)*                           | OMDb key for IMDb ratings (optional) |
| `POPCORN_METADATA_LANGUAGE` | `en-US`                         | Language for titles and plots |
| `POPCORN_METADATA_REGION` | `US`                              | Region for release dates and age ratings |
| `POPCORN_MAX_TRANSCODES` | `3`                                | Conversions that may run at once |
| `POPCORN_VIDEO_ENCODER`  | `libx264`                          | `libx264`, `h264_nvenc`, `h264_qsv` or `h264_vaapi` |
| `POPCORN_TRANSCODE_PRESET` | `veryfast`                       | x264 preset for software transcodes |
| `POPCORN_HW_DEVICE`      | *(none)*                           | Device for VAAPI, e.g. `/dev/dri/renderD128` |
release.