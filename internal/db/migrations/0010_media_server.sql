-- 0010_media_server: per-user state for the movie and TV media server.
--
-- Library content (movies, shows, seasons, episodes, streams, artwork) lives in
-- each library's own database at <root>/.popcorn/library.db. What belongs to a
-- person rather than to a library lives here, so it follows the user across
-- devices and survives a library being re-adopted: playback progress, watch
-- history, favourites and watchlists, collections, and playlists.
--
-- Items are always referenced as (library_id, item_type, item_id). Item ids are
-- random and globally unique, but the library id says which library database
-- holds the item.

-- kind says what a library holds, which steers how its folders are read:
-- 'movies' (one movie per file or folder), 'shows' (Show/Season/Episode), or
-- 'mixed' (decided per file from its name).
ALTER TABLE libraries ADD COLUMN kind TEXT NOT NULL DEFAULT 'mixed'
	CHECK (kind IN ('movies', 'shows', 'mixed'));

-- playback_state is one row per user per playable item: where they stopped,
-- whether they finished it, and the tracks they chose.
CREATE TABLE playback_state (
	user_id           TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	library_id        TEXT NOT NULL,
	item_type         TEXT NOT NULL CHECK (item_type IN ('movie', 'episode')),
	item_id           TEXT NOT NULL,
	show_id           TEXT,
	file_id           TEXT,
	position_secs     REAL NOT NULL DEFAULT 0,
	duration_secs     REAL NOT NULL DEFAULT 0,
	played            INTEGER NOT NULL DEFAULT 0,
	play_count        INTEGER NOT NULL DEFAULT 0,
	audio_language    TEXT,
	subtitle_language TEXT,
	last_played_at    TEXT,
	updated_at        TEXT NOT NULL,
	PRIMARY KEY (user_id, library_id, item_type, item_id)
);

CREATE INDEX playback_state_recent_idx ON playback_state (user_id, last_played_at);
CREATE INDEX playback_state_show_idx ON playback_state (user_id, show_id);

-- watch_history is an append-only log of finished (or substantially watched)
-- plays.
CREATE TABLE watch_history (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	library_id    TEXT NOT NULL,
	item_type     TEXT NOT NULL CHECK (item_type IN ('movie', 'episode')),
	item_id       TEXT NOT NULL,
	show_id       TEXT,
	watched_at    TEXT NOT NULL,
	watched_secs  REAL NOT NULL DEFAULT 0,
	completed     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX watch_history_user_idx ON watch_history (user_id, watched_at DESC);
CREATE INDEX watch_history_item_idx ON watch_history (library_id, item_type, item_id);

-- user_items holds a person's favourites and watchlist.
CREATE TABLE user_items (
	user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	list       TEXT NOT NULL CHECK (list IN ('favorite', 'watchlist')),
	library_id TEXT NOT NULL,
	item_type  TEXT NOT NULL CHECK (item_type IN ('movie', 'show', 'episode')),
	item_id    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (user_id, list, library_id, item_type, item_id)
);

CREATE INDEX user_items_recent_idx ON user_items (user_id, list, created_at DESC);

-- collections are hand-made, ordered groups of movies and shows, owned by one
-- person. They may mix items from several libraries.
CREATE TABLE collections (
	id             TEXT PRIMARY KEY,
	owner_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	name           TEXT NOT NULL,
	description    TEXT NOT NULL DEFAULT '',
	poster_version INTEGER NOT NULL DEFAULT 0,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL
);

CREATE INDEX collections_owner_idx ON collections (owner_id, name COLLATE NOCASE);

CREATE TABLE collection_items (
	collection_id TEXT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
	library_id    TEXT NOT NULL,
	item_type     TEXT NOT NULL CHECK (item_type IN ('movie', 'show')),
	item_id       TEXT NOT NULL,
	position      INTEGER NOT NULL,
	added_at      TEXT NOT NULL,
	PRIMARY KEY (collection_id, library_id, item_type, item_id)
);

CREATE INDEX collection_items_order_idx ON collection_items (collection_id, position);

-- playlists are ordered queues of playable items. Unlike collections an item
-- may appear more than once, so each entry has its own id.
CREATE TABLE playlists (
	id              TEXT PRIMARY KEY,
	owner_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	shuffle         INTEGER NOT NULL DEFAULT 0,
	repeat_mode     TEXT NOT NULL DEFAULT 'off' CHECK (repeat_mode IN ('off', 'all', 'one')),
	current_entry   TEXT,
	poster_version  INTEGER NOT NULL DEFAULT 0,
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL
);

CREATE INDEX playlists_owner_idx ON playlists (owner_id, name COLLATE NOCASE);

CREATE TABLE playlist_items (
	id            TEXT PRIMARY KEY,
	playlist_id   TEXT NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
	library_id    TEXT NOT NULL,
	item_type     TEXT NOT NULL CHECK (item_type IN ('movie', 'episode')),
	item_id       TEXT NOT NULL,
	position      INTEGER NOT NULL,
	position_secs REAL NOT NULL DEFAULT 0,
	added_at      TEXT NOT NULL
);

CREATE INDEX playlist_items_order_idx ON playlist_items (playlist_id, position);
