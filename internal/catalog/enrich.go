package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/provider"
)

// Enricher applies provider metadata and artwork to catalog items.
type Enricher struct {
	Provider provider.Provider
	Ratings  provider.Ratings // optional (OMDb)
	Logger   *slog.Logger
	HTTP     *http.Client
}

func (e *Enricher) httpClient() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// ErrNoMatch means automatic matching found no confident candidate.
var ErrNoMatch = errors.New("no confident metadata match")

// BestMatch picks the candidate for a parsed title and year: an exact title
// (or original title) match with the right year wins; a year off by one is
// accepted (release vs. premiere dates differ by country). Returns nil when
// nothing is confident.
func BestMatch(results []provider.SearchResult, title string, year int) *provider.SearchResult {
	key := NormalizeKey(title)
	var best *provider.SearchResult
	bestScore := 0.0
	for i := range results {
		r := &results[i]
		score := 0.0
		rk, ok := NormalizeKey(r.Title), NormalizeKey(r.OriginalTitle)
		switch {
		case rk == key || ok == key:
			score += 10
		case strings.Contains(rk, key) || strings.Contains(key, rk):
			score += 4
		default:
			score += 1
		}
		if year > 0 && r.Year > 0 {
			d := r.Year - year
			switch {
			case d == 0:
				score += 6
			case d == 1 || d == -1:
				score += 3
			default:
				score -= 6
			}
		}
		score += math.Min(r.Popularity, 100) / 100
		score += float64(len(results)-i) * 0.05 // provider's own relevance order
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	if best == nil || bestScore < 9 {
		return nil
	}
	return best
}

type itemInfo struct {
	title, imdbID string
	year, tmdbID  int
	locked        map[string]bool
}

func loadItemInfo(ctx context.Context, db *sql.DB, table, id string) (*itemInfo, error) {
	var info itemInfo
	var year, tmdb sql.NullInt64
	var imdb sql.NullString
	var locked string
	err := db.QueryRowContext(ctx,
		`SELECT title, year, tmdb_id, imdb_id, locked_fields FROM `+table+` WHERE id = ?`, id).
		Scan(&info.title, &year, &tmdb, &imdb, &locked)
	if err != nil {
		return nil, err
	}
	info.year, info.tmdbID, info.imdbID = int(year.Int64), int(tmdb.Int64), imdb.String
	info.locked = parseLocked(locked)
	return &info, nil
}

func parseLocked(s string) map[string]bool {
	var list []string
	_ = json.Unmarshal([]byte(s), &list)
	out := map[string]bool{}
	for _, f := range list {
		out[f] = true
	}
	return out
}

func setFailed(ctx context.Context, db *sql.DB, table, id string) {
	_, _ = db.ExecContext(ctx, `UPDATE `+table+` SET match_status = 'failed', metadata_at = ? WHERE id = ? AND match_status = 'unmatched'`, now(), id)
}

// EnrichMovie fetches metadata for a movie. providerID forces a specific
// provider title (a manual match); otherwise the movie's stored TMDB/IMDb id
// is used, or a title search.
func (e *Enricher) EnrichMovie(ctx context.Context, db *sql.DB, popcornDir, movieID, providerID string) error {
	if e.Provider == nil {
		return provider.ErrNotConfigured
	}
	info, err := loadItemInfo(ctx, db, "movies", movieID)
	if err != nil {
		return err
	}
	manual := providerID != ""
	if providerID == "" && info.tmdbID > 0 {
		providerID = strconv.Itoa(info.tmdbID)
	}
	if providerID == "" && info.imdbID != "" {
		if kind, id, err := e.Provider.FindIMDb(ctx, info.imdbID); err == nil && kind == "movie" {
			providerID = id
		}
	}
	if providerID == "" {
		results, err := e.Provider.SearchMovies(ctx, info.title, info.year)
		if err != nil {
			return err
		}
		best := BestMatch(results, info.title, info.year)
		if best == nil && info.year > 0 {
			results, err = e.Provider.SearchMovies(ctx, info.title, 0)
			if err != nil {
				return err
			}
			best = BestMatch(results, info.title, info.year)
		}
		if best == nil {
			setFailed(ctx, db, "movies", movieID)
			return ErrNoMatch
		}
		providerID = best.ID
	}
	m, err := e.Provider.Movie(ctx, providerID)
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			setFailed(ctx, db, "movies", movieID)
		}
		return err
	}
	var imdbRating float64
	var imdbVotes int
	if e.Ratings != nil && m.IMDbID != "" {
		r, v, rated, err := e.Ratings.IMDbRating(ctx, m.IMDbID)
		if err == nil {
			imdbRating, imdbVotes = r, v
			if m.ContentRating == "" {
				m.ContentRating = rated
			}
		} else if !errors.Is(err, provider.ErrNotConfigured) {
			e.Logger.Warn("imdb rating lookup", "imdb_id", m.IMDbID, "error", err)
		}
	}
	status := "matched"
	if manual {
		status = "manual"
	}
	fields := map[string]any{
		"title": m.Title, "original_title": nullStr(m.OriginalTitle), "year": nullInt(m.Year),
		"release_date": nullStr(m.ReleaseDate), "overview": nullStr(m.Overview), "tagline": nullStr(m.Tagline),
		"runtime_mins": nullInt(m.RuntimeMins), "content_rating": nullStr(m.ContentRating),
		"rating": nullFloat(m.Rating), "vote_count": nullInt(m.VoteCount),
		"imdb_rating": nullFloat(imdbRating), "imdb_votes": nullInt(imdbVotes),
		"imdb_id": nullStr(m.IMDbID), "tmdb_id": m.TMDbID,
		"genres": jsonList(m.Genres), "languages": jsonList(m.Languages),
		"original_language": nullStr(m.OriginalLanguage), "countries": jsonList(m.Countries),
		"studios": jsonList(m.Studios),
	}
	if m.Title != "" {
		fields["sort_title"] = SortTitle(m.Title)
	}
	if err := updateItem(ctx, db, "movies", movieID, fields, info.locked, status); err != nil {
		return err
	}
	if err := saveCredits(ctx, db, "movie", movieID, m.Credits); err != nil {
		return err
	}
	e.saveArtwork(ctx, db, popcornDir, "movie", movieID, map[string]string{
		"poster": m.Images.Poster, "backdrop": m.Images.Backdrop, "logo": m.Images.Logo,
	}, manual)
	return nil
}

