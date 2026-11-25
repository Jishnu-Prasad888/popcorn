// Package librarydb manages per-library SQLite databases.
//
// Each Popcorn library keeps its own SQLite database at <root>/.popcorn/library.db.
// This database holds library-scoped data: indexed files, folders, jobs,
// thumbnails, embeddings, and any other data that travels with the library.
//
// The server-level database (internal/db) is used for global concerns:
// user accounts, sessions, registered libraries, global settings, and audit
// records.
//
// This package imports internal/media for the extension-to-type classifier,
// because a library's database is only ever opened from inside that library's
// own root, so a schema change has to be applied by the code that opens it
// rather than by a server-side migration. The dependency is safe: internal/media
// never imports this package — it is handed an open *sql.DB instead.
package librarydb

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Jishnu-Prasad888/popcorn/internal/media"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const (
	// dbFileName is the per-library database filename inside .popcorn/.
	dbFileName = "library.db"

	// SchemaVersion is the current version of the library-level database
	// schema. Bump this when adding new tables or changing existing ones.
	SchemaVersion = 20
)

// DB wraps a per-library SQLite connection pool. Use OpenDB or AcquireDB to
// construct one.
type DB struct {
	pool    *sql.DB
	release func() // set when pool is shared (AcquireDB)
}

// DB returns the underlying *sql.DB connection pool.
func (d *DB) DB() *sql.DB { return d.pool }

// Close closes the underlying connection pool, or for a shared pool hands it
// back.
func (d *DB) Close() error {
	if d.release != nil {
		d.release()
		return nil
	}
	return d.pool.Close()
}

// AcquireDB is Acquire wrapped as a DB whose Close releases the shared pool.
func AcquireDB(popcornDir string) (*DB, error) {
	pool, release, err := Acquire(popcornDir)
	if err != nil {
		return nil, err
	}
	return &DB{pool: pool, release: release}, nil
}

// OpenDB opens (creating if necessary) the per-library SQLite database at
// <popcornDir>/library.db and applies any pending schema migrations.
func OpenDB(popcornDir string) (*DB, error) {
	pool, err := Open(popcornDir)
	if err != nil {
		return nil, err
	}
	return &DB{pool: pool}, nil
}

// Open opens (creating if necessary) the per-library SQLite database at
// <popcornDir>/library.db and applies any pending schema migrations.
func Open(popcornDir string) (*sql.DB, error) {
	path := filepath.Join(popcornDir, dbFileName)

	// Every call opens its own *sql.DB pointed at the same file — callers
	// across internal/ml and elsewhere each hold their own short-lived
	// connection rather than sharing one. Two Opens of the same library
	// racing here (e.g. a background ML pass started just before the
	// library is reopened) would otherwise both open a connection to the
	// file at once: migrate()'s check-then-ALTER isn't atomic across
	// connections, so both could see a column missing and both try to add
	// it, and even once that part is serialized, a second connection's Ping
	// can land mid-ALTER on the first and come back SQLITE_BUSY. Holding
	// this per-path lock for the whole open — not just migrate() — keeps
	// only one connection ever touching a given library's file at a time.
	mu := migrationLock(path)
	mu.Lock()
	defer mu.Unlock()

	pool, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open library database: %w", err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)

	if err := pool.Ping(); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping library database: %w", err)
	}
	// The schema (DDL) only has to be brought forward once per process. The
	// stamp in user_version is re-read on every open, so a database file that
	// was swapped out underneath (a restore, a re-created .popcorn) is migrated
	// again rather than trusted on the strength of its path.
	if !migratedBefore(path) || !stamped(pool) {
		if _, err := pool.Exec(`PRAGMA journal_mode = WAL`); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
		if err := migrate(pool); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("migrate library database: %w", err)
		}
		if _, err := pool.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaStamp)); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("stamp library database: %w", err)
		}
		markMigrated(path)
	}
	// Rows an older build wrote may still need bringing forward. Both checks
	// are index lookups that find nothing on an up-to-date library.
	if err := backfill(pool); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("migrate library database: %w", err)
	}
	return pool, nil
}

