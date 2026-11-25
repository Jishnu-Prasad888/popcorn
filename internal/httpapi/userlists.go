package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

func (s *Server) checkRef(r *http.Request, u *auth.User, ref userdata.Ref) error {
	lib, err := s.libraries.Get(r.Context(), ref.LibraryID)
	if err != nil {
		return err
	}
	if u.Role != auth.RoleAdmin && !s.canReadLibrary(r, u, lib.ID) {
		return catalog.ErrNotFound
	}
	st, release, err := s.openCatalog(lib)
	if err != nil {
		return err
	}
	defer release()
	ok, err := st.Exists(r.Context(), ref.Type, ref.ID)
	if err != nil {
		return err
	}
	if !ok {
		return catalog.ErrNotFound
	}
	return nil
}

func (s *Server) writeRefError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrNotFound) {
		s.writeLibraryError(w, r, err)
		return
	}
	s.writeCatalogError(w, r, err)
}

// handleSetListItem — PUT/DELETE /api/v1/me/lists/{list}/{lib}/{type}/{itemID}
func (s *Server) handleSetListItem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ref := userdata.Ref{LibraryID: r.PathValue("lib"), Type: r.PathValue("type"), ID: r.PathValue("itemID")}
	in := r.Method == http.MethodPut
	if in {
		if err := s.checkRef(r, u, ref); err != nil {
			s.writeRefError(w, r, err)
			return
		}
	}
	if err := s.userdata.SetInList(r.Context(), u.ID, r.PathValue("list"), ref, in); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetList — GET /api/v1/me/lists/{list}
func (s *Server) handleGetList(w http.ResponseWriter, r *http.Request, u *auth.User) {
	items, err := s.userdata.List(r.Context(), u.ID, r.PathValue("list"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	refs := make([]userdata.Ref, len(items))
	for i, it := range items {
		refs[i] = it.Ref
	}
	cards := s.newResolver(r, u).cards(refs)
	s.loadUserState(r.Context(), u.ID).decorate(cards)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"items": cards})
}

// handleHistory — GET /api/v1/me/history
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, u *auth.User) {
	limit := intParam(r, "limit", 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	entries, err := s.userdata.History(r.Context(), u.ID, limit, intParam(r, "offset", 0))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	res := s.newResolver(r, u)
	type row struct {
		userdata.HistoryEntry
		Item *catalog.Card `json:"item"`
	}
	out := []row{}
	for _, e := range entries {
		if c, ok := res.card(e.Ref); ok {
			out = append(out, row{HistoryEntry: e, Item: &c})
		}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"entries": out})
}

// handleDeleteHistory — DELETE /api/v1/me/history/{entryID}
func (s *Server) handleDeleteHistory(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id := int64(atoiSafe(r.PathValue("entryID")))
	if err := s.userdata.DeleteHistory(r.Context(), u.ID, id); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetPreferences — GET /api/v1/me/preferences
func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request, u *auth.User) {
	pb, err := s.userdata.Playback(r.Context(), u.ID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	ui, err := s.userdata.Interface(r.Context(), u.ID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"playback": pb, "interface": ui})
}

