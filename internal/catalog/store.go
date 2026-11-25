package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
	"github.com/Jishnu-Prasad888/popcorn/internal/stream"
)

// ErrNotFound means no catalog item has the id.
var ErrNotFound = errors.New("not found")

// Store reads and edits one library's catalog.
type Store struct {
	db    *sql.DB
	libID string
}

// NewStore returns a Store over an open library database.
func NewStore(db *sql.DB, libraryID string) *Store { return &Store{db: db, libID: libraryID} }

// DB returns the underlying database.
func (s *Store) DB() *sql.DB { return s.db }

func jsonStrings(s sql.NullString) []string {
	out := []string{}
	if s.Valid {
		_ = json.Unmarshal([]byte(s.String), &out)
	}
	return out
}

type whereBuilder struct {
	conds []string
	args  []any
}

func (w *whereBuilder) add(cond string, args ...any) {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
}

func (w *whereBuilder) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " AND " + strings.Join(w.conds, " AND ")
}

// personCond matches an item credited to a person (id or name) in a role.
func personCond(itemType, alias, role string) string {
	roleCond := ""
	if role != "" {
		roleCond = " AND c.role = '" + role + "'"
	}
	return `EXISTS (SELECT 1 FROM credits c JOIN people p ON p.id = c.person_id
		WHERE c.item_type = '` + itemType + `' AND c.item_id = ` + alias + `.id` + roleCond + `
		AND (p.id = ? OR p.name = ? COLLATE NOCASE))`
}

func languageArgs(lang string) (short, long string) {
	long = probe.NormalizeLanguage(lang)
	if long == "" {
		long = strings.ToLower(lang)
	}
	short = probe.ShortLanguage(long)
	return short, long
}