// EnrichShow fetches metadata for a show, its seasons and its episodes.
func (e *Enricher) EnrichShow(ctx context.Context, db *sql.DB, popcornDir, showID, providerID string) error {
	if e.Provider == nil {
		return provider.ErrNotConfigured
	}
	info, err := loadItemInfo(ctx, db, "shows", showID)
	if err != nil {
		return err
	}
	manual := providerID != ""
	if providerID == "" && info.tmdbID > 0 {
		providerID = strconv.Itoa(info.tmdbID)
	}
	if providerID == "" && info.imdbID != "" {
		if kind, id, err := e.Provider.FindIMDb(ctx, info.imdbID); err == nil && kind == "show" {
			providerID = id
		}
	}
	if providerID == "" {
		results, err := e.Provider.SearchShows(ctx, info.title, info.year)
		if err != nil {
			return err
		}
		best := BestMatch(results, info.title, info.year)
		if best == nil && info.year > 0 {
			results, err = e.Provider.SearchShows(ctx, info.title, 0)
			if err != nil {
				return err
			}
			best = BestMatch(results, info.title, 0)
		}
		if best == nil {
			setFailed(ctx, db, "shows", showID)
			return ErrNoMatch
		}
		providerID = best.ID
	}
	sh, err := e.Provider.Show(ctx, providerID)
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			setFailed(ctx, db, "shows", showID)
		}
		return err
	}
	var imdbRating float64
	var imdbVotes int
	if e.Ratings != nil && sh.IMDbID != "" {
		if r, v, rated, err := e.Ratings.IMDbRating(ctx, sh.IMDbID); err == nil {
			imdbRating, imdbVotes = r, v
			if sh.ContentRating == "" {
				sh.ContentRating = rated
			}
		}
	}
	status := "matched"
	if manual {
		status = "manual"
	}
	fields := map[string]any{
		"title": sh.Title, "original_title": nullStr(sh.OriginalTitle), "year": nullInt(sh.Year),
		"first_air_date": nullStr(sh.FirstAirDate), "last_air_date": nullStr(sh.LastAirDate),
		"status": nullStr(sh.Status), "overview": nullStr(sh.Overview), "tagline": nullStr(sh.Tagline),
		"content_rating": nullStr(sh.ContentRating), "rating": nullFloat(sh.Rating),
		"vote_count": nullInt(sh.VoteCount), "imdb_rating": nullFloat(imdbRating),
		"imdb_votes": nullInt(imdbVotes), "imdb_id": nullStr(sh.IMDbID), "tmdb_id": sh.TMDbID,
		"genres": jsonList(sh.Genres), "languages": jsonList(sh.Languages),
		"original_language": nullStr(sh.OriginalLanguage), "countries": jsonList(sh.Countries),
		"studios": jsonList(sh.Studios),
	}
	if sh.Title != "" {
		fields["sort_title"] = SortTitle(sh.Title)
	}
	if sh.Animation && sh.OriginalLanguage == "ja" {
		fields["category"] = "anime"
	}
	if err := updateItem(ctx, db, "shows", showID, fields, info.locked, status); err != nil {
		return err
	}
	if err := saveCredits(ctx, db, "show", showID, sh.Credits); err != nil {
		return err
	}
	e.saveArtwork(ctx, db, popcornDir, "show", showID, map[string]string{
		"poster": sh.Images.Poster, "backdrop": sh.Images.Backdrop, "logo": sh.Images.Logo,
	}, manual)
	return e.enrichSeasons(ctx, db, popcornDir, showID, sh, manual)
}

