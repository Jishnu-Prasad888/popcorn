package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Library kinds, matching libraries.kind in the server database.
const (
	KindMovies = "movies"
	KindShows  = "shows"
	KindMixed  = "mixed"
)

// SyncResult summarises a catalog sync.
type SyncResult struct {
	Movies, Shows, Episodes int
	NewItems                int
	// NeedMetadata are items that have never been matched to a metadata
	// provider: ("movie"|"show", id).
	NeedMetadata []ItemRef
	// NeedProbe are video files without (current) stream information.
	NeedProbe []FileRef
}

// ItemRef names a catalog item.
type ItemRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// FileRef names an indexed file.
type FileRef struct {
	ID      string
	RelPath string
}

type fileRow struct {
	id, rel string
	size    int64
	mtime   string
	status  string
}

// NewID returns a random 32-hex-character identifier.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Sync brings the catalog in line with the indexed files of one library. It
// is idempotent: running it twice changes nothing the second time.
func Sync(ctx context.Context, db *sql.DB, kind string) (*SyncResult, error) {
	files, err := loadFiles(ctx, db)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	s := &syncer{tx: tx, ctx: ctx, kind: kind, res: &SyncResult{},
		movieByKey: map[string]string{}, showByKey: map[string]string{},
		seasonByKey: map[string]string{}, episodeByKey: map[string]string{},
		videosByDir: map[string][]fileRow{}}
	if err := s.loadExisting(); err != nil {
		return nil, err
	}

	var videos, subs, images []fileRow
	for _, f := range files {
		switch {
		case IsVideo(f.rel):
			if f.status == "present" {
				videos = append(videos, f)
			}
		case IsSubtitle(f.rel):
			if f.status == "present" {
				subs = append(subs, f)
			}
		default:
			if f.status == "present" {
				if _, _, ok := LocalArtworkKind(f.rel); ok {
					images = append(images, f)
				} else if isImage(f.rel) {
					images = append(images, f)
				}
			}
		}
	}

	for _, v := range videos {
		if IsExtra(v.rel) {
			continue
		}
		s.videosByDir[path.Dir(v.rel)] = append(s.videosByDir[path.Dir(v.rel)], v)
		if err := s.placeVideo(v); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", v.rel, err)
		}
	}
	if err := s.syncSubtitles(subs); err != nil {
		return nil, err
	}
	if err := s.syncLocalArtwork(images); err != nil {
		return nil, err
	}
	if err := s.prune(); err != nil {
		return nil, err
	}
	if err := s.collectWork(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.res, nil
}