// handlePutPreferences — PUT /api/v1/me/preferences {playback?, interface?}
func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Playback  *userdata.Playback  `json:"playback"`
		Interface *userdata.Interface `json:"interface"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if body.Playback != nil {
		if _, err := s.userdata.SetPlayback(r.Context(), u.ID, *body.Playback); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
	}
	if body.Interface != nil {
		if _, err := s.userdata.SetInterface(r.Context(), u.ID, *body.Interface); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
	}
	s.handleGetPreferences(w, r, u)
}

// Collections.

// smartCollections are the predefined collections every person has.
var smartCollections = []struct{ ID, Name, Description string }{
	{"recently_added", "Recently Added", "The newest movies and shows in your libraries."},
	{"favorites", "Favourites", "Everything you marked as a favourite."},
	{"most_watched", "Most Watched", "What you come back to most."},
	{"unwatched", "Unwatched", "Movies and shows you have not seen yet."},
	{"recently_played", "Recently Played", "What you watched lately."},
	{"watchlist", "Watchlist", "Saved for later."},
}

func (s *Server) smartItems(r *http.Request, u *auth.User, id string) ([]catalog.Card, bool) {
	ctx := r.Context()
	res := s.newResolver(r, u)
	us := s.loadUserState(ctx, u.ID)
	switch id {
	case "recently_added":
		all := append(res.allMovies(), res.allShows()...)
		us.decorate(all)
		sortCards(all, "added", "")
		return limitCards(all, 60), true
	case "unwatched":
		all := append(res.allMovies(), res.allShows()...)
		us.decorate(all)
		all = applyWatched(all, "unwatched")
		sortCards(all, "title", "")
		return all, true
	case "favorites", "watchlist":
		list := "favorite"
		if id == "watchlist" {
			list = "watchlist"
		}
		items, _ := s.userdata.List(ctx, u.ID, list)
		refs := make([]userdata.Ref, len(items))
		for i, it := range items {
			refs[i] = it.Ref
		}
		cards := res.cards(refs)
		us.decorate(cards)
		return cards, true
	case "most_watched":
		counts, _ := s.userdata.PlayCounts(ctx, u.ID, 60)
		refs := make([]userdata.Ref, len(counts))
		for i, c := range counts {
			refs[i] = c.Ref
		}
		cards := res.cards(refs)
		us.decorate(cards)
		return cards, true
	case "recently_played":
		all := append(res.allMovies(), res.allShows()...)
		us.decorate(all)
		var played []catalog.Card
		for _, c := range all {
			if c.State != nil && c.State.LastPlayedAt != "" {
				played = append(played, c)
			}
		}
		sortCards(played, "played", "")
		return limitCards(played, 60), true
	}
	return nil, false
}

// handleListCollections — GET /api/v1/collections
func (s *Server) handleListCollections(w http.ResponseWriter, r *http.Request, u *auth.User) {
	cols, err := s.userdata.Collections(r.Context(), u.ID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	res := s.newResolver(r, u)
	type colOut struct {
		userdata.Collection
		Covers []catalog.Card `json:"covers"`
	}
	out := []colOut{}
	for _, c := range cols {
		out = append(out, colOut{Collection: c, Covers: res.cards(c.Preview)})
	}
	type smartOut struct {
		ID          string         `json:"id"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		ItemCount   int            `json:"item_count"`
		Covers      []catalog.Card `json:"covers"`
	}
	smart := []smartOut{}
	if r.URL.Query().Get("smart") != "0" {
		for _, sc := range smartCollections {
			items, _ := s.smartItems(r, u, sc.ID)
			smart = append(smart, smartOut{ID: sc.ID, Name: sc.Name, Description: sc.Description,
				ItemCount: len(items), Covers: limitCards(items, 4)})
		}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"collections": out, "smart": smart})
}