func (e *Enricher) enrichSeasons(ctx context.Context, db *sql.DB, popcornDir, showID string, sh *provider.Show, manual bool) error {
	type localSeason struct {
		id     string
		number int
		locked map[string]bool
	}
	rows, err := db.QueryContext(ctx, `SELECT id, season_number, locked_fields FROM seasons WHERE show_id = ?`, showID)
	if err != nil {
		return err
	}
	var seasons []localSeason
	for rows.Next() {
		var s localSeason
		var locked string
		if err := rows.Scan(&s.id, &s.number, &locked); err != nil {
			_ = rows.Close()
			return err
		}
		s.locked = parseLocked(locked)
		seasons = append(seasons, s)
	}
	_ = rows.Close()

	known := map[int]provider.SeasonSummary{}
	for _, s := range sh.Seasons {
		known[s.Number] = s
	}
	// Anime and some web series number episodes continuously ("episode 30"
	// with no seasons on disk); cumulative counts map those to the
	// provider's seasons.
	var cumulative []provider.SeasonSummary
	for _, s := range sh.Seasons {
		if s.Number > 0 {
			cumulative = append(cumulative, s)
		}
	}
	fetched := map[int]*provider.Season{}
	fetch := func(n int) *provider.Season {
		if s, ok := fetched[n]; ok {
			return s
		}
		s, err := e.Provider.Season(ctx, sh.ProviderID, n)
		if err != nil {
			e.Logger.Warn("season metadata", "show", sh.Title, "season", n, "error", err)
			s = nil
		}
		fetched[n] = s
		return s
	}
	for _, ls := range seasons {
		sum, ok := known[ls.number]
		if !ok {
			continue
		}
		pm := fetch(ls.number)
		fields := map[string]any{"overview": nullStr(sum.Overview), "air_date": nullStr(sum.AirDate),
			"episode_total": nullInt(sum.EpisodeCount)}
		if sum.Title != "" {
			fields["title"] = sum.Title
		}
		if err := updateItem(ctx, db, "seasons", ls.id, fields, ls.locked, ""); err != nil {
			return err
		}
		poster := sum.PosterURL
		if pm != nil && pm.PosterURL != "" {
			poster = pm.PosterURL
		}
		e.saveArtwork(ctx, db, popcornDir, "season", ls.id, map[string]string{"poster": poster}, manual)

		erows, err := db.QueryContext(ctx,
			`SELECT id, episode_number, locked_fields FROM episodes WHERE season_id = ?`, ls.id)
		if err != nil {
			return err
		}
		type localEp struct {
			id     string
			number int
			locked map[string]bool
		}
		var eps []localEp
		for erows.Next() {
			var le localEp
			var locked string
			if err := erows.Scan(&le.id, &le.number, &locked); err != nil {
				_ = erows.Close()
				return err
			}
			le.locked = parseLocked(locked)
			eps = append(eps, le)
		}
		_ = erows.Close()
		for _, le := range eps {
			var meta *provider.Episode
			if pm != nil {
				for i := range pm.Episodes {
					if pm.Episodes[i].Number == le.number {
						meta = &pm.Episodes[i]
						break
					}
				}
			}
			if meta == nil && ls.number == 1 && le.number > sum.EpisodeCount && len(cumulative) > 1 {
				// Absolute numbering: walk the seasons' episode counts.
				n := le.number
				for _, cs := range cumulative {
					if n <= cs.EpisodeCount {
						if sm := fetch(cs.Number); sm != nil {
							for i := range sm.Episodes {
								if sm.Episodes[i].Number == n {
									meta = &sm.Episodes[i]
								}
							}
						}
						break
					}
					n -= cs.EpisodeCount
				}
			}
			if meta == nil {
				continue
			}
			fields := map[string]any{
				"title": nullStr(meta.Title), "overview": nullStr(meta.Overview), "air_date": nullStr(meta.AirDate),
				"rating": nullFloat(meta.Rating), "vote_count": nullInt(meta.VoteCount), "tmdb_id": nullInt(meta.TMDbID),
			}
			if meta.RuntimeMins > 0 {
				fields["runtime_mins"] = meta.RuntimeMins
			}
			if err := updateItem(ctx, db, "episodes", le.id, fields, le.locked, ""); err != nil {
				return err
			}
			if len(meta.Credits) > 0 {
				if err := saveCredits(ctx, db, "episode", le.id, meta.Credits); err != nil {
					return err
				}
			}
			e.saveArtwork(ctx, db, popcornDir, "episode", le.id, map[string]string{"thumb": meta.StillURL}, manual)
		}
	}
	return nil
}