func isImage(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

func loadFiles(ctx context.Context, db *sql.DB) ([]fileRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, rel_path, size_bytes, mod_time, status FROM indexed_files WHERE status != 'deleted'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []fileRow
	for rows.Next() {
		var f fileRow
		if err := rows.Scan(&f.id, &f.rel, &f.size, &f.mtime, &f.status); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type syncer struct {
	tx   *sql.Tx
	ctx  context.Context
	kind string
	res  *SyncResult

	movieByKey   map[string]string // match_key → movie id
	showByKey    map[string]string // match_key → show id
	seasonByKey  map[string]string // show id + "/" + season → season id
	episodeByKey map[string]string // show id + "/" + season + "/" + ep → episode id
	videosByDir  map[string][]fileRow

	manualFiles map[string]bool
	movieFile   map[string]string // file → movie id
	episodeFile map[string]string // file → episode id
}

func (s *syncer) loadExisting() error {
	s.manualFiles = map[string]bool{}
	s.movieFile = map[string]string{}
	s.episodeFile = map[string]string{}
	q := func(query string, fn func(*sql.Rows) error) error {
		rows, err := s.tx.QueryContext(s.ctx, query)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := q(`SELECT id, match_key FROM movies`, func(r *sql.Rows) error {
		var id, key string
		if err := r.Scan(&id, &key); err != nil {
			return err
		}
		s.movieByKey[key] = id
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT id, match_key FROM shows`, func(r *sql.Rows) error {
		var id, key string
		if err := r.Scan(&id, &key); err != nil {
			return err
		}
		s.showByKey[key] = id
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT id, show_id, season_number FROM seasons`, func(r *sql.Rows) error {
		var id, show string
		var n int
		if err := r.Scan(&id, &show, &n); err != nil {
			return err
		}
		s.seasonByKey[show+"/"+strconv.Itoa(n)] = id
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT id, show_id, season_number, episode_number FROM episodes`, func(r *sql.Rows) error {
		var id, show string
		var sn, en int
		if err := r.Scan(&id, &show, &sn, &en); err != nil {
			return err
		}
		s.episodeByKey[fmt.Sprintf("%s/%d/%d", show, sn, en)] = id
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT file_id, movie_id, manual FROM movie_files`, func(r *sql.Rows) error {
		var f, m string
		var manual int
		if err := r.Scan(&f, &m, &manual); err != nil {
			return err
		}
		s.movieFile[f] = m
		if manual == 1 {
			s.manualFiles[f] = true
		}
		return nil
	}); err != nil {
		return err
	}
	return q(`SELECT file_id, episode_id, manual FROM episode_files`, func(r *sql.Rows) error {
		var f, e string
		var manual int
		if err := r.Scan(&f, &e, &manual); err != nil {
			return err
		}
		s.episodeFile[f] = e
		if manual == 1 {
			s.manualFiles[f] = true
		}
		return nil
	})
}

// MovieKey is the identity of a movie read from its name.
func MovieKey(title string, year int) string {
	return NormalizeKey(title) + "|" + strconv.Itoa(year)
}

// ShowKey is the identity of a show read from its folder or name.
func ShowKey(title string, year int) string {
	k := NormalizeKey(title)
	if year > 0 {
		k += "|" + strconv.Itoa(year)
	}
	return k
}

func (s *syncer) placeVideo(v fileRow) error {
	if s.manualFiles[v.id] {
		return nil
	}
	asEpisode := false
	switch s.kind {
	case KindShows:
		asEpisode = true
	case KindMovies:
		asEpisode = false
	default:
		asEpisode = LooksLikeEpisode(v.rel)
	}
	if asEpisode {
		pe, ok := ParseEpisode(v.rel)
		if ok {
			return s.placeEpisode(v, pe)
		}
		if s.kind == KindShows {
			return nil // not recognisable as an episode; left uncatalogued
		}
	}
	return s.placeMovie(v, ParseMovie(v.rel))
}

func (s *syncer) placeMovie(v fileRow, pm ParsedMovie) error {
	key := MovieKey(pm.Title, pm.Year)
	movieID, ok := s.movieByKey[key]
	if !ok {
		movieID = NewID()
		t := now()
		var tmdb any
		if pm.TMDbID > 0 {
			tmdb = pm.TMDbID
		}
		var imdb any
		if pm.IMDbID != "" {
			imdb = pm.IMDbID
		}
		var year any
		if pm.Year > 0 {
			year = pm.Year
		}
		if _, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO movies (id, match_key, title, sort_title, year, folder, imdb_id, tmdb_id, added_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			movieID, key, pm.Title, SortTitle(pm.Title), year, nullStr(pm.Folder), imdb, tmdb, t, t); err != nil {
			return err
		}
		s.movieByKey[key] = movieID
		s.res.NewItems++
	}
	if _, ok := s.episodeFile[v.id]; ok {
		if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM episode_files WHERE file_id = ?`, v.id); err != nil {
			return err
		}
		delete(s.episodeFile, v.id)
	}
	if cur, ok := s.movieFile[v.id]; ok && cur == movieID {
		return nil
	}
	if _, err := s.tx.ExecContext(s.ctx,
		`INSERT INTO movie_files (file_id, movie_id, edition, position) VALUES (?, ?, ?, 0)
		 ON CONFLICT(file_id) DO UPDATE SET movie_id = excluded.movie_id, edition = excluded.edition`,
		v.id, movieID, nullStr(pm.Edition)); err != nil {
		return err
	}
	s.movieFile[v.id] = movieID
	return nil
}

func (s *syncer) placeEpisode(v fileRow, pe ParsedEpisode) error {
	key := ShowKey(pe.ShowTitle, pe.ShowYear)
	showID, ok := s.showByKey[key]
	t := now()
	if !ok {
		showID = NewID()
		category := "tv"
		if pe.Anime {
			category = "anime"
		}
		var year any
		if pe.ShowYear > 0 {
			year = pe.ShowYear
		}
		var tmdb any
		if pe.TMDbID > 0 {
			tmdb = pe.TMDbID
		}
		if _, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO shows (id, match_key, title, sort_title, year, folder, category, tmdb_id, added_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			showID, key, pe.ShowTitle, SortTitle(pe.ShowTitle), year, nullStr(pe.ShowFolder), category, tmdb, t, t); err != nil {
			return err
		}
		s.showByKey[key] = showID
		s.res.NewItems++
	}
	seasonKey := showID + "/" + strconv.Itoa(pe.Season)
	seasonID, ok := s.seasonByKey[seasonKey]
	if !ok {
		seasonID = NewID()
		title := fmt.Sprintf("Season %d", pe.Season)
		if pe.Season == 0 {
			title = "Specials"
		}
		order := pe.Season
		if pe.Season == 0 {
			order = 10000 // specials after the regular seasons
		}
		if _, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO seasons (id, show_id, season_number, title, sort_order) VALUES (?, ?, ?, ?, ?)`,
			seasonID, showID, pe.Season, title, order); err != nil {
			return err
		}
		s.seasonByKey[seasonKey] = seasonID
	}
	epKey := fmt.Sprintf("%s/%d/%d", showID, pe.Season, pe.Episode)
	epID, ok := s.episodeByKey[epKey]
	if !ok {
		epID = NewID()
		var end any
		if pe.EpisodeEnd > 0 {
			end = pe.EpisodeEnd
		}
		if _, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO episodes (id, show_id, season_id, season_number, episode_number, episode_end, title,
			                       sort_order, added_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			epID, showID, seasonID, pe.Season, pe.Episode, end, nullStr(pe.Title), pe.Episode, t, t); err != nil {
			return err
		}
		s.episodeByKey[epKey] = epID
		s.res.NewItems++
		// A new episode makes the show "recently updated".
		if _, err := s.tx.ExecContext(s.ctx, `UPDATE shows SET updated_at = ? WHERE id = ?`, t, showID); err != nil {
			return err
		}
	}
	if _, ok := s.movieFile[v.id]; ok {
		if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM movie_files WHERE file_id = ?`, v.id); err != nil {
			return err
		}
		delete(s.movieFile, v.id)
	}
	if cur, ok := s.episodeFile[v.id]; ok && cur == epID {
		return nil
	}
	if _, err := s.tx.ExecContext(s.ctx,
		`INSERT INTO episode_files (file_id, episode_id, edition, position) VALUES (?, ?, ?, 0)
		 ON CONFLICT(file_id) DO UPDATE SET episode_id = excluded.episode_id, edition = excluded.edition`,
		v.id, epID, nullStr(pe.Edition)); err != nil {
		return err
	}
	s.episodeFile[v.id] = epID
	return nil
}

// syncSubtitles attaches sidecar subtitle files to the video they belong to:
// the video in the same folder (or the parent of a Subs/ folder) whose name
// the subtitle's name starts with, or the folder's only video.
func (s *syncer) syncSubtitles(subs []fileRow) error {
	present := map[string]bool{}
	for _, sub := range subs {
		dir := path.Dir(sub.rel)
		candidates := s.videosByDir[dir]
		if len(candidates) == 0 {
			lb := strings.ToLower(path.Base(dir))
			if lb == "subs" || lb == "subtitles" || lb == "sub" {
				candidates = s.videosByDir[path.Dir(dir)]
			}
		}
		if len(candidates) == 0 {
			continue
		}
		subBase := strings.ToLower(strings.TrimSuffix(path.Base(sub.rel), path.Ext(sub.rel)))
		var best *fileRow
		bestLen := -1
		for i := range candidates {
			vb := strings.ToLower(strings.TrimSuffix(path.Base(candidates[i].rel), path.Ext(candidates[i].rel)))
			if strings.HasPrefix(subBase, vb) && len(vb) > bestLen {
				best, bestLen = &candidates[i], len(vb)
			}
		}
		if best == nil {
			if len(candidates) != 1 {
				continue
			}
			best = &candidates[0]
		}
		videoBase := strings.TrimSuffix(path.Base(best.rel), path.Ext(best.rel))
		info := ParseSubtitleName(videoBase, sub.rel)
		present[best.id+"\x00"+sub.rel] = true
		if _, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO external_subtitles (id, file_id, source, path, format, language, title, is_forced,
			                                 is_hearing_impaired, created_at)
			 VALUES (?, ?, 'sidecar', ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(file_id, path) DO NOTHING`,
			NewID(), best.id, sub.rel, SubtitleFormat(sub.rel), nullStr(info.Language), nullStr(info.Title),
			boolInt(info.Forced), boolInt(info.SDH), now()); err != nil {
			return err
		}
	}
	// Drop sidecar rows whose file is gone or now belongs to another video.
	rows, err := s.tx.QueryContext(s.ctx, `SELECT id, file_id, path FROM external_subtitles WHERE source = 'sidecar'`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id, fileID, p string
		if err := rows.Scan(&id, &fileID, &p); err != nil {
			_ = rows.Close()
			return err
		}
		if !present[fileID+"\x00"+p] {
			stale = append(stale, id)
		}
	}
	_ = rows.Close()
	for _, id := range stale {
		if _, err := s.tx.ExecContext(s.ctx, `DELETE FROM external_subtitles WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// syncLocalArtwork records artwork kept next to the media: poster.jpg and
// fanart.jpg in a movie or show folder, seasonNN-poster.jpg, and
// "<video name>.jpg"/"<video name>-thumb.jpg" for an episode thumbnail or
// movie poster. Uploaded artwork always wins over local files.
func (s *syncer) syncLocalArtwork(images []fileRow) error {
	movieFolders := map[string]string{}
	rows, err := s.tx.QueryContext(s.ctx, `SELECT id, folder FROM movies WHERE folder IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, folder string
		if err := rows.Scan(&id, &folder); err != nil {
			_ = rows.Close()
			return err
		}
		movieFolders[folder] = id
	}
	_ = rows.Close()
	showFolders := map[string]string{}
	rows, err = s.tx.QueryContext(s.ctx, `SELECT id, folder FROM shows WHERE folder IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, folder string
		if err := rows.Scan(&id, &folder); err != nil {
			_ = rows.Close()
			return err
		}
		showFolders[folder] = id
	}
	_ = rows.Close()

	// video base path (without extension) → file id, for "<video>.jpg".
	videoByBase := map[string]string{}
	for _, vids := range s.videosByDir {
		for _, v := range vids {
			videoByBase[strings.TrimSuffix(v.rel, path.Ext(v.rel))] = v.id
		}
	}

	seen := map[string]bool{}
	set := func(itemType, itemID, kind, rel string) error {
		k := itemType + "/" + itemID + "/" + kind
		if seen[k] {
			return nil
		}
		seen[k] = true
		_, err := s.tx.ExecContext(s.ctx,
			`INSERT INTO artwork (item_type, item_id, kind, source, path, updated_at)
			 VALUES (?, ?, ?, 'local', ?, ?)
			 ON CONFLICT(item_type, item_id, kind) DO UPDATE SET source = 'local', path = excluded.path,
			   updated_at = excluded.updated_at
			 WHERE artwork.source IN ('provider', 'generated') OR (artwork.source = 'local' AND artwork.path != excluded.path)`,
			itemType, itemID, kind, rel, now())
		return err
	}
	for _, img := range images {
		dir := path.Dir(img.rel)
		kind, season, named := LocalArtworkKind(img.rel)
		if named {
			if id, ok := movieFolders[dir]; ok && season < 0 {
				if err := set("movie", id, kind, img.rel); err != nil {
					return err
				}
				continue
			}
			if id, ok := showFolders[dir]; ok {
				if season >= 0 {
					if sid, ok := s.seasonByKey[id+"/"+strconv.Itoa(season)]; ok {
						if err := set("season", sid, "poster", img.rel); err != nil {
							return err
						}
					}
					continue
				}
				if err := set("show", id, kind, img.rel); err != nil {
					return err
				}
				continue
			}
			// poster.jpg inside a season folder.
			if id, ok := showFolders[path.Dir(dir)]; ok && kind == "poster" {
				if m := reSeasonDir.FindStringSubmatch(path.Base(dir)); m != nil {
					if sid, ok := s.seasonByKey[id+"/"+strconv.Itoa(atoi(m[1]))]; ok {
						if err := set("season", sid, "poster", img.rel); err != nil {
							return err
						}
					}
				}
			}
			continue
		}
		// "<video>.jpg" or "<video>-thumb.jpg" / "<video>-poster.jpg"
		base := strings.TrimSuffix(img.rel, path.Ext(img.rel))
		suffixKind := ""
		for _, suf := range []string{"-thumb", "-poster", "-fanart"} {
			if strings.HasSuffix(strings.ToLower(base), suf) {
				suffixKind = suf
				base = base[:len(base)-len(suf)]
				break
			}
		}
		fid, ok := videoByBase[base]
		if !ok {
			continue
		}
		if mid, ok := s.movieFile[fid]; ok {
			k := "poster"
			if suffixKind == "-fanart" {
				k = "backdrop"
			}
			if err := set("movie", mid, k, img.rel); err != nil {
				return err
			}
		} else if eid, ok := s.episodeFile[fid]; ok {
			if err := set("episode", eid, "thumb", img.rel); err != nil {
				return err
			}
		}
	}
	// Forget local artwork whose file is gone.
	present := map[string]bool{}
	for _, img := range images {
		present[img.rel] = true
	}
	rows, err = s.tx.QueryContext(s.ctx, `SELECT item_type, item_id, kind, path FROM artwork WHERE source = 'local'`)
	if err != nil {
		return err
	}
	type key struct{ t, id, k string }
	var stale []key
	for rows.Next() {
		var k key
		var p string
		if err := rows.Scan(&k.t, &k.id, &k.k, &p); err != nil {
			_ = rows.Close()
			return err
		}
		if !present[p] {
			stale = append(stale, k)
		}
	}
	_ = rows.Close()
	for _, k := range stale {
		if _, err := s.tx.ExecContext(s.ctx,
			`DELETE FROM artwork WHERE item_type = ? AND item_id = ? AND kind = ? AND source = 'local'`,
			k.t, k.id, k.k); err != nil {
			return err
		}
	}
	return nil
}

// prune removes catalog items no file belongs to any more. Items whose files
// are only missing (an unplugged disk, a file mid-move) keep their rows, and
// with them their metadata and everyone's watch state.
func (s *syncer) prune() error {
	stmts := []string{
		`DELETE FROM movie_files WHERE file_id IN (SELECT id FROM indexed_files WHERE status = 'deleted')`,
		`DELETE FROM episode_files WHERE file_id IN (SELECT id FROM indexed_files WHERE status = 'deleted')`,
		`DELETE FROM movies WHERE id NOT IN (SELECT movie_id FROM movie_files)`,
		`DELETE FROM episodes WHERE id NOT IN (SELECT episode_id FROM episode_files)`,
		`DELETE FROM seasons WHERE id NOT IN (SELECT season_id FROM episodes)`,
		`DELETE FROM shows WHERE id NOT IN (SELECT show_id FROM episodes)`,
		`DELETE FROM credits WHERE item_type = 'movie' AND item_id NOT IN (SELECT id FROM movies)`,
		`DELETE FROM credits WHERE item_type = 'show' AND item_id NOT IN (SELECT id FROM shows)`,
		`DELETE FROM artwork WHERE item_type = 'movie' AND item_id NOT IN (SELECT id FROM movies)`,
		`DELETE FROM artwork WHERE item_type = 'show' AND item_id NOT IN (SELECT id FROM shows)`,
		`DELETE FROM artwork WHERE item_type = 'season' AND item_id NOT IN (SELECT id FROM seasons)`,
		`DELETE FROM artwork WHERE item_type = 'episode' AND item_id NOT IN (SELECT id FROM episodes)`,
	}
	for _, q := range stmts {
		if _, err := s.tx.ExecContext(s.ctx, q); err != nil {
			return fmt.Errorf("prune: %w", err)
		}
	}
	return nil
}

func (s *syncer) collectWork() error {
	for _, q := range []struct{ t, sql string }{
		{"movie", `SELECT id FROM movies WHERE match_status = 'unmatched' ORDER BY added_at`},
		{"show", `SELECT id FROM shows WHERE match_status = 'unmatched' ORDER BY added_at`},
	} {
		rows, err := s.tx.QueryContext(s.ctx, q.sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			s.res.NeedMetadata = append(s.res.NeedMetadata, ItemRef{Type: q.t, ID: id})
		}
		_ = rows.Close()
	}
	rows, err := s.tx.QueryContext(s.ctx, `
		SELECT f.id, f.rel_path FROM indexed_files f
		WHERE f.status = 'present'
		  AND (f.id IN (SELECT file_id FROM movie_files) OR f.id IN (SELECT file_id FROM episode_files))
		  AND NOT EXISTS (SELECT 1 FROM video_files v WHERE v.file_id = f.id
		                  AND v.probe_size = f.size_bytes AND v.probe_mtime = f.mod_time)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var fr FileRef
		if err := rows.Scan(&fr.ID, &fr.RelPath); err != nil {
			_ = rows.Close()
			return err
		}
		s.res.NeedProbe = append(s.res.NeedProbe, fr)
	}
	_ = rows.Close()
	sort.Slice(s.res.NeedProbe, func(i, j int) bool { return s.res.NeedProbe[i].RelPath < s.res.NeedProbe[j].RelPath })
	for _, c := range []struct {
		dst *int
		sql string
	}{
		{&s.res.Movies, `SELECT COUNT(*) FROM movies`},
		{&s.res.Shows, `SELECT COUNT(*) FROM shows`},
		{&s.res.Episodes, `SELECT COUNT(*) FROM episodes`},
	} {
		if err := s.tx.QueryRowContext(s.ctx, c.sql).Scan(c.dst); err != nil {
			return err
		}
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