// schemaStamp is written to PRAGMA user_version once migrate() has run. It
// only has to differ from 0 (a fresh or foreign file) and from any stamp an
// older build wrote, so it tracks SchemaVersion.
const schemaStamp = 0x0CA1_0000 + SchemaVersion

var (
	migratedMu    sync.Mutex
	migratedPaths = map[string]bool{}
)

// migratedBefore reports whether this process has already migrated the
// database at path.
func migratedBefore(path string) bool {
	migratedMu.Lock()
	defer migratedMu.Unlock()
	return migratedPaths[path]
}

func markMigrated(path string) {
	migratedMu.Lock()
	migratedPaths[path] = true
	migratedMu.Unlock()
}

// stamped reports whether the open database carries the current schema stamp.
func stamped(pool *sql.DB) bool {
	var v int
	return pool.QueryRow(`PRAGMA user_version`).Scan(&v) == nil && v == schemaStamp
}

var (
	migrationLocksMu sync.Mutex
	migrationLocks   = map[string]*sync.Mutex{}
)

// migrationLock returns the mutex guarding migrate() for the library database
// at path, creating it on first use. One mutex per path, kept for the life of
// the process, so concurrent Opens of the same library serialize their
// migration while Opens of different libraries do not block each other.
func migrationLock(path string) *sync.Mutex {
	migrationLocksMu.Lock()
	defer migrationLocksMu.Unlock()
	mu, ok := migrationLocks[path]
	if !ok {
		mu = &sync.Mutex{}
		migrationLocks[path] = mu
	}
	return mu
}

func dsn(path string) string {
	pragmas := []string{
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
		"journal_mode(WAL)",
	}
	u := "file:" + filepath.ToSlash(path) + "?"
	for i, p := range pragmas {
		if i > 0 {
			u += "&"
		}
		u += "_pragma=" + p
	}
	return u
}

// migrate applies the per-library schema. Each statement is idempotent via
// CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	if err := addIndexedFileMediaType(db); err != nil {
		return err
	}
	if err := addMediaMetadataColor(db); err != nil {
		return err
	}
	return migrateCatalog(db)
}

// backfill rewrites rows left in an older shape: files indexed before they
// were classified, and memories from before the block model.
func backfill(db *sql.DB) error {
	if err := classifyIndexedFiles(db); err != nil {
		return err
	}
	return nil
}