// updateItem writes fields to an item row, skipping fields a person locked by
// editing them. status, when set, becomes match_status.
func updateItem(ctx context.Context, db *sql.DB, table, id string, fields map[string]any, locked map[string]bool, status string) error {
	sets := []string{}
	args := []any{}
	for k, v := range fields {
		if locked[k] {
			continue
		}
		sets = append(sets, k+" = ?")
		args = append(args, v)
	}
	if status != "" {
		sets = append(sets, "match_status = ?", "metadata_at = ?", "updated_at = ?")
		args = append(args, status, now(), now())
	} else if table == "episodes" {
		sets = append(sets, "updated_at = ?")
		args = append(args, now())
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := db.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update %s: %w", table, err)
	}
	return nil
}

func saveCredits(ctx context.Context, db *sql.DB, itemType, itemID string, people []provider.Person) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM credits WHERE item_type = ? AND item_id = ?`, itemType, itemID); err != nil {
		return err
	}
	for _, p := range people {
		if p.Name == "" {
			continue
		}
		var personID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM people WHERE tmdb_id = ?`, p.ProviderID).Scan(&personID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			personID = NewID()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO people (id, name, tmdb_id, profile_url, updated_at) VALUES (?, ?, ?, ?, ?)`,
				personID, p.Name, p.ProviderID, nullStr(p.ProfileURL), now()); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if p.ProfileURL != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE people SET name = ?, profile_url = ?, updated_at = ? WHERE id = ?`,
					p.Name, p.ProfileURL, now(), personID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO credits (item_type, item_id, person_id, role, character, ord) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			itemType, itemID, personID, p.Role, nullStr(p.Character), p.Order); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ArtworkDir is where downloaded, uploaded and generated artwork lives.
func ArtworkDir(popcornDir string) string { return filepath.Join(popcornDir, "artwork") }

// ArtworkRelPath is an artwork file's path relative to .popcorn/.
func ArtworkRelPath(itemType, itemID, kind, ext string) string {
	return filepath.ToSlash(filepath.Join("artwork", itemType, itemID+"-"+kind+ext))
}

// saveArtwork downloads provider artwork for an item, without replacing
// uploaded or local artwork unless force is set (a manual match also brings
// the right pictures).
func (e *Enricher) saveArtwork(ctx context.Context, db *sql.DB, popcornDir, itemType, itemID string, urls map[string]string, force bool) {
	changed := false
	for kind, u := range urls {
		if u == "" {
			continue
		}
		var source, remote sql.NullString
		err := db.QueryRowContext(ctx, `SELECT source, remote_url FROM artwork WHERE item_type = ? AND item_id = ? AND kind = ?`,
			itemType, itemID, kind).Scan(&source, &remote)
		if err == nil {
			if source.String == "upload" || (source.String == "local" && !force) {
				continue
			}
			if source.String == "provider" && remote.String == u {
				continue
			}
		}
		rel := ArtworkRelPath(itemType, itemID, kind, ".jpg")
		if strings.HasSuffix(strings.ToLower(u), ".png") {
			rel = ArtworkRelPath(itemType, itemID, kind, ".png")
		}
		if err := e.download(ctx, u, filepath.Join(popcornDir, filepath.FromSlash(rel))); err != nil {
			e.Logger.Warn("artwork download", "url", u, "error", err)
			continue
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO artwork (item_type, item_id, kind, source, path, remote_url, updated_at)
			 VALUES (?, ?, ?, 'provider', ?, ?, ?)
			 ON CONFLICT(item_type, item_id, kind) DO UPDATE SET source = 'provider', path = excluded.path,
			   remote_url = excluded.remote_url, updated_at = excluded.updated_at`,
			itemType, itemID, kind, rel, u, now()); err != nil {
			e.Logger.Warn("save artwork", "error", err)
			continue
		}
		changed = true
	}
	if changed {
		BumpArtwork(ctx, db, itemType, itemID)
	}
}

// BumpArtwork increments an item's artwork version so browsers fetch the new
// image instead of a cached one.
func BumpArtwork(ctx context.Context, db *sql.DB, itemType, itemID string) {
	table := map[string]string{"movie": "movies", "show": "shows", "season": "seasons", "episode": "episodes"}[itemType]
	if table == "" {
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE `+table+` SET artwork_version = artwork_version + 1 WHERE id = ?`, itemID)
}

func (e *Enricher) download(ctx context.Context, u, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := e.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 20<<20)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func jsonList(l []string) string {
	if l == nil {
		l = []string{}
	}
	b, _ := json.Marshal(l)
	return string(b)
}