func (f Filter) itemConds(itemType, alias string) *whereBuilder {
	w := &whereBuilder{}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		w.add("("+alias+".title LIKE ? ESCAPE '\\' OR "+alias+".original_title LIKE ? ESCAPE '\\')", like, like)
	}
	if f.Genre != "" {
		w.add(`EXISTS (SELECT 1 FROM json_each(`+alias+`.genres) WHERE value = ? COLLATE NOCASE)`, f.Genre)
	}
	if f.YearFrom > 0 {
		w.add(alias+".year >= ?", f.YearFrom)
	}
	if f.YearTo > 0 {
		w.add(alias+".year <= ?", f.YearTo)
	}
	if f.MinRating > 0 {
		w.add("COALESCE("+alias+".imdb_rating, "+alias+".rating, 0) >= ?", f.MinRating)
	}
	if f.Studio != "" {
		w.add(`EXISTS (SELECT 1 FROM json_each(`+alias+`.studios) WHERE value = ? COLLATE NOCASE)`, f.Studio)
	}
	if f.Director != "" {
		w.add(personCond(itemType, alias, "director"), f.Director, f.Director)
	}
	if f.Actor != "" {
		w.add(personCond(itemType, alias, "cast"), f.Actor, f.Actor)
	}
	if f.Person != "" {
		w.add(personCond(itemType, alias, ""), f.Person, f.Person)
	}
	if f.Unmatched {
		w.add(alias + ".match_status IN ('unmatched', 'failed')")
	}
	return w
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Movies lists the library's movies matching f.
func (s *Store) Movies(ctx context.Context, f Filter) ([]Card, error) {
	w := f.itemConds("movie", "m")
	having := ""
	var havingArgs []any
	if f.Language != "" {
		short, long := languageArgs(f.Language)
		w.add(`(m.original_language = ? OR EXISTS (SELECT 1 FROM movie_files lmf JOIN media_streams ls
			ON ls.file_id = lmf.file_id WHERE lmf.movie_id = m.id AND ls.kind = 'audio' AND ls.language = ?))`,
			short, long)
	}
	if lo, hi, ok := resolutionRange(f.Resolution); ok {
		having = " HAVING MAX(COALESCE(v.height, 0)) BETWEEN ? AND ?"
		havingArgs = append(havingArgs, lo, hi)
	}
	q := `SELECT m.id, m.title, COALESCE(m.original_title, ''), m.sort_title, COALESCE(m.year, 0),
			COALESCE(m.release_date, ''), COALESCE(m.rating, 0), COALESCE(m.imdb_rating, 0),
			COALESCE(m.runtime_mins, 0), m.genres, COALESCE(m.content_rating, ''), COALESCE(m.overview, ''),
			COALESCE(m.tagline, ''), m.added_at, m.updated_at, m.artwork_version, m.match_status,
			(SELECT GROUP_CONCAT(kind) FROM artwork a WHERE a.item_type = 'movie' AND a.item_id = m.id),
			MAX(COALESCE(v.height, 0)), SUM(f.size_bytes), MAX(COALESCE(v.duration_secs, 0)),
			COUNT(f.id), SUM(f.status = 'present')
		FROM movies m
		JOIN movie_files mf ON mf.movie_id = m.id
		JOIN indexed_files f ON f.id = mf.file_id AND f.status != 'deleted'
		LEFT JOIN video_files v ON v.file_id = f.id
		WHERE 1 = 1` + w.sql() + ` GROUP BY m.id` + having
	rows, err := s.db.QueryContext(ctx, q, append(w.args, havingArgs...)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Card
	for rows.Next() {
		c := Card{Type: "movie", LibraryID: s.libID}
		var genres string
		var arts sql.NullString
		var artV, present int
		if err := rows.Scan(&c.ID, &c.Title, &c.OriginalTitle, &c.SortTitle, &c.Year, &c.ReleaseDate,
			&c.Rating, &c.IMDbRating, &c.RuntimeMins, &genres, &c.ContentRating, &c.Overview, &c.Tagline,
			&c.AddedAt, &c.UpdatedAt, &artV, &c.MatchStatus, &arts, &c.Height, &c.SizeBytes,
			&c.DurationSecs, &c.FileCount, &present); err != nil {
			return nil, err
		}
		c.Genres = jsonStrings(sql.NullString{String: genres, Valid: true})
		c.Art = artFrom(arts.String, artV)
		c.Resolution = Resolution(c.Height)
		c.Available = present > 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// Shows lists the library's shows matching f.
func (s *Store) Shows(ctx context.Context, f Filter) ([]Card, error) {
	w := f.itemConds("show", "sh")
	if f.Category != "" {
		w.add("sh.category = ?", f.Category)
	}
	if f.Language != "" {
		short, long := languageArgs(f.Language)
		w.add(`(sh.original_language = ? OR EXISTS (SELECT 1 FROM episodes le JOIN episode_files lef
			ON lef.episode_id = le.id JOIN media_streams ls ON ls.file_id = lef.file_id
			WHERE le.show_id = sh.id AND ls.kind = 'audio' AND ls.language = ?))`, short, long)
	}
	having := ""
	var havingArgs []any
	if lo, hi, ok := resolutionRange(f.Resolution); ok {
		having = " HAVING MAX(COALESCE(v.height, 0)) BETWEEN ? AND ?"
		havingArgs = append(havingArgs, lo, hi)
	}
	q := `SELECT sh.id, sh.title, COALESCE(sh.original_title, ''), sh.sort_title, COALESCE(sh.year, 0),
			COALESCE(sh.first_air_date, ''), COALESCE(sh.rating, 0), COALESCE(sh.imdb_rating, 0), sh.genres,
			COALESCE(sh.content_rating, ''), COALESCE(sh.overview, ''), COALESCE(sh.tagline, ''), sh.category,
			sh.added_at, sh.updated_at, sh.artwork_version, sh.match_status,
			(SELECT GROUP_CONCAT(kind) FROM artwork a WHERE a.item_type = 'show' AND a.item_id = sh.id),
			MAX(COALESCE(v.height, 0)), SUM(f.size_bytes),
			COUNT(DISTINCT e.season_id), COUNT(DISTINCT e.id), SUM(f.status = 'present')
		FROM shows sh
		JOIN episodes e ON e.show_id = sh.id
		JOIN episode_files ef ON ef.episode_id = e.id
		JOIN indexed_files f ON f.id = ef.file_id AND f.status != 'deleted'
		LEFT JOIN video_files v ON v.file_id = f.id
		WHERE 1 = 1` + w.sql() + ` GROUP BY sh.id` + having
	rows, err := s.db.QueryContext(ctx, q, append(w.args, havingArgs...)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Card
	for rows.Next() {
		c := Card{Type: "show", LibraryID: s.libID}
		var genres string
		var arts sql.NullString
		var artV, present int
		if err := rows.Scan(&c.ID, &c.Title, &c.OriginalTitle, &c.SortTitle, &c.Year, &c.ReleaseDate,
			&c.Rating, &c.IMDbRating, &genres, &c.ContentRating, &c.Overview, &c.Tagline, &c.Category,
			&c.AddedAt, &c.UpdatedAt, &artV, &c.MatchStatus, &arts, &c.Height, &c.SizeBytes,
			&c.SeasonCount, &c.EpisodeCount, &present); err != nil {
			return nil, err
		}
		c.Genres = jsonStrings(sql.NullString{String: genres, Valid: true})
		c.Art = artFrom(arts.String, artV)
		c.Resolution = Resolution(c.Height)
		c.Available = present > 0
		out = append(out, c)
	}
	return out, rows.Err()
}

const episodeCardSelect = `SELECT e.id, e.show_id, sh.title, e.season_id, e.season_number, e.episode_number,
		COALESCE(e.episode_end, 0), COALESCE(e.title, ''), COALESCE(e.overview, ''), COALESCE(e.air_date, ''),
		COALESCE(e.runtime_mins, 0), COALESCE(e.rating, 0), e.added_at, e.updated_at, e.artwork_version,
		(SELECT GROUP_CONCAT(kind) FROM artwork a WHERE a.item_type = 'episode' AND a.item_id = e.id),
		sh.artwork_version,
		(SELECT GROUP_CONCAT(kind) FROM artwork a WHERE a.item_type = 'show' AND a.item_id = sh.id),
		sh.genres, COALESCE(sh.content_rating, ''), sh.category, e.sort_order,
		(SELECT MAX(COALESCE(v.height, 0)) FROM episode_files ef2 LEFT JOIN video_files v ON v.file_id = ef2.file_id
		  WHERE ef2.episode_id = e.id),
		(SELECT MAX(COALESCE(v.duration_secs, 0)) FROM episode_files ef2 LEFT JOIN video_files v ON v.file_id = ef2.file_id
		  WHERE ef2.episode_id = e.id),
		(SELECT COUNT(*) FROM episode_files ef2 JOIN indexed_files f ON f.id = ef2.file_id
		  WHERE ef2.episode_id = e.id AND f.status = 'present'),
		(SELECT COALESCE(SUM(f.size_bytes), 0) FROM episode_files ef2 JOIN indexed_files f ON f.id = ef2.file_id
		  WHERE ef2.episode_id = e.id AND f.status != 'deleted')
	FROM episodes e JOIN shows sh ON sh.id = e.show_id
	JOIN seasons se ON se.id = e.season_id`

func (s *Store) scanEpisodeCards(rows *sql.Rows) ([]Card, []int, error) {
	defer func() { _ = rows.Close() }()
	var out []Card
	var orders []int
	for rows.Next() {
		c := Card{Type: "episode", LibraryID: s.libID}
		var arts, showArts sql.NullString
		var artV, showArtV, order, present int
		var genres string
		if err := rows.Scan(&c.ID, &c.ShowID, &c.ShowTitle, &c.SeasonID, &c.SeasonNumber, &c.EpisodeNumber,
			&c.EpisodeEnd, &c.Title, &c.Overview, &c.AirDate, &c.RuntimeMins, &c.Rating, &c.AddedAt,
			&c.UpdatedAt, &artV, &arts, &showArtV, &showArts, &genres, &c.ContentRating, &c.Category, &order,
			&c.Height, &c.DurationSecs, &present, &c.SizeBytes); err != nil {
			return nil, nil, err
		}
		c.Art = artFrom(arts.String, artV)
		sa := artFrom(showArts.String, showArtV)
		c.ShowArt = &sa
		c.Genres = jsonStrings(sql.NullString{String: genres, Valid: true})
		c.Resolution = Resolution(c.Height)
		c.Available = present > 0
		c.ReleaseDate = c.AirDate
		if c.Title == "" {
			c.Title = episodeLabel(c.SeasonNumber, c.EpisodeNumber)
		}
		out = append(out, c)
		orders = append(orders, order)
	}
	return out, orders, rows.Err()
}

func episodeLabel(season, ep int) string {
	return "Episode " + itoa(ep)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Episodes lists episodes matching the query (by title) or every episode of
// a show/season when showID/seasonID are given. Ordered by season then
// episode order.
func (s *Store) Episodes(ctx context.Context, showID, seasonID, query string) ([]Card, error) {
	w := &whereBuilder{}
	if showID != "" {
		w.add("e.show_id = ?", showID)
	}
	if seasonID != "" {
		w.add("e.season_id = ?", seasonID)
	}
	if query != "" {
		like := "%" + escapeLike(query) + "%"
		w.add("(e.title LIKE ? ESCAPE '\\' OR e.overview LIKE ? ESCAPE '\\')", like, like)
	}
	rows, err := s.db.QueryContext(ctx, episodeCardSelect+` WHERE 1 = 1`+w.sql()+
		` ORDER BY se.sort_order, se.season_number, e.sort_order, e.episode_number`, w.args...)
	if err != nil {
		return nil, err
	}
	cards, _, err := s.scanEpisodeCards(rows)
	return cards, err
}

// RecentEpisodes lists the newest episodes added.
func (s *Store) RecentEpisodes(ctx context.Context, limit int) ([]Card, error) {
	rows, err := s.db.QueryContext(ctx, episodeCardSelect+` ORDER BY e.added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	cards, _, err := s.scanEpisodeCards(rows)
	return cards, err
}

// EpisodeCard returns one episode's card.
func (s *Store) EpisodeCard(ctx context.Context, id string) (*Card, error) {
	rows, err := s.db.QueryContext(ctx, episodeCardSelect+` WHERE e.id = ?`, id)
	if err != nil {
		return nil, err
	}
	cards, _, err := s.scanEpisodeCards(rows)
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, ErrNotFound
	}
	return &cards[0], nil
}

// MovieCard returns one movie's card.
func (s *Store) MovieCard(ctx context.Context, id string) (*Card, error) {
	cards, err := s.Movies(ctx, Filter{})
	if err != nil {
		return nil, err
	}
	for i := range cards {
		if cards[i].ID == id {
			return &cards[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) credits(ctx context.Context, itemType, itemID string) ([]Credit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, c.role, COALESCE(c.character, ''), COALESCE(p.profile_url, '')
		 FROM credits c JOIN people p ON p.id = c.person_id
		 WHERE c.item_type = ? AND c.item_id = ? ORDER BY c.ord`, itemType, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Credit{}
	for rows.Next() {
		var c Credit
		if err := rows.Scan(&c.PersonID, &c.Name, &c.Role, &c.Character, &c.ProfileURL); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Files returns the video files attached to a movie or episode, with their
// streams and subtitles, best version first.
func (s *Store) Files(ctx context.Context, itemType, itemID string) ([]FileInfo, error) {
	table, col := "movie_files", "movie_id"
	if itemType == "episode" {
		table, col = "episode_files", "episode_id"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.rel_path, COALESCE(x.edition, ''), f.size_bytes, f.mod_time,
			f.status, x.manual, COALESCE(v.container, ''), COALESCE(v.duration_secs, 0), COALESCE(v.bitrate, 0),
			COALESCE(v.width, 0), COALESCE(v.height, 0), COALESCE(v.video_codec, ''), COALESCE(v.video_profile, ''),
			COALESCE(v.pix_fmt, ''), COALESCE(v.frame_rate, 0), COALESCE(v.hdr, ''), COALESCE(v.probe_error, ''),
			v.probed_at IS NOT NULL, COALESCE(v.chapters, '[]')
		FROM `+table+` x JOIN indexed_files f ON f.id = x.file_id
		LEFT JOIN video_files v ON v.file_id = f.id
		WHERE x.`+col+` = ? AND f.status != 'deleted'
		ORDER BY f.status = 'present' DESC, x.position, COALESCE(v.height, 0) DESC, f.size_bytes DESC`, itemID)
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	for rows.Next() {
		var fi FileInfo
		var manual int
		var chapters string
		if err := rows.Scan(&fi.ID, &fi.RelPath, &fi.Edition, &fi.SizeBytes, &fi.ModTime, &fi.Status, &manual,
			&fi.Container, &fi.DurationSecs, &fi.Bitrate, &fi.Width, &fi.Height, &fi.VideoCodec,
			&fi.VideoProfile, &fi.PixFmt, &fi.FrameRate, &fi.HDR, &fi.ProbeError, &fi.Probed, &chapters); err != nil {
			_ = rows.Close()
			return nil, err
		}
		fi.Manual = manual == 1
		fi.Name = path.Base(fi.RelPath)
		fi.Resolution = Resolution(fi.Height)
		fi.Chapters = []Chapter{}
		_ = json.Unmarshal([]byte(chapters), &fi.Chapters)
		files = append(files, fi)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range files {
		st, err := s.Streams(ctx, files[i].ID)
		if err != nil {
			return nil, err
		}
		files[i].Streams = st
		subs, err := s.Subtitles(ctx, files[i].ID)
		if err != nil {
			return nil, err
		}
		files[i].Subtitles = subs
	}
	if files == nil {
		files = []FileInfo{}
	}
	return files, nil
}

// Streams lists a file's probed streams.
func (s *Store) Streams(ctx context.Context, fileID string) ([]StreamInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT stream_index, kind, type_index, COALESCE(codec, ''),
			COALESCE(profile, ''), COALESCE(language, ''), COALESCE(title, ''), COALESCE(channels, 0),
			COALESCE(channel_layout, ''), COALESCE(sample_rate, 0), COALESCE(bitrate, 0), COALESCE(width, 0),
			COALESCE(height, 0), is_default, is_forced, is_hearing_impaired
		FROM media_streams WHERE file_id = ? AND kind IN ('video', 'audio', 'subtitle')
		ORDER BY stream_index`, fileID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []StreamInfo{}
	for rows.Next() {
		var st StreamInfo
		var def, forced, sdh int
		if err := rows.Scan(&st.Index, &st.Kind, &st.TypeIndex, &st.Codec, &st.Profile, &st.Language, &st.Title,
			&st.Channels, &st.ChannelLayout, &st.SampleRate, &st.Bitrate, &st.Width, &st.Height,
			&def, &forced, &sdh); err != nil {
			return nil, err
		}
		st.IsDefault, st.IsForced, st.IsHearingImpaired = def == 1, forced == 1, sdh == 1
		if st.Kind != "video" {
			st.LanguageName = probe.LanguageName(st.Language)
		}
		if st.Kind == "subtitle" {
			st.ImageBased = stream.ImageSubtitle(st.Codec)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Subtitles lists a file's external subtitles.
func (s *Store) Subtitles(ctx context.Context, fileID string) ([]ExternalSubtitle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, path, format, COALESCE(language, ''), COALESCE(title, ''),
			is_forced, is_hearing_impaired FROM external_subtitles WHERE file_id = ?
		ORDER BY language, is_forced, path`, fileID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ExternalSubtitle{}
	for rows.Next() {
		var x ExternalSubtitle
		var forced, sdh int
		if err := rows.Scan(&x.ID, &x.Source, &x.Path, &x.Format, &x.Language, &x.Title, &forced, &sdh); err != nil {
			return nil, err
		}
		x.IsForced, x.IsHearingImpaired = forced == 1, sdh == 1
		x.LanguageName = probe.LanguageName(x.Language)
		out = append(out, x)
	}
	return out, rows.Err()
}

// Subtitle returns one external subtitle.
func (s *Store) Subtitle(ctx context.Context, id string) (*ExternalSubtitle, string, error) {
	var x ExternalSubtitle
	var fileID string
	var forced, sdh int
	err := s.db.QueryRowContext(ctx, `SELECT id, file_id, source, path, format, COALESCE(language, ''),
			COALESCE(title, ''), is_forced, is_hearing_impaired FROM external_subtitles WHERE id = ?`, id).
		Scan(&x.ID, &fileID, &x.Source, &x.Path, &x.Format, &x.Language, &x.Title, &forced, &sdh)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	x.IsForced, x.IsHearingImpaired = forced == 1, sdh == 1
	return &x, fileID, nil
}

// Movie returns a movie's detail.
func (s *Store) Movie(ctx context.Context, id string) (*Movie, error) {
	card, err := s.MovieCard(ctx, id)
	if err != nil {
		return nil, err
	}
	m := &Movie{Card: *card}
	var langs, countries, studios, locked string
	var origLang, imdb, metaAt, folder sql.NullString
	var tmdb, votes, imdbVotes sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT original_language, languages, countries, studios, imdb_id, tmdb_id,
			vote_count, imdb_votes, locked_fields, metadata_at, folder FROM movies WHERE id = ?`, id).
		Scan(&origLang, &langs, &countries, &studios, &imdb, &tmdb, &votes, &imdbVotes, &locked, &metaAt, &folder)
	if err != nil {
		return nil, err
	}
	m.OriginalLanguage, m.IMDbID, m.MetadataAt, m.Folder = origLang.String, imdb.String, metaAt.String, folder.String
	m.TMDbID, m.VoteCount, m.IMDbVotes = int(tmdb.Int64), int(votes.Int64), int(imdbVotes.Int64)
	m.Languages = jsonStrings(sql.NullString{String: langs, Valid: true})
	m.Countries = jsonStrings(sql.NullString{String: countries, Valid: true})
	m.Studios = jsonStrings(sql.NullString{String: studios, Valid: true})
	m.LockedFields = jsonStrings(sql.NullString{String: locked, Valid: true})
	if m.Credits, err = s.credits(ctx, "movie", id); err != nil {
		return nil, err
	}
	if m.Files, err = s.Files(ctx, "movie", id); err != nil {
		return nil, err
	}
	return m, nil
}

// ShowCard returns one show's card.
func (s *Store) ShowCard(ctx context.Context, id string) (*Card, error) {
	cards, err := s.Shows(ctx, Filter{})
	if err != nil {
		return nil, err
	}
	for i := range cards {
		if cards[i].ID == id {
			return &cards[i], nil
		}
	}
	return nil, ErrNotFound
}

// Show returns a show's detail with its seasons.
func (s *Store) Show(ctx context.Context, id string) (*Show, error) {
	card, err := s.ShowCard(ctx, id)
	if err != nil {
		return nil, err
	}
	sh := &Show{Card: *card}
	var langs, countries, studios, locked string
	var first, last, status, origLang, imdb, metaAt, folder sql.NullString
	var tmdb, votes, imdbVotes sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT first_air_date, last_air_date, status, original_language, languages,
			countries, studios, imdb_id, tmdb_id, vote_count, imdb_votes, locked_fields, metadata_at, folder
		FROM shows WHERE id = ?`, id).
		Scan(&first, &last, &status, &origLang, &langs, &countries, &studios, &imdb, &tmdb, &votes, &imdbVotes,
			&locked, &metaAt, &folder)
	if err != nil {
		return nil, err
	}
	sh.FirstAirDate, sh.LastAirDate, sh.Status = first.String, last.String, status.String
	sh.OriginalLanguage, sh.IMDbID, sh.MetadataAt, sh.Folder = origLang.String, imdb.String, metaAt.String, folder.String
	sh.TMDbID, sh.VoteCount, sh.IMDbVotes = int(tmdb.Int64), int(votes.Int64), int(imdbVotes.Int64)
	sh.Languages = jsonStrings(sql.NullString{String: langs, Valid: true})
	sh.Countries = jsonStrings(sql.NullString{String: countries, Valid: true})
	sh.Studios = jsonStrings(sql.NullString{String: studios, Valid: true})
	sh.LockedFields = jsonStrings(sql.NullString{String: locked, Valid: true})
	if sh.Credits, err = s.credits(ctx, "show", id); err != nil {
		return nil, err
	}
	if sh.Seasons, err = s.Seasons(ctx, id); err != nil {
		return nil, err
	}
	return sh, nil
}

// Seasons lists a show's seasons in display order.
func (s *Store) Seasons(ctx context.Context, showID string) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT se.id, se.season_number, COALESCE(se.title, ''),
			COALESCE(se.overview, ''), COALESCE(se.air_date, ''), COALESCE(se.episode_total, 0), se.sort_order,
			se.artwork_version, se.locked_fields,
			(SELECT GROUP_CONCAT(kind) FROM artwork a WHERE a.item_type = 'season' AND a.item_id = se.id),
			(SELECT COUNT(*) FROM episodes e WHERE e.season_id = se.id)
		FROM seasons se WHERE se.show_id = ? ORDER BY se.sort_order, se.season_number`, showID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Season{}
	for rows.Next() {
		se := Season{ShowID: showID, LibraryID: s.libID}
		var artV int
		var locked string
		var arts sql.NullString
		if err := rows.Scan(&se.ID, &se.Number, &se.Title, &se.Overview, &se.AirDate, &se.EpisodeTotal,
			&se.SortOrder, &artV, &locked, &arts, &se.EpisodeCount); err != nil {
			return nil, err
		}
		se.Art = artFrom(arts.String, artV)
		se.LockedFields = jsonStrings(sql.NullString{String: locked, Valid: true})
		out = append(out, se)
	}
	return out, rows.Err()
}

// Season returns one season.
func (s *Store) Season(ctx context.Context, id string) (*Season, error) {
	var showID string
	if err := s.db.QueryRowContext(ctx, `SELECT show_id FROM seasons WHERE id = ?`, id).Scan(&showID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	seasons, err := s.Seasons(ctx, showID)
	if err != nil {
		return nil, err
	}
	for i := range seasons {
		if seasons[i].ID == id {
			return &seasons[i], nil
		}
	}
	return nil, ErrNotFound
}

// Episode returns an episode's detail.
func (s *Store) Episode(ctx context.Context, id string) (*Episode, error) {
	card, err := s.EpisodeCard(ctx, id)
	if err != nil {
		return nil, err
	}
	e := &Episode{Card: *card}
	var is, ie, cs sql.NullFloat64
	var locked string
	err = s.db.QueryRowContext(ctx, `SELECT intro_start, intro_end, credits_start, sort_order, locked_fields
		FROM episodes WHERE id = ?`, id).Scan(&is, &ie, &cs, &e.SortOrder, &locked)
	if err != nil {
		return nil, err
	}
	if is.Valid {
		e.IntroStart = &is.Float64
	}
	if ie.Valid {
		e.IntroEnd = &ie.Float64
	}
	if cs.Valid {
		e.CreditsStart = &cs.Float64
	}
	e.LockedFields = jsonStrings(sql.NullString{String: locked, Valid: true})
	if e.Credits, err = s.credits(ctx, "episode", id); err != nil {
		return nil, err
	}
	// Episodes inherit the show's main cast for display.
	showCredits, err := s.credits(ctx, "show", e.ShowID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range e.Credits {
		seen[c.PersonID+c.Role] = true
	}
	for _, c := range showCredits {
		if !seen[c.PersonID+c.Role] && (c.Role == "cast" || c.Role == "creator") {
			e.Credits = append(e.Credits, c)
		}
	}
	if e.Files, err = s.Files(ctx, "episode", id); err != nil {
		return nil, err
	}
	return e, nil
}

// EpisodeOrder lists every episode id of a show in viewing order (season
// order, then episode order; specials last unless rearranged).
func (s *Store) EpisodeOrder(ctx context.Context, showID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id FROM episodes e JOIN seasons se ON se.id = e.season_id
		WHERE e.show_id = ? ORDER BY se.sort_order, se.season_number, e.sort_order, e.episode_number`, showID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AdjacentEpisodes returns the episodes before and after id in viewing
// order ("" at either end). Specials (season 0) are skipped when moving on
// from a regular episode.
func (s *Store) AdjacentEpisodes(ctx context.Context, showID, id string) (prev, next string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.season_number FROM episodes e JOIN seasons se ON se.id = e.season_id
		WHERE e.show_id = ? ORDER BY se.sort_order, se.season_number, e.sort_order, e.episode_number`, showID)
	if err != nil {
		return "", "", err
	}
	type ep struct {
		id     string
		season int
	}
	var list []ep
	for rows.Next() {
		var e ep
		if err := rows.Scan(&e.id, &e.season); err != nil {
			_ = rows.Close()
			return "", "", err
		}
		list = append(list, e)
	}
	_ = rows.Close()
	for i, e := range list {
		if e.id != id {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if e.season == 0 || list[j].season != 0 {
				prev = list[j].id
				break
			}
		}
		for j := i + 1; j < len(list); j++ {
			if e.season == 0 || list[j].season != 0 {
				next = list[j].id
				break
			}
		}
		break
	}
	return prev, next, nil
}

// ItemForFile returns which movie or episode a file belongs to.
func (s *Store) ItemForFile(ctx context.Context, fileID string) (itemType, itemID string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT 'movie', movie_id FROM movie_files WHERE file_id = ?
		UNION ALL SELECT 'episode', episode_id FROM episode_files WHERE file_id = ? LIMIT 1`, fileID, fileID).
		Scan(&itemType, &itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return itemType, itemID, err
}

// FileOf returns a file's relative path and status, and checks it belongs to
// a movie or episode.
func (s *Store) FileOf(ctx context.Context, fileID string) (rel, status string, size int64, mtime string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT rel_path, status, size_bytes, mod_time FROM indexed_files WHERE id = ?`, fileID).
		Scan(&rel, &status, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, "", ErrNotFound
	}
	return rel, status, size, mtime, err
}

// Person returns a person and every item in this library crediting them.
func (s *Store) Person(ctx context.Context, id string) (*Person, error) {
	p := &Person{ID: id, LibraryID: s.libID, Roles: map[string][]string{}, Items: []Card{}}
	var tmdb sql.NullInt64
	var profile sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT name, tmdb_id, profile_url FROM people WHERE id = ?`, id).
		Scan(&p.Name, &tmdb, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.TMDbID, p.ProfileURL = int(tmdb.Int64), profile.String
	movies, err := s.Movies(ctx, Filter{Person: id})
	if err != nil {
		return nil, err
	}
	shows, err := s.Shows(ctx, Filter{Person: id})
	if err != nil {
		return nil, err
	}
	// Guest roles on episodes count for the show.
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.show_id FROM credits c JOIN episodes e ON e.id = c.item_id
		WHERE c.item_type = 'episode' AND c.person_id = ?`, id)
	if err != nil {
		return nil, err
	}
	guest := map[string]bool{}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err == nil {
			guest[sid] = true
		}
	}
	_ = rows.Close()
	have := map[string]bool{}
	for _, c := range shows {
		have[c.ID] = true
	}
	if len(guest) > 0 {
		all, err := s.Shows(ctx, Filter{})
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			if guest[c.ID] && !have[c.ID] {
				shows = append(shows, c)
			}
		}
	}
	p.Items = append(p.Items, movies...)
	p.Items = append(p.Items, shows...)
	sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].Year > p.Items[j].Year })
	rows, err = s.db.QueryContext(ctx, `SELECT item_type, item_id, role, COALESCE(character, '') FROM credits WHERE person_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t, itemID, role, ch string
		if err := rows.Scan(&t, &itemID, &role, &ch); err != nil {
			return nil, err
		}
		label := role
		if ch != "" {
			label = "as " + ch
		}
		if t == "episode" {
			continue
		}
		p.Roles[itemID] = append(p.Roles[itemID], label)
	}
	return p, rows.Err()
}

// PersonByTMDb finds a person by TMDB id in this library.
func (s *Store) PersonByTMDb(ctx context.Context, tmdbID int) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM people WHERE tmdb_id = ?`, tmdbID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// PersonByName finds a person by exact name in this library.
func (s *Store) PersonByName(ctx context.Context, name string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM people WHERE name = ? COLLATE NOCASE LIMIT 1`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// PersonHit is a person found by a search.
type PersonHit struct {
	ID         string `json:"id"`
	LibraryID  string `json:"library_id"`
	Name       string `json:"name"`
	ProfileURL string `json:"profile_url,omitempty"`
	Credits    int    `json:"credits"`
}

// SearchPeople finds cast and crew by name.
func (s *Store) SearchPeople(ctx context.Context, q string, limit int) ([]PersonHit, error) {
	like := "%" + escapeLike(q) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.name, COALESCE(p.profile_url, ''),
			(SELECT COUNT(*) FROM credits c WHERE c.person_id = p.id AND c.item_type != 'episode') AS n
		FROM people p WHERE p.name LIKE ? ESCAPE '\' ORDER BY n DESC, p.name LIMIT ?`, like, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PersonHit
	for rows.Next() {
		h := PersonHit{LibraryID: s.libID}
		if err := rows.Scan(&h.ID, &h.Name, &h.ProfileURL, &h.Credits); err != nil {
			return nil, err
		}
		if h.Credits > 0 {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

// Facets are the values a library's filters can offer.
type Facets struct {
	Genres    []string `json:"genres"`
	Years     []int    `json:"years"`
	Languages []string `json:"languages"`
	Studios   []string `json:"studios"`
}

// Facets lists genres, years, languages and studios present in the library.
func (s *Store) Facets(ctx context.Context, itemType string) (*Facets, error) {
	table := "movies"
	if itemType == "show" {
		table = "shows"
	}
	f := &Facets{Genres: []string{}, Years: []int{}, Languages: []string{}, Studios: []string{}}
	collect := func(q string, fn func(*sql.Rows) error) error {
		rows, err := s.db.QueryContext(ctx, q)
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
	str := func(dst *[]string) func(*sql.Rows) error {
		return func(r *sql.Rows) error {
			var v string
			if err := r.Scan(&v); err != nil {
				return err
			}
			if v != "" {
				*dst = append(*dst, v)
			}
			return nil
		}
	}
	if err := collect(`SELECT DISTINCT j.value FROM `+table+` t, json_each(t.genres) j ORDER BY j.value`, str(&f.Genres)); err != nil {
		return nil, err
	}
	if err := collect(`SELECT DISTINCT j.value FROM `+table+` t, json_each(t.studios) j ORDER BY j.value LIMIT 300`, str(&f.Studios)); err != nil {
		return nil, err
	}
	if err := collect(`SELECT DISTINCT year FROM `+table+` WHERE year IS NOT NULL ORDER BY year DESC`, func(r *sql.Rows) error {
		var y int
		if err := r.Scan(&y); err != nil {
			return err
		}
		f.Years = append(f.Years, y)
		return nil
	}); err != nil {
		return nil, err
	}
	langs := map[string]bool{}
	if err := collect(`SELECT DISTINCT original_language FROM `+table+` WHERE original_language IS NOT NULL`, func(r *sql.Rows) error {
		var l string
		if err := r.Scan(&l); err != nil {
			return err
		}
		if c := probe.NormalizeLanguage(l); c != "" {
			langs[c] = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT DISTINCT language FROM media_streams WHERE kind = 'audio' AND language IS NOT NULL`, func(r *sql.Rows) error {
		var l string
		if err := r.Scan(&l); err != nil {
			return err
		}
		langs[l] = true
		return nil
	}); err != nil {
		return nil, err
	}
	for l := range langs {
		f.Languages = append(f.Languages, l)
	}
	sort.Strings(f.Languages)
	return f, nil
}

// Stats summarises a library's catalog.
type Stats struct {
	Movies        int              `json:"movies"`
	Shows         int              `json:"shows"`
	Seasons       int              `json:"seasons"`
	Episodes      int              `json:"episodes"`
	VideoFiles    int              `json:"video_files"`
	MissingFiles  int              `json:"missing_files"`
	TotalBytes    int64            `json:"total_bytes"`
	MovieBytes    int64            `json:"movie_bytes"`
	EpisodeBytes  int64            `json:"episode_bytes"`
	TotalSecs     float64          `json:"total_secs"`
	Unmatched     int              `json:"unmatched"`
	Unprobed      int              `json:"unprobed"`
	Resolutions   map[string]int   `json:"resolutions"`
	VideoCodecs   map[string]int   `json:"video_codecs"`
	Containers    map[string]int   `json:"containers"`
	Subtitles     int              `json:"subtitles"`
}

// Stats computes the library's catalog statistics.
func (s *Store) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{Resolutions: map[string]int{}, VideoCodecs: map[string]int{}, Containers: map[string]int{}}
	one := func(dst any, q string) error { return s.db.QueryRowContext(ctx, q).Scan(dst) }
	for _, c := range []struct {
		dst any
		q   string
	}{
		{&st.Movies, `SELECT COUNT(*) FROM movies`},
		{&st.Shows, `SELECT COUNT(*) FROM shows`},
		{&st.Seasons, `SELECT COUNT(*) FROM seasons`},
		{&st.Episodes, `SELECT COUNT(*) FROM episodes`},
		{&st.Unmatched, `SELECT (SELECT COUNT(*) FROM movies WHERE match_status IN ('unmatched','failed')) +
			(SELECT COUNT(*) FROM shows WHERE match_status IN ('unmatched','failed'))`},
		{&st.Subtitles, `SELECT COUNT(*) FROM external_subtitles`},
		{&st.MovieBytes, `SELECT COALESCE(SUM(f.size_bytes), 0) FROM movie_files x JOIN indexed_files f ON f.id = x.file_id WHERE f.status = 'present'`},
		{&st.EpisodeBytes, `SELECT COALESCE(SUM(f.size_bytes), 0) FROM episode_files x JOIN indexed_files f ON f.id = x.file_id WHERE f.status = 'present'`},
	} {
		if err := one(c.dst, c.q); err != nil {
			return nil, err
		}
	}
	st.TotalBytes = st.MovieBytes + st.EpisodeBytes
	rows, err := s.db.QueryContext(ctx, `SELECT f.status, COALESCE(v.height, 0), COALESCE(v.video_codec, ''),
			COALESCE(v.container, ''), COALESCE(v.duration_secs, 0), v.probed_at IS NOT NULL
		FROM indexed_files f LEFT JOIN video_files v ON v.file_id = f.id
		WHERE f.status != 'deleted' AND (f.id IN (SELECT file_id FROM movie_files) OR f.id IN (SELECT file_id FROM episode_files))`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status, codec, container string
		var height int
		var dur float64
		var probed bool
		if err := rows.Scan(&status, &height, &codec, &container, &dur, &probed); err != nil {
			return nil, err
		}
		if status == "missing" {
			st.MissingFiles++
			continue
		}
		st.VideoFiles++
		st.TotalSecs += dur
		if !probed {
			st.Unprobed++
			continue
		}
		if r := Resolution(height); r != "" {
			st.Resolutions[r]++
		}
		if codec != "" {
			st.VideoCodecs[codec]++
		}
		if container != "" {
			st.Containers[container]++
		}
	}
	return st, rows.Err()
}

// Duplicate is a movie with more than one file, or several movies matched
// to the same title.
type Duplicate struct {
	Kind   string `json:"kind"` // versions or same_title
	Movies []Card `json:"movies"`
}

// Duplicates finds movies stored more than once.
func (s *Store) Duplicates(ctx context.Context) ([]Duplicate, error) {
	cards, err := s.Movies(ctx, Filter{})
	if err != nil {
		return nil, err
	}
	out := []Duplicate{}
	byTMDb := map[int][]Card{}
	ids := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tmdb_id FROM movies WHERE tmdb_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var t int
		if err := rows.Scan(&id, &t); err == nil {
			ids[id] = t
		}
	}
	_ = rows.Close()
	for _, c := range cards {
		if c.FileCount > 1 {
			out = append(out, Duplicate{Kind: "versions", Movies: []Card{c}})
		}
		if t := ids[c.ID]; t > 0 {
			byTMDb[t] = append(byTMDb[t], c)
		}
	}
	for _, group := range byTMDb {
		if len(group) > 1 {
			out = append(out, Duplicate{Kind: "same_title", Movies: group})
		}
	}
	return out, nil
}

// ArtworkPath returns the stored artwork for an item and kind: the path and
// whether it is relative to the library root (local artwork) rather than to
// .popcorn/.
func (s *Store) ArtworkPath(ctx context.Context, itemType, itemID, kind string) (p string, inLibrary bool, err error) {
	var source string
	err = s.db.QueryRowContext(ctx, `SELECT source, path FROM artwork WHERE item_type = ? AND item_id = ? AND kind = ?`,
		itemType, itemID, kind).Scan(&source, &p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return p, source == "local", err
}
