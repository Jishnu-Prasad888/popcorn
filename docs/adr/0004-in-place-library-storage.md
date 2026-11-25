# 0004 — Libraries are indexed in place, metadata beside the media

- Status: accepted
- Date: 2026-09-22

## Context

Popcorn's defining requirement is to manage existing media without copying it into
a proprietary storage directory. Users point Popcorn at directories that already
hold years of photos, videos, and files. Originals must never be moved, copied,
recompressed, or rewritten silently. Libraries may be portable disks mounted at
different paths at different times.

## Decision

A library is any existing directory the user registers. Popcorn indexes it in
place. All Popcorn-generated data for a library lives in a single directory
inside the library, named `.popcorn/`:

```text
<library>/
├── originals...
└── .popcorn/
    ├── library.db        (library-level SQLite database)
    ├── thumbnails/
    ├── index/
    ├── embeddings/
    └── ...
```

Original media blobs are never stored in SQLite. `.popcorn/` is excluded from the
index. Library identity is not the mount path alone; the library is detected via
a persistent identifier inside `.popcorn/` (falling back to volume identity where
possible), so reconnecting a disk at a different mount point is recognized.

## Consequences

- Users keep exactly the directory structure they already have.
- The library is self-describing and portable: move the directory, reconnect the
  disk, and Popcorn can adopt it and resume.
- Generated content is unambiguously separated from user content.
- Backing up a library with its `.popcorn/` backs up all Popcorn knowledge of it.