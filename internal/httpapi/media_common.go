package httpapi

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/authz"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/librarydb"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// popcornDirOf is a library's metadata directory.
func popcornDirOf(lib *library.Library) string { return filepath.Join(lib.Root, ".popcorn") }

// openCatalog borrows a library's database as a catalog store.
func (s *Server) openCatalog(lib *library.Library) (*catalog.Store, func(), error) {
	db, release, err := librarydb.Acquire(popcornDirOf(lib))
	if err != nil {
		return nil, nil, err
	}
	return catalog.NewStore(db, lib.ID), release, nil
}

// readableLibraries returns the libraries the caller may read, optionally
// narrowed to one (lib query parameter) and to online ones with a database.
func (s *Server) readableLibraries(r *http.Request, u *auth.User) []library.Library {
	libs, err := s.libraries.List(r.Context())
	if err != nil {
		s.logger.Error("list libraries", "error", err)
		return nil
	}
	only := r.URL.Query().Get("lib")
	isAdmin := u != nil && u.Role == auth.RoleAdmin
	out := make([]library.Library, 0, len(libs))
	for _, l := range libs {
		if only != "" && l.ID != only {
			continue
		}
		if !isAdmin && !s.canReadLibrary(r, u, l.ID) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// eachCatalog runs fn over every readable library's catalog. A library whose
// database cannot be opened (an unplugged disk) is skipped.
func (s *Server) eachCatalog(r *http.Request, u *auth.User, fn func(lib *library.Library, st *catalog.Store) error) error {
	for _, lib := range s.readableLibraries(r, u) {
		st, release, err := s.openCatalog(&lib)
		if err != nil {
			continue
		}
		err = fn(&lib, st)
		release()
		if err != nil {
			return err
		}
	}
	return nil
}

// withLibrary resolves the {id} library and checks the caller holds caps on
// it, then opens its catalog. ok is false when a response was written.
func (s *Server) withLibrary(w http.ResponseWriter, r *http.Request, u *auth.User, caps ...authz.Capability) (*library.Library, *catalog.Store, func(), bool) {
	lib, err := s.libraries.Get(actorCtx(r, u).Context(), r.PathValue("id"))
	if err != nil {
		s.writeLibraryError(w, r, err)
		return nil, nil, nil, false
	}
	if len(caps) == 0 {
		caps = []authz.Capability{authz.CapRead}
	}
	if !s.requireCap(w, r, u, authz.LibraryKey(lib.ID), caps...) {
		return nil, nil, nil, false
	}
	st, release, err := s.openCatalog(lib)
	if err != nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable,
			"The library's storage is not available right now.")
		return nil, nil, nil, false
	}
	return lib, st, release, true
}

// writeCatalogError maps catalog and user-data errors to responses.
func (s *Server) writeCatalogError(w http.ResponseWriter, r *http.Request, err error) {
	rid := requestIDOrEmpty(r)
	var ci *catalog.ErrInvalid
	var ui *userdata.ErrInvalid
	switch {
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, userdata.ErrNotFound):
		writeError(w, s.logger, rid, http.StatusNotFound, CodeNotFound, "Not found.")
	case errors.As(err, &ci):
		writeError(w, s.logger, rid, http.StatusBadRequest, CodeBadRequest, ci.Msg)
	case errors.As(err, &ui):
		writeError(w, s.logger, rid, http.StatusBadRequest, CodeBadRequest, ui.Msg)
	case errors.Is(err, context.Canceled):
	default:
		s.logger.Error("catalog request", "path", r.URL.Path, "error", err)
		writeError(w, s.logger, rid, http.StatusInternalServerError, CodeInternal, "Internal server error.")
	}
}

func (s *Server) badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	writeError(w, s.logger, requestIDOrEmpty(r), http.StatusBadRequest, CodeBadRequest, msg)
}

// userState is a person's state across items, loaded once per request.
type userState struct {
	states map[string]*userdata.State
	lists  map[string]map[string]bool
	byShow map[string][]*userdata.State // library/show id → episode states
}

func (s *Server) loadUserState(ctx context.Context, userID string) *userState {
	us := &userState{states: map[string]*userdata.State{}, lists: map[string]map[string]bool{},
		byShow: map[string][]*userdata.State{}}
	if s.userdata == nil {
		return us
	}
	if st, err := s.userdata.States(ctx, userID); err == nil {
		us.states = st
		for _, v := range st {
			if v.Type == "episode" && v.ShowID != "" {
				k := v.LibraryID + "/" + v.ShowID
				us.byShow[k] = append(us.byShow[k], v)
			}
		}
	}
	if l, err := s.userdata.ListMembership(ctx, userID); err == nil {
		us.lists = l
	}
	return us
}

