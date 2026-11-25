package librarydb

import "database/sql"

// migrateCatalog creates the media-server catalog: what the library's video
// files are (movies, shows, seasons, episodes), what is inside them (streams,
// chapters), the subtitles that go with them, and the metadata and artwork
// fetched for them.
//
// Media files stay where they are. Every table here references files through
// indexed_files.id; deleting a catalog row never touches a file on disk.
func migrateCatalog(db *sql.DB) error {
	_, err := db.Exec(catalogSchema)
	return err
}

const catalogSchema = `
-- video_files holds what ffprobe found in a video file. probe_size and
-- probe_mtime record the file version that was probed, so a changed file is
-- probed again.
CREATE TABLE IF NOT EXISTS video_files (
	file_id       TEXT PRIMARY KEY REFERENCES indexed_files(id) ON DELETE CASCADE,
	container     TEXT,
	duration_secs REAL,
	bitrate       INTEGER,
	width         INTEGER,
	height        INTEGER,
	video_codec   TEXT,
	video_profile TEXT,
	pix_fmt       TEXT,
	frame_rate    REAL,
	hdr           TEXT,
	chapters      TEXT NOT NULL DEFAULT '[]',
	probe_size    INTEGER,
	probe_mtime   TEXT,
	probed_at     TEXT,
	probe_error   TEXT
);

-- media_streams lists every stream inside a probed file. type_index is the
-- stream's position among streams of its kind, which is what ffmpeg's
-- "-map 0:a:N" selects.
CREATE TABLE IF NOT EXISTS media_streams (
	file_id             TEXT NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
	stream_index        INTEGER NOT NULL,
	kind                TEXT NOT NULL CHECK (kind IN ('video', 'audio', 'subtitle', 'attachment', 'data')),
	type_index          INTEGER NOT NULL,
	codec               TEXT,
	profile             TEXT,
	language            TEXT,
	title               TEXT,
	channels            INTEGER,
	channel_layout      TEXT,
	sample_rate         INTEGER,
	bitrate             INTEGER,
	width               INTEGER,
	height              INTEGER,
	is_default          INTEGER NOT NULL DEFAULT 0,
	is_forced           INTEGER NOT NULL DEFAULT 0,
	is_hearing_impaired INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (file_id, stream_index)
);

CREATE INDEX IF NOT EXISTS media_streams_kind_idx ON media_streams (file_id, kind, type_index);

-- external_subtitles are subtitle files that go with a video: sidecars found
-- next to it in the library (path relative to the library root) or files a
-- person uploaded (path relative to .popcorn/). Original files are never
-- modified.
CREATE TABLE IF NOT EXISTS external_subtitles (
	id                  TEXT PRIMARY KEY,
	file_id             TEXT NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
	source              TEXT NOT NULL CHECK (source IN ('sidecar', 'upload')),
	path                TEXT NOT NULL,
	format              TEXT NOT NULL,
	language            TEXT,
	title               TEXT,
	is_forced           INTEGER NOT NULL DEFAULT 0,
	is_hearing_impaired INTEGER NOT NULL DEFAULT 0,
	created_at          TEXT NOT NULL,
	UNIQUE (file_id, path)
);

CREATE INDEX IF NOT EXISTS external_subtitles_file_idx ON external_subtitles (file_id);

-- movies are the films in the library. match_key is the normalised title and
-- year read from the file or folder name: files that share one are versions
-- (editions) of the same movie. locked_fields lists fields a person edited by
-- hand, which a metadata refresh leaves alone.
CREATE TABLE IF NOT EXISTS movies (
	id                TEXT PRIMARY KEY,
	match_key         TEXT NOT NULL UNIQUE,
	title             TEXT NOT NULL,
	original_title    TEXT,
	sort_title        TEXT NOT NULL,
	year              INTEGER,
	release_date      TEXT,
	overview          TEXT,
	tagline           TEXT,
	runtime_mins      INTEGER,
	content_rating    TEXT,
	rating            REAL,
	vote_count        INTEGER,
	imdb_rating       REAL,
	imdb_votes        INTEGER,
	imdb_id           TEXT,
	tmdb_id           INTEGER,
	genres            TEXT NOT NULL DEFAULT '[]',
	languages         TEXT NOT NULL DEFAULT '[]',
	original_language TEXT,
	countries         TEXT NOT NULL DEFAULT '[]',
	studios           TEXT NOT NULL DEFAULT '[]',
	folder            TEXT,
	match_status      TEXT NOT NULL DEFAULT 'unmatched'
		CHECK (match_status IN ('unmatched', 'matched', 'manual', 'failed')),
	locked_fields     TEXT NOT NULL DEFAULT '[]',
	artwork_version   INTEGER NOT NULL DEFAULT 0,
	added_at          TEXT NOT NULL,
	updated_at        TEXT NOT NULL,
	metadata_at       TEXT
);

CREATE INDEX IF NOT EXISTS movies_sort_idx  ON movies (sort_title);
CREATE INDEX IF NOT EXISTS movies_added_idx ON movies (added_at);
CREATE INDEX IF NOT EXISTS movies_tmdb_idx  ON movies (tmdb_id);

-- movie_files attaches video files to a movie. A movie can have several
-- (editions, resolutions); edition labels them. manual marks an attachment a
-- person made, which the folder sync never undoes.
CREATE TABLE IF NOT EXISTS movie_files (
	file_id  TEXT PRIMARY KEY REFERENCES indexed_files(id) ON DELETE CASCADE,
	movie_id TEXT NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
	edition  TEXT,
	position INTEGER NOT NULL DEFAULT 0,
	manual   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS movie_files_movie_idx ON movie_files (movie_id, position);

-- shows are TV shows, web series and anime. category separates them for
-- browsing; match_key is the normalised name of the show's folder.
CREATE TABLE IF NOT EXISTS shows (
	id                TEXT PRIMARY KEY,
	match_key         TEXT NOT NULL UNIQUE,
	title             TEXT NOT NULL,
	original_title    TEXT,
	sort_title        TEXT NOT NULL,
	year              INTEGER,
	first_air_date    TEXT,
	last_air_date     TEXT,
	status            TEXT,
	overview          TEXT,
	tagline           TEXT,
	content_rating    TEXT,
	rating            REAL,
	vote_count        INTEGER,
	imdb_rating       REAL,
	imdb_votes        INTEGER,
	imdb_id           TEXT,
	tmdb_id           INTEGER,
	category          TEXT NOT NULL DEFAULT 'tv' CHECK (category IN ('tv', 'anime', 'web')),
	genres            TEXT NOT NULL DEFAULT '[]',
	languages         TEXT NOT NULL DEFAULT '[]',
	original_language TEXT,
	countries         TEXT NOT NULL DEFAULT '[]',
	studios           TEXT NOT NULL DEFAULT '[]',
	folder            TEXT,
	match_status      TEXT NOT NULL DEFAULT 'unmatched'
		CHECK (match_status IN ('unmatched', 'matched', 'manual', 'failed')),
	locked_fields     TEXT NOT NULL DEFAULT '[]',
	artwork_version   INTEGER NOT NULL DEFAULT 0,
	added_at          TEXT NOT NULL,
	updated_at        TEXT NOT NULL,
	metadata_at       TEXT
);

CREATE INDEX IF NOT EXISTS shows_sort_idx  ON shows (sort_title);
CREATE INDEX IF NOT EXISTS shows_added_idx ON shows (added_at);

-- seasons of a show. Season 0 holds specials. sort_order lets a person
-- rearrange seasons; it defaults to the season number.
CREATE TABLE IF NOT EXISTS seasons (
	id              TEXT PRIMARY KEY,
	show_id         TEXT NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
	season_number   INTEGER NOT NULL,
	title           TEXT,
	overview        TEXT,
	air_date        TEXT,
	episode_total   INTEGER,
	sort_order      INTEGER NOT NULL,
	locked_fields   TEXT NOT NULL DEFAULT '[]',
	artwork_version INTEGER NOT NULL DEFAULT 0,
	UNIQUE (show_id, season_number)
);

-- episodes of a season. episode_end is set for a file holding several
-- episodes (S01E01-E02). intro_* and credits_start drive skip buttons; they
-- come from chapter markers or are set by hand.
CREATE TABLE IF NOT EXISTS episodes (
	id              TEXT PRIMARY KEY,
	show_id         TEXT NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
	season_id       TEXT NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
	season_number   INTEGER NOT NULL,
	episode_number  INTEGER NOT NULL,
	episode_end     INTEGER,
	title           TEXT,
	overview        TEXT,
	air_date        TEXT,
	runtime_mins    INTEGER,
	rating          REAL,
	vote_count      INTEGER,
	imdb_id         TEXT,
	tmdb_id         INTEGER,
	sort_order      INTEGER NOT NULL,
	intro_start     REAL,
	intro_end       REAL,
	credits_start   REAL,
	locked_fields   TEXT NOT NULL DEFAULT '[]',
	artwork_version INTEGER NOT NULL DEFAULT 0,
	added_at        TEXT NOT NULL,
	updated_at      TEXT NOT NULL,
	UNIQUE (show_id, season_number, episode_number)
);

CREATE INDEX IF NOT EXISTS episodes_season_idx ON episodes (season_id, sort_order);
CREATE INDEX IF NOT EXISTS episodes_added_idx  ON episodes (added_at);

CREATE TABLE IF NOT EXISTS episode_files (
	file_id    TEXT PRIMARY KEY REFERENCES indexed_files(id) ON DELETE CASCADE,
	episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
	edition    TEXT,
	position   INTEGER NOT NULL DEFAULT 0,
	manual     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS episode_files_episode_idx ON episode_files (episode_id, position);

-- people are cast and crew, shared by every item that credits them.
CREATE TABLE IF NOT EXISTS people (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	tmdb_id     INTEGER UNIQUE,
	profile_url TEXT,
	updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS people_name_idx ON people (name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS credits (
	item_type TEXT NOT NULL CHECK (item_type IN ('movie', 'show', 'episode')),
	item_id   TEXT NOT NULL,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	role      TEXT NOT NULL CHECK (role IN ('cast', 'director', 'writer', 'producer', 'creator')),
	character TEXT,
	ord       INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (item_type, item_id, person_id, role)
);

CREATE INDEX IF NOT EXISTS credits_person_idx ON credits (person_id, role);

-- artwork records the image to show for each item and kind. path is relative
-- to .popcorn/ for provider downloads, uploads and generated frames, and
-- relative to the library root for local artwork (poster.jpg next to the
-- media), which is never copied.
CREATE TABLE IF NOT EXISTS artwork (
	item_type  TEXT NOT NULL CHECK (item_type IN ('movie', 'show', 'season', 'episode', 'person')),
	item_id    TEXT NOT NULL,
	kind       TEXT NOT NULL CHECK (kind IN ('poster', 'backdrop', 'logo', 'thumb')),
	source     TEXT NOT NULL CHECK (source IN ('provider', 'upload', 'generated', 'local')),
	path       TEXT NOT NULL,
	remote_url TEXT,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (item_type, item_id, kind)
);
`
