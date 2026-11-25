package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
	"github.com/Jishnu-Prasad888/popcorn/internal/stream"
)

// ErrInvalid is a rejected edit.
type ErrInvalid struct{ Msg string }

func (e *ErrInvalid) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ErrInvalid{Msg: fmt.Sprintf(format, a...)} }

// editable lists the fields a person may edit per item table, with their
// kind: s=string, i=int, f=float, l=list of strings.
var editable = map[string]map[string]byte{
	"movies": {
		"title": 's', "original_title": 's', "sort_title": 's', "year": 'i', "release_date": 's',
		"overview": 's', "tagline": 's', "runtime_mins": 'i', "content_rating": 's', "rating": 'f',
		"imdb_rating": 'f', "imdb_id": 's', "tmdb_id": 'i', "genres": 'l', "languages": 'l',
		"original_language": 's', "countries": 'l', "studios": 'l',
	},
	"shows": {
		"title": 's', "original_title": 's', "sort_title": 's', "year": 'i', "first_air_date": 's',
		"last_air_date": 's', "status": 's', "overview": 's', "tagline": 's', "content_rating": 's',
		"rating": 'f', "imdb_rating": 'f', "imdb_id": 's', "tmdb_id": 'i', "genres": 'l', "languages": 'l',
		"original_language": 's', "countries": 'l', "studios": 'l', "category": 's',
	},
	"seasons": {"title": 's', "overview": 's', "air_date": 's'},
	"episodes": {
		"title": 's', "overview": 's', "air_date": 's', "runtime_mins": 'i', "rating": 'f',
		"intro_start": 'f', "intro_end": 'f', "credits_start": 'f',
	},
}

// TableFor maps an item type to its table.
func TableFor(itemType string) string {
	return map[string]string{"movie": "movies", "show": "shows", "season": "seasons", "episode": "episodes"}[itemType]
}