// handleCreateCollection — POST /api/v1/collections {name, description, items?}
func (s *Server) handleCreateCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Items       []userdata.Ref `json:"items"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	c, err := s.userdata.CreateCollection(r.Context(), u.ID, body.Name, body.Description)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if len(body.Items) > 0 {
		if err := s.userdata.AddToCollection(r.Context(), u.ID, c.ID, body.Items); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
	}
	writeJSON(w, s.logger, http.StatusCreated, c)
}

// handleGetCollection — GET /api/v1/collections/{cid}
func (s *Server) handleGetCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	cid := r.PathValue("cid")
	for _, sc := range smartCollections {
		if sc.ID == cid {
			items, _ := s.smartItems(r, u, cid)
			writeJSON(w, s.logger, http.StatusOK, map[string]any{
				"collection": map[string]any{"id": sc.ID, "name": sc.Name, "description": sc.Description,
					"item_count": len(items), "smart": true},
				"items": items,
			})
			return
		}
	}
	c, err := s.userdata.Collection(r.Context(), u.ID, cid)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	members, err := s.userdata.CollectionItems(r.Context(), u.ID, cid)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	refs := make([]userdata.Ref, len(members))
	for i, m := range members {
		refs[i] = m.Ref
	}
	cards := s.newResolver(r, u).cards(refs)
	s.loadUserState(r.Context(), u.ID).decorate(cards)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"collection": c, "items": cards})
}

// handleUpdateCollection — PATCH /api/v1/collections/{cid}
func (s *Server) handleUpdateCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := s.userdata.UpdateCollection(r.Context(), u.ID, r.PathValue("cid"), body.Name, body.Description); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	c, err := s.userdata.Collection(r.Context(), u.ID, r.PathValue("cid"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, c)
}

// handleDeleteCollection — DELETE /api/v1/collections/{cid}
func (s *Server) handleDeleteCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if err := s.userdata.DeleteCollection(r.Context(), u.ID, r.PathValue("cid")); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	_ = os.Remove(s.userPosterPath("collection", r.PathValue("cid")))
	w.WriteHeader(http.StatusNoContent)
}

// handleDuplicateCollection — POST /api/v1/collections/{cid}/duplicate {name}
func (s *Server) handleDuplicateCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name string `json:"name"`
	}
	_ = readJSON(w, r, &body)
	c, err := s.userdata.DuplicateCollection(r.Context(), u.ID, r.PathValue("cid"), body.Name)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, c)
}

// handleCollectionItems — POST (add) / DELETE (remove) /api/v1/collections/{cid}/items {items}
func (s *Server) handleCollectionItems(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Items []userdata.Ref `json:"items"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	cid := r.PathValue("cid")
	var err error
	if r.Method == http.MethodDelete {
		for _, ref := range body.Items {
			if err = s.userdata.RemoveFromCollection(r.Context(), u.ID, cid, ref); err != nil {
				break
			}
		}
	} else {
		for _, ref := range body.Items {
			if err = s.checkRef(r, u, ref); err != nil {
				s.writeRefError(w, r, err)
				return
			}
		}
		err = s.userdata.AddToCollection(r.Context(), u.ID, cid, body.Items)
	}
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReorderCollection — PUT /api/v1/collections/{cid}/order {items}
func (s *Server) handleReorderCollection(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Items []userdata.Ref `json:"items"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := s.userdata.ReorderCollection(r.Context(), u.ID, r.PathValue("cid"), body.Items); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCollectionQueue — GET /api/v1/collections/{cid}/queue
//
// The collection as a play queue: its movies, and every episode of its
// shows, in the collection's order.
func (s *Server) handleCollectionQueue(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var refs []refBody
	cid := r.PathValue("cid")
	if items, ok := s.smartItems(r, u, cid); ok {
		for _, c := range items {
			refs = append(refs, refBody{LibraryID: c.LibraryID, Type: c.Type, ID: c.ID})
		}
	} else {
		members, err := s.userdata.CollectionItems(r.Context(), u.ID, cid)
		if err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		for _, m := range members {
			refs = append(refs, refBody{LibraryID: m.LibraryID, Type: m.Type, ID: m.ID})
		}
	}
	queue, _, err := s.expandPlayable(r, u, refs)
	if err != nil {
		s.writeRefError(w, r, err)
		return
	}
	if queue == nil {
		queue = []userdata.Ref{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"queue": queue})
}

// User artwork (collection and playlist posters) lives in the server data
// directory, since collections can span libraries.
func (s *Server) userPosterPath(kind, id string) string {
	return filepath.Join(s.dataDir, "artwork", kind, id+".jpg")
}

const maxArtworkBytes = 15 << 20

func readImageUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxArtworkBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	ct := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(ct, "image/jpeg"):
		return data, ".jpg", nil
	case strings.HasPrefix(ct, "image/png"):
		return data, ".png", nil
	case strings.HasPrefix(ct, "image/webp"):
		return data, ".webp", nil
	}
	return nil, "", &catalog.ErrInvalid{Msg: "Artwork must be a JPEG, PNG or WebP image."}
}

// handleUserPoster — GET/PUT/DELETE /api/v1/{kind}/{cid}/poster
// (collections and playlists).
func (s *Server) handleUserPoster(kind string) func(http.ResponseWriter, *http.Request, *auth.User) {
	return func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		id := r.PathValue("cid")
		exists := func() error {
			if kind == "collection" {
				_, err := s.userdata.Collection(r.Context(), u.ID, id)
				return err
			}
			_, err := s.userdata.Playlist(r.Context(), u.ID, id)
			return err
		}
		if err := exists(); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		p := s.userPosterPath(kind, id)
		bump := func(d int) error {
			if kind == "collection" {
				return s.userdata.BumpCollectionPoster(r.Context(), u.ID, id, d)
			}
			return s.userdata.BumpPlaylistPoster(r.Context(), u.ID, id, d)
		}
		switch r.Method {
		case http.MethodGet:
			serveImageFile(w, r, p)
		case http.MethodPut:
			data, _, err := readImageUpload(w, r)
			if err != nil {
				s.writeCatalogError(w, r, err)
				return
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				s.writeCatalogError(w, r, err)
				return
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				s.writeCatalogError(w, r, err)
				return
			}
			_ = bump(1)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			_ = os.Remove(p)
			_ = bump(-1)
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// Playlists.

type playlistOut struct {
	userdata.Playlist
	Covers []catalog.Card `json:"covers"`
}

// handleListPlaylists — GET /api/v1/playlists
func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request, u *auth.User) {
	pls, err := s.userdata.Playlists(r.Context(), u.ID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	res := s.newResolver(r, u)
	out := []playlistOut{}
	for _, p := range pls {
		out = append(out, playlistOut{Playlist: p, Covers: res.cards(p.Preview)})
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"playlists": out})
}

// handleCreatePlaylist — POST /api/v1/playlists {name, description, items?}
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Items       []refBody `json:"items"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	p, err := s.userdata.CreatePlaylist(r.Context(), u.ID, body.Name, body.Description)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if len(body.Items) > 0 {
		refs, _, err := s.expandPlayable(r, u, body.Items)
		if err != nil {
			s.writeRefError(w, r, err)
			return
		}
		if err := s.userdata.AddToPlaylist(r.Context(), u.ID, p.ID, refs); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
	}
	writeJSON(w, s.logger, http.StatusCreated, p)
}

