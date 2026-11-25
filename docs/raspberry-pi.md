# Raspberry Pi

Popcorn targets low-resource ARM64 hardware (Raspberry Pi 4/5, NAS boxes).

## Constraints honored

- Single small binary, embedded frontend — no Node runtime on the device.
- Pure-Go SQLite driver — no CGO, straightforward `GOARCH=arm64` builds.
- Bounded background processing with concurrency limits (later phases).
- SQLite WAL read scaling suits single-process metadata access.

## Recommended OS

Raspberry Pi OS Lite or any 64-bit Linux (arm64). Avoid running the server as
root; use the `popcorn` service user.

## Build for arm64 (from any machine)

```sh
GOOS=linux GOARCH=arm64 go build -o bin/popcorn ./cmd/popcorn
```

Or use the cross-build convenience:

```sh
make docker-buildx
```

## Systemd unit

```ini
[Unit]
Description=Popcorn
After=network.target

[Service]
User=popcorn
Group=popcorn
ExecStart=/usr/local/bin/popcorn
Environment=POPCORN_HTTP_ADDR=:8715
Environment=POPCORN_DATA_DIR=/var/lib/popcorn
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Keep the data directory on the same disk layout you will register as a library
so metadata is near the originals (see [storage.md](storage.md)).

## Storage

Use an SSD or USB3-attached disk for libraries; avoid spinning disks as the
primary store for metadata databases if scans are heavy. Indexing runs
incrementally and respects low priority (nice value, I/O limits) in later
phases.