// Edit applies a person's edits to an item and locks the edited fields, so a
// metadata refresh leaves them alone. A null value clears a field. Markers
// (intro/credits) are not locked: they are not provider data.
func (s *Store) Edit(ctx context.Context, itemType, id string, changes map[string]json.RawMessage) error {
	table := TableFor(itemType)
	allowed := editable[table]
	if allowed == nil {
		return invalid("unknown item type %q", itemType)
	}
	var lockedRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT locked_fields FROM `+table+` WHERE id = ?`, id).Scan(&lockedRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	locked := parseLocked(lockedRaw)
	sets := []string{}
	args := []any{}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, field := range keys {
		raw := changes[field]
		kind, ok := allowed[field]
		if !ok {
			return invalid("field %q cannot be edited", field)
		}
		var val any
		if string(raw) != "null" {
			switch kind {
			case 's':
				var v string
				if err := json.Unmarshal(raw, &v); err != nil {
					return invalid("%s must be text", field)
				}
				v = strings.TrimSpace(v)
				if field == "title" && v == "" {
					return invalid("title cannot be empty")
				}
				if field == "category" && v != "tv" && v != "anime" && v != "web" {
					return invalid("category must be tv, anime or web")
				}
				if field == "original_language" {
					v = probe.ShortLanguage(probe.NormalizeLanguage(v))
				}
				val = nullStr(v)
			case 'i':
				var v int
				if err := json.Unmarshal(raw, &v); err != nil {
					return invalid("%s must be a whole number", field)
				}
				val = nullInt(v)
			case 'f':
				var v float64
				if err := json.Unmarshal(raw, &v); err != nil {
					return invalid("%s must be a number", field)
				}
				if v < 0 {
					return invalid("%s cannot be negative", field)
				}
				val = v
			case 'l':
				var v []string
				if err := json.Unmarshal(raw, &v); err != nil {
					return invalid("%s must be a list of text", field)
				}
				clean := []string{}
				for _, x := range v {
					if x = strings.TrimSpace(x); x != "" {
						clean = append(clean, x)
					}
				}
				val = jsonList(clean)
			}
		} else if kind == 'l' {
			val = "[]"
		} else if field == "title" {
			return invalid("title cannot be empty")
		}
		sets = append(sets, field+" = ?")
		args = append(args, val)
		if field == "title" {
			if _, ok := changes["sort_title"]; !ok && !locked["sort_title"] {
				t := ""
				_ = json.Unmarshal(raw, &t)
				sets = append(sets, "sort_title = ?")
				args = append(args, SortTitle(t))
			}
		}
		if !strings.HasPrefix(field, "intro_") && field != "credits_start" {
			locked[field] = true
		}
	}
	if len(sets) == 0 {
		return nil
	}
	lockedList := make([]string, 0, len(locked))
	for f := range locked {
		lockedList = append(lockedList, f)
	}
	sort.Strings(lockedList)
	sets = append(sets, "locked_fields = ?")
	args = append(args, jsonList(lockedList))
	if table != "seasons" {
		sets = append(sets, "updated_at = ?")
		args = append(args, now())
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// Unlock removes fields from an item's locked list so the next refresh
// fills them from the provider again. An empty list unlocks everything.
func (s *Store) Unlock(ctx context.Context, itemType, id string, fields []string) error {
	table := TableFor(itemType)
	if table == "" {
		return invalid("unknown item type")
	}
	var lockedRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT locked_fields FROM `+table+` WHERE id = ?`, id).Scan(&lockedRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	locked := parseLocked(lockedRaw)
	if len(fields) == 0 {
		locked = map[string]bool{}
	}
	for _, f := range fields {
		delete(locked, f)
	}
	list := []string{}
	for f := range locked {
		list = append(list, f)
	}
	sort.Strings(list)
	_, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET locked_fields = ? WHERE id = ?`, jsonList(list), id)
	return err
}

// ResetMatch forgets an item's provider match so it is matched again from
// its name (or by hand).
func (s *Store) ResetMatch(ctx context.Context, itemType, id string) error {
	table := TableFor(itemType)
	if table != "movies" && table != "shows" {
		return invalid("only movies and shows are matched")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET match_status = 'unmatched', tmdb_id = NULL, imdb_id = NULL WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Exists reports whether an item exists.
func (s *Store) Exists(ctx context.Context, itemType, id string) (bool, error) {
	table := TableFor(itemType)
	if table == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// SaveUploadedArtwork stores an uploaded image as an item's artwork. Uploads
// win over local and provider artwork until removed.
func (s *Store) SaveUploadedArtwork(ctx context.Context, popcornDir, itemType, itemID, kind, ext string, data []byte) error {
	if ok, err := s.Exists(ctx, itemType, itemID); err != nil || !ok {
		if err == nil {
			err = ErrNotFound
		}
		return err
	}
	switch kind {
	case "poster", "backdrop", "logo", "thumb":
	default:
		return invalid("unknown artwork kind %q", kind)
	}
	rel := ArtworkRelPath(itemType, itemID, kind+"-upload", ext)
	dest := filepath.Join(popcornDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest+".part", data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(dest+".part", dest); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO artwork (item_type, item_id, kind, source, path, updated_at) VALUES (?, ?, ?, 'upload', ?, ?)
		 ON CONFLICT(item_type, item_id, kind) DO UPDATE SET source = 'upload', path = excluded.path,
		   remote_url = NULL, updated_at = excluded.updated_at`,
		itemType, itemID, kind, rel, now()); err != nil {
		return err
	}
	BumpArtwork(ctx, s.db, itemType, itemID)
	return nil
}

// RemoveArtwork deletes an item's artwork of a kind (an uploaded file is
// removed from .popcorn/; local artwork in the library is never deleted,
// only forgotten until the next scan).
func (s *Store) RemoveArtwork(ctx context.Context, popcornDir, itemType, itemID, kind string) error {
	p, inLib, err := s.ArtworkPath(ctx, itemType, itemID, kind)
	if err != nil {
		return err
	}
	if !inLib {
		_ = os.Remove(filepath.Join(popcornDir, filepath.FromSlash(p)))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artwork WHERE item_type = ? AND item_id = ? AND kind = ?`,
		itemType, itemID, kind); err != nil {
		return err
	}
	BumpArtwork(ctx, s.db, itemType, itemID)
	return nil
}

// GenerateArtwork grabs a frame from an item's video as its thumbnail (for
// episodes) or poster/backdrop (movies), unless better artwork exists and
// force is false. at is the position in seconds; <= 0 picks a point a third
// of the way in.
func (s *Store) GenerateArtwork(ctx context.Context, root, popcornDir, itemType, itemID, kind string, at float64, force bool) error {
	if !force {
		if _, _, err := s.ArtworkPath(ctx, itemType, itemID, kind); err == nil {
			return nil
		}
	}
	files, err := s.Files(ctx, itemType, itemID)
	if err != nil {
		return err
	}
	var f *FileInfo
	for i := range files {
		if files[i].Status == "present" {
			f = &files[i]
			break
		}
	}
	if f == nil {
		return invalid("no playable file")
	}
	if at <= 0 {
		at = f.DurationSecs / 3
		if at <= 0 {
			at = 60
		}
	}
	width := 960
	if kind == "backdrop" {
		width = 1600
	}
	rel := ArtworkRelPath(itemType, itemID, kind+"-frame", ".jpg")
	dest := filepath.Join(popcornDir, filepath.FromSlash(rel))
	if err := stream.Frame(ctx, filepath.Join(root, filepath.FromSlash(f.RelPath)), at, width, dest); err != nil {
		return err
	}
	src := "generated"
	if force {
		src = "upload" // chosen by a person: keep it over provider art
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO artwork (item_type, item_id, kind, source, path, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(item_type, item_id, kind) DO UPDATE SET source = excluded.source, path = excluded.path,
		   remote_url = NULL, updated_at = excluded.updated_at`,
		itemType, itemID, kind, src, rel, now()); err != nil {
		return err
	}
	BumpArtwork(ctx, s.db, itemType, itemID)
	return nil
}

