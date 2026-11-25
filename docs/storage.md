# Storage

## Principles

1. **The filesystem is the source of truth for media bytes.**
2. Popcorn **never silently modifies originals.** It will not move, copy, resize,
   recompress, rotate, or rewrite metadata unless the user explicitly requests
   an operation with that intent.
3. **All application-generated data is derived** and kept out of the way of the
   user's files.

## Three kinds of data

| Kind          | Where it lives                          | Owner                   |
| ------------- | --------------------------------------- | ----------------------- |
| Original media| user's directories (anywhere)           | users and their disks   |
| Library metadata | `<library>/.popcorn/`                  | Popcorn                   |
| Server data   | `POPCORN_DATA_DIR`                        | Popcorn                   |

### Original media

Popcorn indexes existing directories in place. There is no proprietary storage
volume and no import step that copies files.

### Library metadata directory

Each library carries its own metadata inside the user-visible library, in a
single dot-directory:

```text
/mnt/photos/
├── 2022/
├── IMG001.jpg
└── .popcorn/
    ├── library.db              library-level SQLite database
    ├── thumbnails/             derived previews
    ├── previews/               screen-sized photos for the viewer
    ├── index/                  indexer state and change journals
    ├── embeddings/             optional ML embeddings (future)
    ├── faces/                  optional face data (future)
    ├── jobs/                   persisted background job state
    ├── metadata/               extracted metadata cache
    └── memory-media/           optional edited copies of memory photos
```

The `.popcorn/` name is the project's chosen internal metadata directory. It is
documented here and used consistently across the codebase and documentation.

Portability rule: a library is self-describing. A fresh Popcorn installation can
detect an existing library, adopt it, and reuse its metadata instead of
reprocessing everything. This is expanded in [libraries.md](libraries.md).

ML similarity signatures are derived data and live as a table inside
`library.db` (not as files), so they follow the library and are captured by any
backup that includes `.popcorn/`. They can be discarded and regenerated with a
similarity pass. See [ml.md](ml.md).

### Server data

Data that belongs to the server instance, not to any library, lives in
`POPCORN_DATA_DIR`:

```text
popcorn-data/
└── popcorn.db            server-level SQLite database
```

Backups are written wherever `POPCORN_BACKUP_DIR` points (see docs/backups.md).
Backup archives are meant to restore a Popcorn installation, not to be pulled into
the indexer as media; never place `POPCORN_BACKUP_DIR` inside a library root.

## Memory media

When a user turns on *Settings → Memories → Also save edited copies in the
library*, edited memory photos are rendered to:

```text
<library>/.popcorn/memory-media/
├── .popcorn-memory-media                 marker: claims the directory for Popcorn
└── <memory-id>/
    └── <memory-image-id>-<signature>.jpg
```

- **Reserved namespace.** The copies live inside the library (so they travel
  and back up with it) but inside `.popcorn/`, which the indexer never scans,
  so they are never indexed as new media and never appear as a user folder.
- **Collision safety.** Popcorn creates `memory-media/` and writes the marker
  file. If that path already exists as a file, a symlink, or a non-empty
  directory without the marker, Popcorn treats it as someone else's: it writes
  nothing there, moves nothing, overwrites nothing, and the editor reports
  that edited copies could not be created. An empty pre-existing directory is
  claimed.
- **Stable identity.** Names come from IDs, never from user-visible file
  names: `<memory-image-id>` is the memory image that owns the copy and
  `<signature>` a hash of its edits and source, so copies never collide with
  each other or with user files. `memory_derived_media` records the original
  file id, memory, memory image, path, edit signature, size and timestamps;
  the caption and edits stay on the memory image.
- **Lifecycle.** A copy exists only for an image with edits, and only while
  the edits match its signature. Unedited images always reference the
  original. Copies no memory image references are deleted (row and file);
  copies of soft-deleted memories are kept. Everything here can be deleted
  by hand — Popcorn regenerates what it needs on the next save.
- **Originals are read-only.** Rendering opens the original read-only; its
  bytes, name, location, timestamps and metadata are never changed. This is
  covered by an automated SHA-256 integrity test.
- Encrypted with the at-rest key when encryption is enabled, like
  thumbnails.

## What is never done

- Original media blobs are never stored inside SQLite.
- Thumbnails and previews are never written next to originals outside `.popcorn/`.
- Derived data is never stored so that it would be picked up by the indexer as
  new media.
- `.popcorn/` is excluded from indexing.

## Duplicate/privacy note

Because derived data is kept inside `<library>/.popcorn/`, backing up a library
with its metadata directory captures everything Popcorn knows about it. A library
whose `.popcorn/` directory is removed is simply a folder of files again — nothing
about the originals is lost, only Popcorn's knowledge of them.