func (us *userState) stateFor(libID, itemType, id string, episodeCount int) *catalog.UserState {
	key := libID + "/" + itemType + "/" + id
	out := &catalog.UserState{}
	if st, ok := us.states[key]; ok {
		out.PositionSecs, out.DurationSecs, out.Played, out.PlayCount, out.LastPlayedAt =
			st.PositionSecs, st.DurationSecs, st.Played, st.PlayCount, st.LastPlayedAt
	}
	if l := us.lists[key]; l != nil {
		out.Favorite, out.Watchlist = l["favorite"], l["watchlist"]
	}
	if itemType == "show" {
		played := 0
		last := ""
		for _, st := range us.byShow[libID+"/"+id] {
			if st.Played {
				played++
			}
			if st.LastPlayedAt > last {
				last = st.LastPlayedAt
			}
		}
		out.UnplayedCount = max(episodeCount-played, 0)
		out.Played = episodeCount > 0 && played >= episodeCount
		out.LastPlayedAt = last
	}
	return out
}

func (us *userState) decorate(cards []catalog.Card) {
	for i := range cards {
		c := &cards[i]
		c.State = us.stateFor(c.LibraryID, c.Type, c.ID, c.EpisodeCount)
	}
}

// sortCards orders a listing. sort is title, release, rating, added,
// runtime, size, played (last played), random; order asc or desc (each sort
// has a natural default).
func sortCards(cards []catalog.Card, by, order string) {
	desc := order == "desc"
	if order == "" {
		switch by {
		case "release", "rating", "added", "size", "played", "updated":
			desc = true
		}
	}
	key := func(c *catalog.Card) string {
		t := c.SortTitle
		if t == "" {
			t = strings.ToLower(c.Title)
		}
		return t
	}
	less := func(a, b *catalog.Card) bool {
		switch by {
		case "release":
			da, db := a.ReleaseDate, b.ReleaseDate
			if da == "" && a.Year > 0 {
				da = strconv.Itoa(a.Year)
			}
			if db == "" && b.Year > 0 {
				db = strconv.Itoa(b.Year)
			}
			if da != db {
				return da < db
			}
		case "rating":
			ra, rb := a.IMDbRating, b.IMDbRating
			if ra == 0 {
				ra = a.Rating
			}
			if rb == 0 {
				rb = b.Rating
			}
			if ra != rb {
				return ra < rb
			}
		case "added":
			if a.AddedAt != b.AddedAt {
				return a.AddedAt < b.AddedAt
			}
		case "updated":
			if a.UpdatedAt != b.UpdatedAt {
				return a.UpdatedAt < b.UpdatedAt
			}
		case "runtime":
			ra, rb := a.RuntimeMins, b.RuntimeMins
			if ra == 0 {
				ra = int(a.DurationSecs / 60)
			}
			if rb == 0 {
				rb = int(b.DurationSecs / 60)
			}
			if ra != rb {
				return ra < rb
			}
		case "size":
			if a.SizeBytes != b.SizeBytes {
				return a.SizeBytes < b.SizeBytes
			}
		case "played":
			la, lb := "", ""
			if a.State != nil {
				la = a.State.LastPlayedAt
			}
			if b.State != nil {
				lb = b.State.LastPlayedAt
			}
			if la != lb {
				return la < lb
			}
		}
		return key(a) < key(b)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if desc {
			return less(&cards[j], &cards[i])
		}
		return less(&cards[i], &cards[j])
	})
}

func intParam(r *http.Request, name string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return v
}

func floatParam(r *http.Request, name string) float64 {
	v, _ := strconv.ParseFloat(r.URL.Query().Get(name), 64)
	return v
}

func filterFromQuery(r *http.Request) catalog.Filter {
	q := r.URL.Query()
	f := catalog.Filter{
		Query: q.Get("q"), Genre: q.Get("genre"), Language: q.Get("language"),
		Director: q.Get("director"), Actor: q.Get("actor"), Person: q.Get("person"),
		Resolution: q.Get("resolution"), Category: q.Get("category"), Studio: q.Get("studio"),
		YearFrom: intParam(r, "year_from", 0), YearTo: intParam(r, "year_to", 0),
		MinRating: floatParam(r, "min_rating"), Unmatched: q.Get("unmatched") == "1",
	}
	if y := intParam(r, "year", 0); y > 0 {
		f.YearFrom, f.YearTo = y, y
	}
	if d := intParam(r, "decade", 0); d > 0 {
		f.YearFrom, f.YearTo = d, d+9
	}
	return f
}

// applyWatched narrows decorated cards by watched state: watched, unwatched,
// in_progress, favorite, watchlist.
func applyWatched(cards []catalog.Card, mode string) []catalog.Card {
	if mode == "" {
		return cards
	}
	out := cards[:0]
	for _, c := range cards {
		st := c.State
		if st == nil {
			st = &catalog.UserState{}
		}
		keep := true
		switch mode {
		case "watched":
			keep = st.Played
		case "unwatched":
			keep = !st.Played
		case "in_progress":
			keep = st.PositionSecs > 0 && !st.Played || c.Type == "show" && st.LastPlayedAt != "" && !st.Played
		case "favorite":
			keep = st.Favorite
		case "watchlist":
			keep = st.Watchlist
		}
		if keep {
			out = append(out, c)
		}
	}
	return out
}

func paginate(cards []catalog.Card, r *http.Request) ([]catalog.Card, int) {
	total := len(cards)
	limit := intParam(r, "limit", 0)
	offset := intParam(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	cards = cards[offset:]
	if limit > 0 && limit < len(cards) {
		cards = cards[:limit]
	}
	if cards == nil {
		cards = []catalog.Card{}
	}
	return cards, total
}
