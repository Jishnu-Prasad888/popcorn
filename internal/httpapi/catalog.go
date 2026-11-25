package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// handleListMovies — GET /api/v1/movies
//
// Every movie the caller can read, across libraries (or one, ?lib=), with
// filters, the person's watched state, sorting and paging.
func (s *Server) handleListMovies(w http.ResponseWriter, r *http.Request, u *auth.User) {
	s.listItems(w, r, u, "movie")
}

// handleListShows — GET /api/v1/shows
func (s *Server) handleListShows(w http.ResponseWriter, r *http.Request, u *auth.User) {
	s.listItems(w, r, u, "show")
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request, u *auth.User, itemType string) {
	f := filterFromQuery(r)
	var cards []catalog.Card
	err := s.eachCatalog(r, u, func(lib *library.Library, st *catalog.Store) error {
		var got []catalog.Card
		var err error
		if itemType == "show" {
			got, err = st.Shows(r.Context(), f)
		} else {
			got, err = st.Movies(r.Context(), f)
		}
		cards = append(cards, got...)
		return err
	})
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	us.decorate(cards)
	cards = applyWatched(cards, r.URL.Query().Get("watched"))
	sortCards(cards, r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
	page, total := paginate(cards, r)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"items": page, "total": total})
}

// handleFacets — GET /api/v1/facets?type=movie|show
func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request, u *auth.User) {
	itemType := r.URL.Query().Get("type")
	merged := &catalog.Facets{}
	seen := map[string]map[string]bool{"g": {}, "l": {}, "s": {}}
	years := map[int]bool{}
	err := s.eachCatalog(r, u, func(lib *library.Library, st *catalog.Store) error {
		f, err := st.Facets(r.Context(), itemType)
		if err != nil {
			return err
		}
		add := func(dst *[]string, set map[string]bool, vals []string) {
			for _, v := range vals {
				if !set[v] {
					set[v] = true
					*dst = append(*dst, v)
				}
			}
		}
		add(&merged.Genres, seen["g"], f.Genres)
		add(&merged.Languages, seen["l"], f.Languages)
		add(&merged.Studios, seen["s"], f.Studios)
		for _, y := range f.Years {
			if !years[y] {
				years[y] = true
				merged.Years = append(merged.Years, y)
			}
		}
		return nil
	})
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	sort.Strings(merged.Genres)
	sort.Strings(merged.Languages)
	sort.Strings(merged.Studios)
	sort.Sort(sort.Reverse(sort.IntSlice(merged.Years)))
	for _, p := range []*[]string{&merged.Genres, &merged.Languages, &merged.Studios} {
		if *p == nil {
			*p = []string{}
		}
	}
	if merged.Years == nil {
		merged.Years = []int{}
	}
	writeJSON(w, s.logger, http.StatusOK, merged)
}

