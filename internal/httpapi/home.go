package httpapi

import (
	"context"
	"net/http"
	"sort"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/authz"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

func (s *Server) canEdit(r *http.Request, u *auth.User, libID string) bool {
	if u != nil && u.Role == auth.RoleAdmin {
		return true
	}
	if s.authz == nil {
		return false
	}
	ok, err := s.authz.Can(r.Context(), principalFrom(u), authz.LibraryKey(libID), authz.CapEdit)
	return err == nil && ok
}

// resolver turns saved references into cards, loading each library's
// listings once per request.
type resolver struct {
	s      *Server
	r      *http.Request
	u      *auth.User
	libs   map[string]*library.Library
	movies map[string]map[string]catalog.Card
	shows  map[string]map[string]catalog.Card
}

func (s *Server) newResolver(r *http.Request, u *auth.User) *resolver {
	res := &resolver{s: s, r: r, u: u, libs: map[string]*library.Library{},
		movies: map[string]map[string]catalog.Card{}, shows: map[string]map[string]catalog.Card{}}
	for _, l := range s.readableLibraries(r, u) {
		l := l
		res.libs[l.ID] = &l
	}
	return res
}

func (res *resolver) load(libID string) bool {
	if _, ok := res.movies[libID]; ok {
		return true
	}
	lib, ok := res.libs[libID]
	if !ok {
		return false
	}
	st, release, err := res.s.openCatalog(lib)
	if err != nil {
		return false
	}
	defer release()
	m := map[string]catalog.Card{}
	if cards, err := st.Movies(res.r.Context(), catalog.Filter{}); err == nil {
		for _, c := range cards {
			m[c.ID] = c
		}
	}
	sh := map[string]catalog.Card{}
	if cards, err := st.Shows(res.r.Context(), catalog.Filter{}); err == nil {
		for _, c := range cards {
			sh[c.ID] = c
		}
	}
	res.movies[libID], res.shows[libID] = m, sh
	return true
}

func (res *resolver) allMovies() []catalog.Card {
	var out []catalog.Card
	for id := range res.libs {
		if res.load(id) {
			for _, c := range res.movies[id] {
				out = append(out, c)
			}
		}
	}
	return out
}

func (res *resolver) allShows() []catalog.Card {
	var out []catalog.Card
	for id := range res.libs {
		if res.load(id) {
			for _, c := range res.shows[id] {
				out = append(out, c)
			}
		}
	}
	return out
}

// card resolves one reference; ok is false when the item is gone or not
// readable.
func (res *resolver) card(ref userdata.Ref) (catalog.Card, bool) {
	if !res.load(ref.LibraryID) {
		return catalog.Card{}, false
	}
	switch ref.Type {
	case "movie":
		c, ok := res.movies[ref.LibraryID][ref.ID]
		return c, ok
	case "show":
		c, ok := res.shows[ref.LibraryID][ref.ID]
		return c, ok
	case "episode":
		lib := res.libs[ref.LibraryID]
		st, release, err := res.s.openCatalog(lib)
		if err != nil {
			return catalog.Card{}, false
		}
		defer release()
		c, err := st.EpisodeCard(res.r.Context(), ref.ID)
		if err != nil {
			return catalog.Card{}, false
		}
		return *c, true
	}
	return catalog.Card{}, false
}

func (res *resolver) cards(refs []userdata.Ref) []catalog.Card {
	out := []catalog.Card{}
	for _, ref := range refs {
		if c, ok := res.card(ref); ok {
			out = append(out, c)
		}
	}
	return out
}

// nextUpFor returns the next episode to watch of a show.
func (s *Server) nextUpFor(ctx context.Context, lib *library.Library, showID string, us *userState) *catalog.Card {
	st, release, err := s.openCatalog(lib)
	if err != nil {
		return nil
	}
	defer release()
	eps, err := st.Episodes(ctx, showID, "", "")
	if err != nil {
		return nil
	}
	us.decorate(eps)
	return nextUp(eps)
}

type homeRow struct {
	ID    string         `json:"id"`
	Title string         `json:"title"`
	Kind  string         `json:"kind"` // cards, landscape, collections
	Items []catalog.Card `json:"items"`
	Link  string         `json:"link,omitempty"`
}

// handleHome — GET /api/v1/home
//
// The rows of the home page: featured titles, Continue Watching, Next Up,
// recently added, recommendations and genre rows.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	res := s.newResolver(r, u)
	us := s.loadUserState(ctx, u.ID)
	movies := res.allMovies()
	shows := res.allShows()
	us.decorate(movies)
	us.decorate(shows)

	rows := []homeRow{}
	add := func(row homeRow) {
		if len(row.Items) > 0 {
			rows = append(rows, row)
		}
	}

	// Continue Watching: partly watched movies and episodes, one per show.
	var cont []catalog.Card
	inShow := map[string]bool{}
	if s.userdata != nil {
		states, _ := s.userdata.InProgress(ctx, u.ID, 40)
		for _, st := range states {
			if st.Type == "episode" && st.ShowID != "" {
				if inShow[st.LibraryID+st.ShowID] {
					continue
				}
				inShow[st.LibraryID+st.ShowID] = true
			}
			if c, ok := res.card(st.Ref); ok {
				cont = append(cont, c)
			}
		}
	}
	us.decorate(cont)

	// Next Up: the next episode of shows watched recently.
	var next []catalog.Card
	if s.userdata != nil {
		recent, _ := s.userdata.RecentShows(ctx, u.ID, 20)
		for _, ref := range recent {
			if inShow[ref.LibraryID+ref.ID] {
				continue
			}
			lib := res.libs[ref.LibraryID]
			if lib == nil {
				continue
			}
			if e := s.nextUpFor(ctx, lib, ref.ID, us); e != nil {
				next = append(next, *e)
			}
		}
	}

	// Featured: the hero. What the person is in the middle of, then new
	// arrivals and favourites that have a backdrop.
	var featured []catalog.Card
	seen := map[string]bool{}
	pick := func(c catalog.Card) {
		k := c.LibraryID + c.ID
		if seen[k] || len(featured) >= 6 {
			return
		}
		seen[k] = true
		featured = append(featured, c)
	}
	for _, c := range cont {
		if c.Type == "movie" && c.Art.Backdrop {
			pick(c)
		}
		if c.Type == "episode" && c.ShowArt != nil && c.ShowArt.Backdrop {
			if sh, ok := res.card(userdata.Ref{LibraryID: c.LibraryID, Type: "show", ID: c.ShowID}); ok {
				sh.State = us.stateFor(sh.LibraryID, "show", sh.ID, sh.EpisodeCount)
				pick(sh)
			}
		}
	}
	recentAll := append(append([]catalog.Card{}, movies...), shows...)
	sortCards(recentAll, "added", "")
	for _, c := range recentAll {
		if c.Art.Backdrop && c.Overview != "" && (c.State == nil || !c.State.Played) {
			pick(c)
		}
	}
	add(homeRow{ID: "featured", Title: "Featured", Kind: "hero", Items: featured})
	add(homeRow{ID: "continue", Title: "Continue Watching", Kind: "landscape", Items: limitCards(cont, 20)})
	add(homeRow{ID: "next_up", Title: "Next Up", Kind: "landscape", Items: limitCards(next, 20)})

	recentMovies := append([]catalog.Card{}, movies...)
	sortCards(recentMovies, "added", "")
	add(homeRow{ID: "recent_movies", Title: "Recently Added Movies", Kind: "cards", Items: limitCards(recentMovies, 24),
		Link: "/movies?sort=added"})
	recentShows := append([]catalog.Card{}, shows...)
	sortCards(recentShows, "updated", "")
	add(homeRow{ID: "recent_shows", Title: "New Episodes", Kind: "cards", Items: limitCards(recentShows, 24),
		Link: "/shows?sort=updated"})

	if s.userdata != nil {
		wl, _ := s.userdata.List(ctx, u.ID, "watchlist")
		refs := make([]userdata.Ref, len(wl))
		for i, it := range wl {
			refs[i] = it.Ref
		}
		items := res.cards(refs)
		us.decorate(items)
		add(homeRow{ID: "watchlist", Title: "Your Watchlist", Kind: "cards", Items: limitCards(items, 24), Link: "/watchlist"})
	}

	// Because you watched: titles like the last movie the person finished.
	if s.userdata != nil {
		hist, _ := s.userdata.History(ctx, u.ID, 30, 0)
		for _, h := range hist {
			if h.Type != "movie" {
				continue
			}
			lib := res.libs[h.LibraryID]
			if lib == nil {
				continue
			}
			src, ok := res.card(h.Ref)
			if !ok {
				continue
			}
			st, release, err := s.openCatalog(lib)
			if err != nil {
				continue
			}
			rel, _ := st.Related(ctx, "movie", h.Ref.ID, 20)
			release()
			us.decorate(rel)
			rel = applyWatched(rel, "unwatched")
			add(homeRow{ID: "because", Title: "Because you watched " + src.Title, Kind: "cards", Items: rel})
			break
		}
	}

	// Top rated, unwatched.
	top := applyWatched(append([]catalog.Card{}, movies...), "unwatched")
	sortCards(top, "rating", "")
	add(homeRow{ID: "top_rated", Title: "Top Rated", Kind: "cards", Items: limitCards(top, 24), Link: "/movies?sort=rating"})

	// Genre rows: the most common genres.
	count := map[string]int{}
	for _, c := range recentAll {
		for _, g := range c.Genres {
			count[g]++
		}
	}
	type gc struct {
		g string
		n int
	}
	var genres []gc
	for g, n := range count {
		genres = append(genres, gc{g, n})
	}
	sort.Slice(genres, func(i, j int) bool { return genres[i].n > genres[j].n })
	for i := 0; i < len(genres) && i < 4; i++ {
		g := genres[i].g
		var items []catalog.Card
		for _, c := range recentAll {
			for _, cg := range c.Genres {
				if cg == g {
					items = append(items, c)
					break
				}
			}
		}
		sortCards(items, "rating", "")
		add(homeRow{ID: "genre:" + g, Title: g, Kind: "cards", Items: limitCards(items, 20), Link: "/movies?genre=" + g})
	}

	var cols []userdata.Collection
	if s.userdata != nil {
		cols, _ = s.userdata.Collections(ctx, u.ID)
	}
	if cols == nil {
		cols = []userdata.Collection{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"rows":        rows,
		"collections": cols,
		"counts": map[string]int{
			"movies": len(movies), "shows": len(shows),
		},
	})
}

func limitCards(c []catalog.Card, n int) []catalog.Card {
	if len(c) > n {
		return c[:n]
	}
	if c == nil {
		return []catalog.Card{}
	}
	return c
}