// MissingThumbnails lists episodes without a thumbnail and movies without a
// poster, which get a frame grabbed from the video.
func (s *Store) MissingThumbnails(ctx context.Context, limit int) ([]ItemRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'episode', e.id FROM episodes e WHERE NOT EXISTS (
			SELECT 1 FROM artwork a WHERE a.item_type = 'episode' AND a.item_id = e.id AND a.kind = 'thumb')
		  AND EXISTS (SELECT 1 FROM episode_files ef JOIN video_files v ON v.file_id = ef.file_id
		              WHERE ef.episode_id = e.id AND v.duration_secs > 0)
		UNION ALL
		SELECT 'movie', m.id FROM movies m WHERE m.match_status IN ('failed') AND NOT EXISTS (
			SELECT 1 FROM artwork a WHERE a.item_type = 'movie' AND a.item_id = m.id AND a.kind IN ('poster', 'backdrop'))
		  AND EXISTS (SELECT 1 FROM movie_files mf JOIN video_files v ON v.file_id = mf.file_id
		              WHERE mf.movie_id = m.id AND v.duration_secs > 0)
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ItemRef
	for rows.Next() {
		var r ItemRef
		if err := rows.Scan(&r.Type, &r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AttachFile makes a video file a version of a movie (by hand). The file
// leaves whatever movie or episode it belonged to.
func (s *Store) AttachFile(ctx context.Context, movieID, fileID, edition string) error {
	if ok, err := s.Exists(ctx, "movie", movieID); err != nil || !ok {
		if err == nil {
			err = ErrNotFound
		}
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var rel string
	if err := tx.QueryRowContext(ctx, `SELECT rel_path FROM indexed_files WHERE id = ? AND status != 'deleted'`, fileID).Scan(&rel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if !IsVideo(rel) {
		return invalid("%s is not a video file", rel)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM episode_files WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO movie_files (file_id, movie_id, edition, position, manual) VALUES (?, ?, ?, 0, 1)
		 ON CONFLICT(file_id) DO UPDATE SET movie_id = excluded.movie_id, edition = excluded.edition, manual = 1`,
		fileID, movieID, nullStr(edition)); err != nil {
		return err
	}
	if err := pruneEmpty(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// SetEdition labels a movie or episode file's version.
func (s *Store) SetEdition(ctx context.Context, fileID, edition string) error {
	for _, t := range []string{"movie_files", "episode_files"} {
		if _, err := s.db.ExecContext(ctx, `UPDATE `+t+` SET edition = ? WHERE file_id = ?`, nullStr(strings.TrimSpace(edition)), fileID); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseFile undoes a manual attachment: the next sync places the file by
// its name again.
func (s *Store) ReleaseFile(ctx context.Context, fileID string) error {
	for _, t := range []string{"movie_files", "episode_files"} {
		if _, err := s.db.ExecContext(ctx, `UPDATE `+t+` SET manual = 0 WHERE file_id = ?`, fileID); err != nil {
			return err
		}
	}
	return nil
}

// SplitFile takes a file out of its movie into a new movie of its own.
func (s *Store) SplitFile(ctx context.Context, fileID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var movieID, rel string
	err = tx.QueryRowContext(ctx, `SELECT mf.movie_id, f.rel_path FROM movie_files mf JOIN indexed_files f ON f.id = mf.file_id
		WHERE mf.file_id = ?`, fileID).Scan(&movieID, &rel)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	pm := ParseMovie(rel)
	id := NewID()
	t := now()
	key := "split:" + fileID
	if _, err := tx.ExecContext(ctx, `INSERT INTO movies (id, match_key, title, sort_title, year, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, key, pm.Title, SortTitle(pm.Title), nullInt(pm.Year), t, t); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE movie_files SET movie_id = ?, manual = 1 WHERE file_id = ?`, id, fileID); err != nil {
		return "", err
	}
	if err := pruneEmpty(ctx, tx); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// MergeMovies moves every file of the other movies into target, making them
// versions of one movie.
func (s *Store) MergeMovies(ctx context.Context, target string, others []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, o := range others {
		if o == target {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE movie_files SET movie_id = ?, manual = 1 WHERE movie_id = ?`, target, o); err != nil {
			return err
		}
	}
	if err := pruneEmpty(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveEpisode renumbers an episode (and can move it to another season). Its
// files are pinned so the folder sync keeps the new numbering.
func (s *Store) MoveEpisode(ctx context.Context, episodeID string, season, number int) error {
	if season < 0 || number < 0 {
		return invalid("season and episode numbers cannot be negative")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var showID string
	if err := tx.QueryRowContext(ctx, `SELECT show_id FROM episodes WHERE id = ?`, episodeID).Scan(&showID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var clash string
	err = tx.QueryRowContext(ctx, `SELECT id FROM episodes WHERE show_id = ? AND season_number = ? AND episode_number = ? AND id != ?`,
		showID, season, number, episodeID).Scan(&clash)
	if err == nil {
		return invalid("season %d already has an episode %d", season, number)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var seasonID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM seasons WHERE show_id = ? AND season_number = ?`, showID, season).Scan(&seasonID)
	if errors.Is(err, sql.ErrNoRows) {
		seasonID = NewID()
		title := fmt.Sprintf("Season %d", season)
		order := season
		if season == 0 {
			title, order = "Specials", 10000
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO seasons (id, show_id, season_number, title, sort_order) VALUES (?, ?, ?, ?, ?)`,
			seasonID, showID, season, title, order); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE episodes SET season_id = ?, season_number = ?, episode_number = ?, sort_order = ?,
		updated_at = ? WHERE id = ?`, seasonID, season, number, number, now(), episodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE episode_files SET manual = 1 WHERE episode_id = ?`, episodeID); err != nil {
		return err
	}
	if err := pruneEmpty(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ReorderSeasons sets a show's season display order.
func (s *Store) ReorderSeasons(ctx context.Context, showID string, seasonIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range seasonIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE seasons SET sort_order = ? WHERE id = ? AND show_id = ?`, i, id, showID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReorderEpisodes sets a season's episode display (and playback) order.
func (s *Store) ReorderEpisodes(ctx context.Context, seasonID string, episodeIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range episodeIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE episodes SET sort_order = ? WHERE id = ? AND season_id = ?`, i+1, id, seasonID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func pruneEmpty(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DELETE FROM movies WHERE id NOT IN (SELECT movie_id FROM movie_files)`,
		`DELETE FROM episodes WHERE id NOT IN (SELECT episode_id FROM episode_files)`,
		`DELETE FROM seasons WHERE id NOT IN (SELECT season_id FROM episodes)`,
		`DELETE FROM shows WHERE id NOT IN (SELECT show_id FROM episodes)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// SubtitleDir is where uploaded subtitles are kept.
func SubtitleDir(popcornDir string) string { return filepath.Join(popcornDir, "subtitles") }

// AddUploadedSubtitle stores an uploaded subtitle file for a video.
func (s *Store) AddUploadedSubtitle(ctx context.Context, popcornDir, fileID, format string, data []byte, info SubtitleInfo) (string, error) {
	format = strings.ToLower(strings.TrimPrefix(format, "."))
	if !subtitleExts["."+format] {
		return "", invalid("subtitles must be .srt, .ass, .ssa, .vtt or .sub")
	}
	if _, _, err := s.ItemForFile(ctx, fileID); err != nil {
		return "", err
	}
	id := NewID()
	rel := filepath.ToSlash(filepath.Join("subtitles", fileID, id+"."+format))
	dest := filepath.Join(popcornDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO external_subtitles (id, file_id, source, path, format, language, title,
			is_forced, is_hearing_impaired, created_at) VALUES (?, ?, 'upload', ?, ?, ?, ?, ?, ?, ?)`,
		id, fileID, rel, format, nullStr(probe.NormalizeLanguage(info.Language)), nullStr(info.Title),
		boolInt(info.Forced), boolInt(info.SDH), now()); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return id, nil
}

// UpdateSubtitle changes a subtitle's label: language, title, forced, SDH.
func (s *Store) UpdateSubtitle(ctx context.Context, id string, info SubtitleInfo) error {
	res, err := s.db.ExecContext(ctx, `UPDATE external_subtitles SET language = ?, title = ?, is_forced = ?,
		is_hearing_impaired = ? WHERE id = ?`, nullStr(probe.NormalizeLanguage(info.Language)), nullStr(info.Title),
		boolInt(info.Forced), boolInt(info.SDH), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceSubtitle overwrites a subtitle's content (an upload in .popcorn/ or
// a sidecar file in the library).
func (s *Store) ReplaceSubtitle(ctx context.Context, root, popcornDir, id string, data []byte) error {
	sub, _, err := s.Subtitle(ctx, id)
	if err != nil {
		return err
	}
	dest := filepath.Join(popcornDir, filepath.FromSlash(sub.Path))
	if sub.Source == "sidecar" {
		dest = filepath.Join(root, filepath.FromSlash(sub.Path))
	}
	if err := os.WriteFile(dest+".popcorn-tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(dest+".popcorn-tmp", dest)
}

// RenameSidecar renames a sidecar subtitle file next to its video, keeping
// its folder and extension.
func (s *Store) RenameSidecar(ctx context.Context, root, id, newBase string) (string, error) {
	sub, _, err := s.Subtitle(ctx, id)
	if err != nil {
		return "", err
	}
	if sub.Source != "sidecar" {
		return "", invalid("only subtitle files in the library can be renamed")
	}
	newBase = strings.TrimSpace(newBase)
	if newBase == "" || strings.ContainsAny(newBase, `/\`) || newBase == "." || newBase == ".." {
		return "", invalid("enter a file name without folders")
	}
	ext := filepath.Ext(sub.Path)
	if !strings.HasSuffix(strings.ToLower(newBase), strings.ToLower(ext)) {
		newBase += ext
	}
	newRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(sub.Path)), newBase))
	from := filepath.Join(root, filepath.FromSlash(sub.Path))
	to := filepath.Join(root, filepath.FromSlash(newRel))
	if _, err := os.Stat(to); err == nil {
		return "", invalid("a file named %s already exists", newBase)
	}
	if err := os.Rename(from, to); err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE external_subtitles SET path = ? WHERE id = ?`, newRel, id); err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE indexed_files SET rel_path = ? WHERE rel_path = ?`, newRel, sub.Path); err != nil {
		return "", err
	}
	return newRel, nil
}

// RemoveSubtitle deletes a subtitle: an upload is removed from .popcorn/, a
// sidecar subtitle file is deleted from the library. Video files are never
// touched.
func (s *Store) RemoveSubtitle(ctx context.Context, root, popcornDir, id string) error {
	sub, _, err := s.Subtitle(ctx, id)
	if err != nil {
		return err
	}
	p := filepath.Join(popcornDir, filepath.FromSlash(sub.Path))
	if sub.Source == "sidecar" {
		if !IsSubtitle(sub.Path) {
			return invalid("not a subtitle file")
		}
		p = filepath.Join(root, filepath.FromSlash(sub.Path))
		if _, err := s.db.ExecContext(ctx, `UPDATE indexed_files SET status = 'deleted' WHERE rel_path = ?`, sub.Path); err != nil {
			return err
		}
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM external_subtitles WHERE id = ?`, id)
	return err
}

// UnassignedVideos lists video files that belong to no movie or episode
// (unrecognised names, extras), for manual matching.
func (s *Store) UnassignedVideos(ctx context.Context) ([]FileRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, rel_path FROM indexed_files f WHERE status = 'present'
		AND NOT EXISTS (SELECT 1 FROM movie_files WHERE file_id = f.id)
		AND NOT EXISTS (SELECT 1 FROM episode_files WHERE file_id = f.id) ORDER BY rel_path`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FileRef
	for rows.Next() {
		var f FileRef
		if err := rows.Scan(&f.ID, &f.RelPath); err != nil {
			return nil, err
		}
		if IsVideo(f.RelPath) {
			out = append(out, f)
		}
	}
	return out, rows.Err()
}