// handleGetMovie — GET /api/v1/libraries/{id}/movies/{movieID}
func (s *Server) handleGetMovie(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	m, err := st.Movie(r.Context(), r.PathValue("movieID"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	related, err := st.Related(r.Context(), "movie", m.ID, 16)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	m.State = us.stateFor(lib.ID, "movie", m.ID, 0)
	us.decorate(related)
	var history []userdata.HistoryEntry
	if s.userdata != nil {
		all, _ := s.userdata.History(r.Context(), u.ID, 500, 0)
		for _, h := range all {
			if h.LibraryID == lib.ID && h.Type == "movie" && h.Ref.ID == m.ID {
				history = append(history, h)
			}
		}
	}
	if history == nil {
		history = []userdata.HistoryEntry{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"movie": m, "related": related, "history": history,
		"can_edit": s.canEdit(r, u, lib.ID)})
}

// handleGetShow — GET /api/v1/libraries/{id}/shows/{showID}
//
// The show with its seasons (and the person's progress in each) and "next
// up": the episode to play next.
func (s *Server) handleGetShow(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	sh, err := st.Show(r.Context(), r.PathValue("showID"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	episodes, err := st.Episodes(r.Context(), sh.ID, "", "")
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	sh.State = us.stateFor(lib.ID, "show", sh.ID, sh.EpisodeCount)
	us.decorate(episodes)
	perSeason := map[string][2]int{}
	for _, e := range episodes {
		c := perSeason[e.SeasonID]
		c[0]++
		if e.State != nil && e.State.Played {
			c[1]++
		}
		perSeason[e.SeasonID] = c
	}
	for i := range sh.Seasons {
		c := perSeason[sh.Seasons[i].ID]
		sh.Seasons[i].State = &catalog.UserState{UnplayedCount: c[0] - c[1], Played: c[0] > 0 && c[0] == c[1]}
	}
	next := nextUp(episodes)
	related, _ := st.Related(r.Context(), "show", sh.ID, 16)
	us.decorate(related)
	if related == nil {
		related = []catalog.Card{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"show": sh, "next_up": next, "related": related,
		"can_edit": s.canEdit(r, u, lib.ID)})
}

// nextUp picks the episode to continue a show with: an episode in progress,
// else the first unwatched one after the last watched, else the first
// unwatched. Specials are skipped unless everything else is watched.
func nextUp(episodes []catalog.Card) *catalog.Card {
	var inProgress *catalog.Card
	lastPlayed := ""
	lastIdx := -1
	for i := range episodes {
		e := &episodes[i]
		if e.State == nil {
			continue
		}
		if e.State.PositionSecs > 0 && !e.State.Played {
			if inProgress == nil || e.State.LastPlayedAt > inProgress.State.LastPlayedAt {
				inProgress = e
			}
		}
		if e.State.Played && e.State.LastPlayedAt >= lastPlayed && e.SeasonNumber != 0 {
			lastPlayed = e.State.LastPlayedAt
			lastIdx = i
		}
	}
	if inProgress != nil {
		return inProgress
	}
	for i := lastIdx + 1; i < len(episodes); i++ {
		if episodes[i].SeasonNumber != 0 && (episodes[i].State == nil || !episodes[i].State.Played) {
			return &episodes[i]
		}
	}
	for i := range episodes {
		if episodes[i].SeasonNumber != 0 && (episodes[i].State == nil || !episodes[i].State.Played) {
			return &episodes[i]
		}
	}
	return nil
}

// handleSeasonEpisodes — GET /api/v1/libraries/{id}/seasons/{seasonID}/episodes
func (s *Server) handleSeasonEpisodes(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	season, err := st.Season(r.Context(), r.PathValue("seasonID"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	eps, err := st.Episodes(r.Context(), "", season.ID, "")
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	us.decorate(eps)
	if eps == nil {
		eps = []catalog.Card{}
	}
	_ = lib
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"season": season, "episodes": eps})
}

// handleGetEpisode — GET /api/v1/libraries/{id}/episodes/{episodeID}
func (s *Server) handleGetEpisode(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	e, err := st.Episode(r.Context(), r.PathValue("episodeID"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	e.State = us.stateFor(lib.ID, "episode", e.ID, 0)
	prev, next, _ := st.AdjacentEpisodes(r.Context(), e.ShowID, e.ID)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"episode": e, "previous_id": prev, "next_id": next,
		"can_edit": s.canEdit(r, u, lib.ID)})
}

// handleGetPerson — GET /api/v1/libraries/{id}/people/{personID}
//
// A person and everything they are credited on, in every readable library
// (matched by TMDB id, else by name).
func (s *Server) handleGetPerson(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	p, err := st.Person(r.Context(), r.PathValue("personID"))
	release()
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	items := []catalog.Card{}
	roles := map[string][]string{}
	err = s.eachCatalog(r, u, func(lib *library.Library, other *catalog.Store) error {
		var pid string
		var err error
		if p.TMDbID > 0 {
			pid, err = other.PersonByTMDb(r.Context(), p.TMDbID)
		} else {
			pid, err = other.PersonByName(r.Context(), p.Name)
		}
		if errors.Is(err, catalog.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		op, err := other.Person(r.Context(), pid)
		if err != nil {
			return err
		}
		items = append(items, op.Items...)
		for k, v := range op.Roles {
			roles[k] = v
		}
		return nil
	})
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	us.decorate(items)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Year > items[j].Year })
	p.Items, p.Roles = items, roles
	writeJSON(w, s.logger, http.StatusOK, p)
}

// handleSearch — GET /api/v1/search?q=
//
// Titles (movies, shows, episodes) and people matching the query. A query
// can also be a year ("1999") or a genre name.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, s.logger, http.StatusOK, map[string]any{"movies": []any{}, "shows": []any{}, "episodes": []any{}, "people": []any{}})
		return
	}
	var movies, shows, episodes []catalog.Card
	var people []catalog.PersonHit
	err := s.eachCatalog(r, u, func(lib *library.Library, st *catalog.Store) error {
		ctx := r.Context()
		byTitle := catalog.Filter{Query: q}
		m, err := st.Movies(ctx, byTitle)
		if err != nil {
			return err
		}
		sh, err := st.Shows(ctx, byTitle)
		if err != nil {
			return err
		}
		// A genre or year query adds those titles too.
		extra := []catalog.Filter{{Genre: q}}
		if y := atoiSafe(q); y >= 1880 && y <= 2100 {
			extra = append(extra, catalog.Filter{YearFrom: y, YearTo: y})
		}
		for _, f := range extra {
			mm, err := st.Movies(ctx, f)
			if err != nil {
				return err
			}
			m = append(m, mm...)
			ss, err := st.Shows(ctx, f)
			if err != nil {
				return err
			}
			sh = append(sh, ss...)
		}
		movies = append(movies, dedupe(m)...)
		shows = append(shows, dedupe(sh)...)
		ep, err := st.Episodes(ctx, "", "", q)
		if err != nil {
			return err
		}
		if len(ep) > 30 {
			ep = ep[:30]
		}
		episodes = append(episodes, ep...)
		ppl, err := st.SearchPeople(ctx, q, 20)
		if err != nil {
			return err
		}
		people = append(people, ppl...)
		return nil
	})
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	us := s.loadUserState(r.Context(), u.ID)
	us.decorate(movies)
	us.decorate(shows)
	us.decorate(episodes)
	rank := func(cards []catalog.Card) {
		lq := strings.ToLower(q)
		sort.SliceStable(cards, func(i, j int) bool {
			ai := strings.HasPrefix(strings.ToLower(cards[i].Title), lq)
			aj := strings.HasPrefix(strings.ToLower(cards[j].Title), lq)
			if ai != aj {
				return ai
			}
			return cards[i].Rating > cards[j].Rating
		})
	}
	rank(movies)
	rank(shows)
	if movies == nil {
		movies = []catalog.Card{}
	}
	if shows == nil {
		shows = []catalog.Card{}
	}
	if episodes == nil {
		episodes = []catalog.Card{}
	}
	if people == nil {
		people = []catalog.PersonHit{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"movies": movies, "shows": shows, "episodes": episodes, "people": people})
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func dedupe(cards []catalog.Card) []catalog.Card {
	seen := map[string]bool{}
	out := cards[:0]
	for _, c := range cards {
		k := c.LibraryID + c.ID
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}

// artTypes are the item types artwork is served for.
var artTypes = map[string]bool{"movie": true, "show": true, "season": true, "episode": true}

// handleGetArtwork — GET /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}
//
// Serves an item's poster, backdrop, logo or thumbnail. URLs carry the
// artwork version (?v=), so responses are cached for good.
func (s *Server) handleGetArtwork(w http.ResponseWriter, r *http.Request, u *auth.User) {
	itemType, kind := r.PathValue("type"), r.PathValue("kind")
	if !artTypes[itemType] {
		s.badRequest(w, r, "Unknown item type.")
		return
	}
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	p, inLib, err := st.ArtworkPath(r.Context(), itemType, r.PathValue("itemID"), kind)
	release()
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	base := popcornDirOf(lib)
	if inLib {
		base = lib.Root
	}
	full := filepath.Join(base, filepath.FromSlash(p))
	if !withinDir(base, full) {
		s.writeCatalogError(w, r, catalog.ErrNotFound)
		return
	}
	serveImageFile(w, r, full)
}

func withinDir(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func serveImageFile(w http.ResponseWriter, r *http.Request, full string) {
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	switch strings.ToLower(filepath.Ext(full)) {
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "image/jpeg")
	}
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handlePersonImage — GET /api/v1/libraries/{id}/people/{personID}/image
//
// A person's photo, downloaded from the provider once and kept in the
// library, so the cast list works offline afterwards.
func (s *Server) handlePersonImage(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	pid := r.PathValue("personID")
	var url string
	err := st.DB().QueryRowContext(r.Context(), `SELECT COALESCE(profile_url, '') FROM people WHERE id = ?`, pid).Scan(&url)
	release()
	if err != nil || url == "" || len(pid) > 64 || strings.ContainsAny(pid, `/\.`) {
		http.NotFound(w, r)
		return
	}
	dest := filepath.Join(popcornDirOf(lib), "artwork", "person", pid+".jpg")
	if _, err := os.Stat(dest); err != nil {
		if !strings.HasPrefix(url, "https://image.tmdb.org/") {
			http.NotFound(w, r)
			return
		}
		if err := downloadTo(r, url, dest); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=2592000")
	serveImageFile(w, r, dest)
}

var imageClient = &http.Client{Timeout: 20 * time.Second}

func downloadTo(r *http.Request, url, dest string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := imageClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("download failed")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 5<<20)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