// addMediaMetadataColor adds media_metadata.color — a photo's average colour,
// which the grid paints while the thumbnail loads — to a library database
// created before it existed. Rows start without one; the background renditions
// job fills them in (see indexer).
func addMediaMetadataColor(db *sql.DB) error {
	has, err := columnExists(db, "media_metadata", "color")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE media_metadata ADD COLUMN color TEXT`)
	return err
}

// addIndexedFileMediaType backfills indexed_files.media_type on a library
// database created before that column existed.
//
// SQLite has no `ADD COLUMN IF NOT EXISTS`, so a library that was indexed
// before this change would otherwise keep a table without the column and every
// `?type=` listing would fail with "no such column". A library's database
// lives inside its own root, so this cannot be a server-side migration: the
// file is only opened when that library is next used.
func addIndexedFileMediaType(db *sql.DB) error {
	has, err := columnExists(db, "indexed_files", "media_type")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE indexed_files
			ADD COLUMN media_type TEXT NOT NULL DEFAULT 'other'
			CHECK (media_type IN ('photo','video','audio','document','other'))`); err != nil {
			return err
		}
	}
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS indexed_files_type_idx ON indexed_files (media_type)`); err != nil {
		return err
	}
	// Covers the dashboard's per-type counts and newest-first listings.
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS indexed_files_status_type_mod_idx
		 ON indexed_files (status, media_type, mod_time)`); err != nil {
		return err
	}
	// Covers "Recently added": newest-first by the date a file joined.
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS indexed_files_status_type_added_idx
		 ON indexed_files (status, media_type, first_seen_at, mod_time)`); err != nil {
		return err
	}
	return nil
}

// classifyIndexedFiles sets media_type on rows still classified as 'other'
// whose extension says otherwise.
func classifyIndexedFiles(db *sql.DB) error {
	// The extension is what the type is derived from, so the backfill is a
	// rewrite rather than a guess — it is also exactly what a re-index would
	// store, so the two paths cannot disagree.
	rows, err := db.Query(`SELECT id, rel_path FROM indexed_files WHERE media_type = 'other'`)
	if err != nil {
		return err
	}
	type pending struct {
		id, relPath, mediaType string
	}
	var updates []pending
	for rows.Next() {
		var id, relPath string
		if err := rows.Scan(&id, &relPath); err != nil {
			_ = rows.Close()
			return err
		}
		// Most rows that are still 'other' really are (sidecars, archives…);
		// rewriting them to the same value would be a write per row per open.
		if t := media.DetectMediaType(relPath); t != media.MediaTypeOther {
			updates = append(updates, pending{id: id, relPath: relPath, mediaType: string(t)})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`UPDATE indexed_files SET media_type = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, u := range updates {
		if _, err := stmt.Exec(u.mediaType, u.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// columnExists reports whether a table already has the named column.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notNull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// schema is the canonical per-library DDL. All tables use CREATE TABLE IF NOT
// EXISTS so the function is safe to call on an already-initialised database.
const schema = `
-- Tracking table for every file the indexer has seen under the library root.
-- Relative paths are relative to the library root (never absolute) so the
-- library remains portable when remounted at a different path.
CREATE TABLE IF NOT EXISTS indexed_files (
	id            TEXT PRIMARY KEY,
	rel_path      TEXT NOT NULL UNIQUE,
	size_bytes    INTEGER NOT NULL,
	mod_time      TEXT NOT NULL,
	content_hash  TEXT,
	media_type    TEXT NOT NULL DEFAULT 'other'
		CHECK (media_type IN ('photo','video','audio','document','other')),
	status        TEXT NOT NULL DEFAULT 'present'
		CHECK (status IN ('present', 'missing', 'deleted')),
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	indexed_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS indexed_files_rel_path_idx ON indexed_files (rel_path);
CREATE INDEX IF NOT EXISTS indexed_files_status_idx   ON indexed_files (status);
-- Folder views: present files under a path prefix, in name order.
CREATE INDEX IF NOT EXISTS indexed_files_status_path_idx ON indexed_files (status, rel_path);
CREATE INDEX IF NOT EXISTS indexed_files_hash_idx     ON indexed_files (content_hash)
	WHERE content_hash IS NOT NULL;
-- indexed_files_type_idx is deliberately not created here: on a database that
-- predates the media_type column it would fail before the column is added. It
-- is created by addIndexedFileMediaType, which runs for every database.

-- Background job queue for library-scoped operations.
-- Jobs are persisted so they survive process restarts; a job that was
-- 'running' at shutdown is reset to 'queued' on the next startup.
CREATE TABLE IF NOT EXISTS index_jobs (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT 'queued'
		CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
	priority     INTEGER NOT NULL DEFAULT 0,
	payload      TEXT NOT NULL DEFAULT '{}',
	result       TEXT,
	error_msg    TEXT,
	attempt      INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL DEFAULT 3,
	created_at   TEXT NOT NULL,
	started_at   TEXT,
	finished_at  TEXT,
	next_run_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS index_jobs_status_idx      ON index_jobs (status);
CREATE INDEX IF NOT EXISTS index_jobs_next_run_idx    ON index_jobs (next_run_at)
	WHERE status IN ('queued', 'failed');
-- The worker's claim order: the next queued job is the first row of this
-- index, rather than the result of sorting every queued job.
CREATE INDEX IF NOT EXISTS index_jobs_claim_idx ON index_jobs (status, priority DESC, created_at);
-- Status lookups (the oldest running/queued job) and "is a scan pending".
CREATE INDEX IF NOT EXISTS index_jobs_status_created_idx ON index_jobs (status, created_at);
CREATE INDEX IF NOT EXISTS index_jobs_kind_status_idx ON index_jobs (kind, status);

-- folders tracks directories under the library root that contain indexed files.
CREATE TABLE IF NOT EXISTS folders (
	id           TEXT PRIMARY KEY,
	rel_path     TEXT NOT NULL UNIQUE,
	parent_id    TEXT REFERENCES folders(id),
	name         TEXT NOT NULL,
	file_count   INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS folders_rel_path_idx ON folders (rel_path);
CREATE INDEX IF NOT EXISTS folders_parent_idx   ON folders (parent_id);

-- media_metadata stores media-specific attributes derived from indexed files.
-- Rows are regenerable; keyed on indexed_files.id for efficient joins.
CREATE TABLE IF NOT EXISTS media_metadata (
	file_id       TEXT PRIMARY KEY REFERENCES indexed_files(id) ON DELETE CASCADE,
	media_type    TEXT NOT NULL DEFAULT 'other'
		CHECK (media_type IN ('photo','video','audio','document','other')),
	mime_type     TEXT,
	width         INTEGER,
	height        INTEGER,
	duration_secs REAL,
	taken_at      TEXT,
	camera_make   TEXT,
	camera_model  TEXT,
	latitude      REAL,
	longitude     REAL,
	has_thumbnail INTEGER NOT NULL DEFAULT 0,
	updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS media_metadata_type_idx ON media_metadata (media_type);

-- trash records soft-deleted files for restore or permanent deletion.
CREATE TABLE IF NOT EXISTS trash (
	file_id         TEXT PRIMARY KEY REFERENCES indexed_files(id) ON DELETE CASCADE,
	original_path   TEXT NOT NULL,
	trash_path      TEXT NOT NULL,
	deleted_at      TEXT NOT NULL,
	deleted_by      TEXT
);

-- fts_files is a FTS5 virtual table for full-text search over file paths and
-- names. The file_id column is UNINDEXED (stored but not tokenized) so
-- searches only match against rel_path. The default unicode61 tokenizer splits
-- on '/' and '.' so searching "beach" matches "holiday/beach.jpg".
CREATE VIRTUAL TABLE IF NOT EXISTS fts_files USING fts5(
	file_id UNINDEXED,
	rel_path
);

-- Keep fts_files in sync with indexed_files.
CREATE TRIGGER IF NOT EXISTS fts_files_insert AFTER INSERT ON indexed_files BEGIN
	INSERT INTO fts_files(file_id, rel_path) VALUES (new.id, new.rel_path);
END;

CREATE TRIGGER IF NOT EXISTS fts_files_update AFTER UPDATE OF rel_path ON indexed_files BEGIN
	DELETE FROM fts_files WHERE file_id = old.id;
	INSERT INTO fts_files(file_id, rel_path) VALUES (new.id, new.rel_path);
END;

CREATE TRIGGER IF NOT EXISTS fts_files_delete AFTER DELETE ON indexed_files BEGIN
	DELETE FROM fts_files WHERE file_id = old.id;
END;

-- ignore_rules lists what the scanner leaves out of the index: a path inside
-- the library (a folder and everything under it, or one file), a file
-- extension, or a name pattern matched against every file and folder name.
CREATE TABLE IF NOT EXISTS ignore_rules (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	kind       TEXT NOT NULL CHECK (kind IN ('path', 'extension', 'name')),
	pattern    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE (kind, pattern)
);

-- favorite_folders are folders starred as favourites, by path. A folder has no
-- id of its own (it is whatever is on disk), so renaming or moving it through
-- Popcorn rewrites the path here too.
CREATE TABLE IF NOT EXISTS favorite_folders (
	rel_path   TEXT PRIMARY KEY,
	created_at TEXT NOT NULL
);

-- duplicate_ignores are groups of byte-identical files the user has looked at
-- and wants kept: the duplicates page leaves a group with an ignored content
-- hash out until it is un-ignored.
CREATE TABLE IF NOT EXISTS duplicate_ignores (
	content_hash TEXT PRIMARY KEY,
	created_at   TEXT NOT NULL
);
`