// handleGetPlaylist — GET /api/v1/playlists/{cid}
func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	p, err := s.userdata.Playlist(r.Context(), u.ID, r.PathValue("cid"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	entries, err := s.userdata.PlaylistEntries(r.Context(), u.ID, p.ID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	res := s.newResolver(r, u)
	us := s.loadUserState(r.Context(), u.ID)
	type entryOut struct {
		userdata.PlaylistEntry
		Item *catalog.Card `json:"item"`
	}
	out := []entryOut{}
	for _, e := range entries {
		eo := entryOut{PlaylistEntry: e}
		if c, ok := res.card(e.Ref); ok {
			c.State = us.stateFor(c.LibraryID, c.Type, c.ID, 0)
			eo.Item = &c
		}
		out = append(out, eo)
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"playlist": p, "entries": out})
}

// handleUpdatePlaylist — PATCH /api/v1/playlists/{cid}
func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body userdata.PlaylistPatch
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := s.userdata.UpdatePlaylist(r.Context(), u.ID, r.PathValue("cid"), body); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	p, err := s.userdata.Playlist(r.Context(), u.ID, r.PathValue("cid"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, p)
}

// handleDeletePlaylist — DELETE /api/v1/playlists/{cid}
func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if err := s.userdata.DeletePlaylist(r.Context(), u.ID, r.PathValue("cid")); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	_ = os.Remove(s.userPosterPath("playlist", r.PathValue("cid")))
	w.WriteHeader(http.StatusNoContent)
}

// handlePlaylistItems — POST /api/v1/playlists/{cid}/items {items} adds
// (seasons, shows and collections expand to their episodes/movies);
// DELETE {entry_ids} removes.
func (s *Server) handlePlaylistItems(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Items      []refBody `json:"items"`
		EntryIDs   []string  `json:"entry_ids"`
		Collection string    `json:"collection"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	cid := r.PathValue("cid")
	if r.Method == http.MethodDelete {
		if err := s.userdata.RemoveFromPlaylist(r.Context(), u.ID, cid, body.EntryIDs); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	items := body.Items
	if body.Collection != "" {
		members, err := s.userdata.CollectionItems(r.Context(), u.ID, body.Collection)
		if err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		for _, m := range members {
			items = append(items, refBody{LibraryID: m.LibraryID, Type: m.Type, ID: m.ID})
		}
	}
	refs, _, err := s.expandPlayable(r, u, items)
	if err != nil {
		s.writeRefError(w, r, err)
		return
	}
	if err := s.userdata.AddToPlaylist(r.Context(), u.ID, cid, refs); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"added": len(refs)})
}

// handleReorderPlaylist — PUT /api/v1/playlists/{cid}/order {entry_ids}
func (s *Server) handleReorderPlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		EntryIDs []string `json:"entry_ids"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := s.userdata.ReorderPlaylist(r.Context(), u.ID, r.PathValue("cid"), body.EntryIDs); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleShufflePlaylist — POST /api/v1/playlists/{cid}/shuffle
func (s *Server) handleShufflePlaylist(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if err := s.userdata.ShufflePlaylist(r.Context(), u.ID, r.PathValue("cid")); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
