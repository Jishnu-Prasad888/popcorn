package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/authz"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/jobs"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/mediajobs"
	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
	"github.com/Jishnu-Prasad888/popcorn/internal/provider"
)

func (s *Server) metadataConfigured() bool {
	return s.mediaSettings != nil && s.mediaSettings.Configured(context.Background())
}

// handleUpdateLibrary — PATCH /api/v1/libraries/{id} {name?, kind?} (admin)
//
// Changing the kind re-sorts the library's videos into movies and shows.
func (s *Server) handleUpdateLibrary(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Name *string `json:"name"`
		Kind *string `json:"kind"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	id := r.PathValue("id")
	lib, err := s.libraries.Get(actorCtx(r, u).Context(), id)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	if body.Name != nil {
		if err := s.libraries.Rename(r.Context(), id, *body.Name); err != nil {
			s.writeLibraryError(w, r, err)
			return
		}
	}
	if body.Kind != nil && *body.Kind != lib.Kind {
		if !library.ValidKind(*body.Kind) {
			s.badRequest(w, r, "Library kind must be movies, shows or mixed.")
			return
		}
		if err := s.libraries.SetKind(r.Context(), id, *body.Kind); err != nil {
			s.writeLibraryError(w, r, err)
			return
		}
		if s.indexer != nil {
			_, _ = s.indexer.Enqueue(r.Context(), lib.ID, lib.Root, jobs.KindCatalog, map[string]any{"reason": "kind"})
		}
	}
	lib, err = s.libraries.Get(r.Context(), id)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"library": toLibraryResponse(lib)})
}

type metadataSettingsOut struct {
	TMDbKeySet     bool   `json:"tmdb_key_set"`
	OMDbKeySet     bool   `json:"omdb_key_set"`
	TMDbKeyHint    string `json:"tmdb_key_hint,omitempty"`
	OMDbKeyHint    string `json:"omdb_key_hint,omitempty"`
	Language       string `json:"language"`
	Region         string `json:"region"`
	Auto           bool   `json:"auto"`
	GenerateThumbs bool   `json:"generate_thumbs"`
}

func keyHint(k string) string {
	if len(k) < 8 {
		return ""
	}
	return "…" + k[len(k)-4:]
}

func settingsOut(v mediajobs.MetadataSettings) metadataSettingsOut {
	return metadataSettingsOut{TMDbKeySet: v.TMDbKey != "", OMDbKeySet: v.OMDbKey != "",
		TMDbKeyHint: keyHint(v.TMDbKey), OMDbKeyHint: keyHint(v.OMDbKey), Language: v.Language,
		Region: v.Region, Auto: v.Auto, GenerateThumbs: v.GenerateThumbs}
}

// handleGetMetadataSettings — GET /api/v1/settings/metadata (admin)
//
// API keys are never sent back; only whether one is set.
func (s *Server) handleGetMetadataSettings(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if s.mediaSettings == nil {
		writeJSON(w, s.logger, http.StatusOK, metadataSettingsOut{})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, settingsOut(s.mediaSettings.Get(r.Context())))
}

// handlePutMetadataSettings — PUT /api/v1/settings/metadata (admin)
//
// A key left out (null) is kept; an empty string clears it.
func (s *Server) handlePutMetadataSettings(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if s.mediaSettings == nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable, "Metadata is not available.")
		return
	}
	var body struct {
		TMDbKey        *string `json:"tmdb_key"`
		OMDbKey        *string `json:"omdb_key"`
		Language       *string `json:"language"`
		Region         *string `json:"region"`
		Auto           *bool   `json:"auto"`
		GenerateThumbs *bool   `json:"generate_thumbs"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	v := s.mediaSettings.Get(r.Context())
	if body.TMDbKey != nil {
		v.TMDbKey = *body.TMDbKey
	}
	if body.OMDbKey != nil {
		v.OMDbKey = *body.OMDbKey
	}
	if body.Language != nil {
		v.Language = *body.Language
	}
	if body.Region != nil {
		v.Region = *body.Region
	}
	if body.Auto != nil {
		v.Auto = *body.Auto
	}
	if body.GenerateThumbs != nil {
		v.GenerateThumbs = *body.GenerateThumbs
	}
	if err := s.mediaSettings.Set(r.Context(), v); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, settingsOut(s.mediaSettings.Get(r.Context())))
}

// handleTestMetadata — POST /api/v1/settings/metadata/test (admin)
func (s *Server) handleTestMetadata(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if s.mediaSettings == nil {
		s.badRequest(w, r, "No TMDB API key is set.")
		return
	}
	p := s.mediaSettings.Provider(r.Context())
	if p == nil {
		s.badRequest(w, r, "No TMDB API key is set.")
		return
	}
	res, err := p.SearchMovies(r.Context(), "The Matrix", 1999)
	if err != nil {
		writeJSON(w, s.logger, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"ok": len(res) > 0})
}

// handleProviderSearch — GET /api/v1/metadata/search?type=movie|show&q=&year=
//
// Candidates for a manual match. A query that is an IMDb id (tt…) or a TMDB
// id (tmdb:123) is looked up directly.
func (s *Server) handleProviderSearch(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var p provider.Provider
	if s.mediaSettings != nil {
		p = s.mediaSettings.Provider(r.Context())
	}
	if p == nil {
		s.badRequest(w, r, "Set a TMDB API key in Settings → Metadata first.")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := r.URL.Query().Get("type")
	year := intParam(r, "year", 0)
	if q == "" {
		s.badRequest(w, r, "Enter a title to search for.")
		return
	}
	var results []provider.SearchResult
	var err error
	switch {
	case strings.HasPrefix(strings.ToLower(q), "tt"):
		var k, id string
		k, id, err = p.FindIMDb(r.Context(), strings.ToLower(q))
		if err == nil {
			results, err = s.lookupResult(r, p, k, id)
		}
	case strings.HasPrefix(strings.ToLower(q), "tmdb:"):
		k := "movie"
		if kind == "show" {
			k = "show"
		}
		results, err = s.lookupResult(r, p, k, strings.TrimSpace(q[5:]))
	case kind == "show":
		results, err = p.SearchShows(r.Context(), q, year)
	default:
		results, err = p.SearchMovies(r.Context(), q, year)
	}
	if errors.Is(err, provider.ErrNotFound) {
		results, err = nil, nil
	}
	if err != nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusBadGateway, CodeServiceUnavailable,
			"The metadata provider could not be reached: "+err.Error())
		return
	}
	if results == nil {
		results = []provider.SearchResult{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) lookupResult(r *http.Request, p provider.Provider, kind, id string) ([]provider.SearchResult, error) {
	if kind == "show" {
		sh, err := p.Show(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return []provider.SearchResult{{Provider: p.Name(), ID: sh.ProviderID, Kind: "show", Title: sh.Title,
			Year: sh.Year, Overview: sh.Overview, PosterURL: sh.Images.Poster}}, nil
	}
	m, err := p.Movie(r.Context(), id)
	if err != nil {
		return nil, err
	}
	return []provider.SearchResult{{Provider: p.Name(), ID: m.ProviderID, Kind: "movie", Title: m.Title,
		Year: m.Year, Overview: m.Overview, PosterURL: m.Images.Poster}}, nil
}

// handleMatch — POST /api/v1/libraries/{id}/items/{type}/{itemID}/match {provider_id}
//
// Matches a movie or show to a provider title and fetches its metadata now.
func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if s.mediaSettings == nil || !s.mediaSettings.Configured(r.Context()) {
		s.badRequest(w, r, "Set a TMDB API key in Settings → Metadata first.")
		return
	}
	e := s.mediaSettings.Enricher(r.Context(), s.logger)
	itemType, id := r.PathValue("type"), r.PathValue("itemID")
	var err error
	switch itemType {
	case "movie":
		err = e.EnrichMovie(r.Context(), st.DB(), popcornDirOf(lib), id, body.ProviderID)
	case "show":
		err = e.EnrichShow(r.Context(), st.DB(), popcornDirOf(lib), id, body.ProviderID)
	default:
		s.badRequest(w, r, "Only movies and shows can be matched.")
		return
	}
	if errors.Is(err, catalog.ErrNoMatch) || errors.Is(err, provider.ErrNotFound) {
		s.badRequest(w, r, "The provider has no such title.")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnmatch — POST /api/v1/libraries/{id}/items/{type}/{itemID}/unmatch
//
// Forgets an item's match and its provider metadata (edited fields stay).
func (s *Server) handleUnmatch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	if err := st.ResetMatch(r.Context(), r.PathValue("type"), r.PathValue("itemID")); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRefreshMetadata — POST /api/v1/libraries/{id}/metadata/refresh {items?, force?}
//
// Queues metadata fetches: the given items, or every movie and show of the
// library (with force, matched ones too; otherwise only unmatched).
func (s *Server) handleRefreshMetadata(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	var body struct {
		Items []catalog.ItemRef `json:"items"`
		Force bool              `json:"force"`
	}
	if err := readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		release()
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	items := body.Items
	if len(items) == 0 {
		f := catalog.Filter{Unmatched: !body.Force}
		movies, _ := st.Movies(r.Context(), f)
		shows, _ := st.Shows(r.Context(), f)
		for _, c := range movies {
			items = append(items, catalog.ItemRef{Type: "movie", ID: c.ID})
		}
		for _, c := range shows {
			items = append(items, catalog.ItemRef{Type: "show", ID: c.ID})
		}
	} else {
		body.Force = true
	}
	release()
	if s.indexer == nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable, "Background jobs are not running.")
		return
	}
	n := 0
	err := s.indexer.WithQueue(r.Context(), lib.ID, lib.Root, func(q *jobs.Queue) error {
		n = mediajobs.EnqueueMetadata(r.Context(), q, s.logger, items, body.Force)
		return nil
	})
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusAccepted, map[string]any{"queued": n})
}

// handleRescanCatalog — POST /api/v1/libraries/{id}/catalog/sync
//
// Re-sorts the library's files into movies and shows without a full scan.
func (s *Server) handleRescanCatalog(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, err := s.libraries.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	if !s.requireCap(w, r, u, authz.LibraryKey(lib.ID), authz.CapEdit) {
		return
	}
	if s.indexer == nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable, "Background jobs are not running.")
		return
	}
	if _, err := s.indexer.Enqueue(r.Context(), lib.ID, lib.Root, jobs.KindCatalog, map[string]any{"reason": "manual"}); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleEditItem — PATCH /api/v1/libraries/{id}/items/{type}/{itemID}
//
// Edits metadata fields. Edited fields are locked so a refresh keeps them.
func (s *Server) handleEditItem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var changes map[string]json.RawMessage
	if err := readJSON(w, r, &changes); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.Edit(r.Context(), r.PathValue("type"), r.PathValue("itemID"), changes); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnlockFields — POST /api/v1/libraries/{id}/items/{type}/{itemID}/unlock {fields}
func (s *Server) handleUnlockFields(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Fields []string `json:"fields"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.Unlock(r.Context(), r.PathValue("type"), r.PathValue("itemID"), body.Fields); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var artKinds = map[string]bool{"poster": true, "backdrop": true, "logo": true, "thumb": true}

// handlePutArtwork — PUT/DELETE /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}
func (s *Server) handlePutArtwork(w http.ResponseWriter, r *http.Request, u *auth.User) {
	itemType, kind := r.PathValue("type"), r.PathValue("kind")
	if !artTypes[itemType] || !artKinds[kind] {
		s.badRequest(w, r, "Unknown artwork.")
		return
	}
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	id := r.PathValue("itemID")
	if r.Method == http.MethodDelete {
		if err := st.RemoveArtwork(r.Context(), popcornDirOf(lib), itemType, id, kind); err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	data, ext, err := readImageUpload(w, r)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if err := st.SaveUploadedArtwork(r.Context(), popcornDirOf(lib), itemType, id, kind, ext, data); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGenerateArtwork — POST /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}/generate {at}
//
// Uses a frame of the video (at seconds) as the artwork.
func (s *Server) handleGenerateArtwork(w http.ResponseWriter, r *http.Request, u *auth.User) {
	itemType, kind := r.PathValue("type"), r.PathValue("kind")
	if !artTypes[itemType] || !artKinds[kind] {
		s.badRequest(w, r, "Unknown artwork.")
		return
	}
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		At float64 `json:"at"`
	}
	_ = readJSON(w, r, &body)
	if err := st.GenerateArtwork(r.Context(), lib.Root, popcornDirOf(lib), itemType, r.PathValue("itemID"), kind, body.At, true); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLibraryStats — GET /api/v1/libraries/{id}/stats
func (s *Server) handleLibraryStats(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	stats, err := st.Stats(r.Context())
	release()
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	var jobsOut map[string]int
	if s.indexer != nil {
		jobsOut, _ = s.indexer.JobCounts(r.Context(), lib.Root)
	}
	if jobsOut == nil {
		jobsOut = map[string]int{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"stats": stats, "jobs": jobsOut,
		"library": toLibraryResponse(lib), "ffprobe": probe.Available()})
}

// handleCatalogDuplicates — GET /api/v1/libraries/{id}/catalog/duplicates
//
// Titles with more than one file, and files that are the same video.
func (s *Server) handleCatalogDuplicates(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	d, err := st.Duplicates(r.Context())
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if d == nil {
		d = []catalog.Duplicate{}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"duplicates": d})
}

// handleUnassigned — GET /api/v1/libraries/{id}/catalog/unassigned
//
// Videos that were not recognised as a movie or an episode.
func (s *Server) handleUnassigned(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	files, err := st.UnassignedVideos(r.Context())
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	type out struct {
		ID      string `json:"id"`
		RelPath string `json:"rel_path"`
	}
	res := []out{}
	for _, f := range files {
		res = append(res, out{f.ID, f.RelPath})
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"files": res})
}

// handleAttachFile — POST /api/v1/libraries/{id}/movies/{movieID}/files {file_id, edition}
//
// Adds a file to a movie as another version (edition).
func (s *Server) handleAttachFile(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		FileID  string `json:"file_id"`
		Edition string `json:"edition"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.AttachFile(r.Context(), r.PathValue("movieID"), body.FileID, body.Edition); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFileEdition — PUT /api/v1/libraries/{id}/files/{fileID}/edition {edition}
func (s *Server) handleFileEdition(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Edition string `json:"edition"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.SetEdition(r.Context(), r.PathValue("fileID"), body.Edition); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSplitFile — POST /api/v1/libraries/{id}/files/{fileID}/split
//
// Makes a version of a movie its own movie.
func (s *Server) handleSplitFile(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	id, err := st.SplitFile(r.Context(), r.PathValue("fileID"))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"movie_id": id})
}

// handleReleaseFile — POST /api/v1/libraries/{id}/files/{fileID}/release
//
// Undoes a manual assignment so automatic matching places the file again.
func (s *Server) handleReleaseFile(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	err := st.ReleaseFile(r.Context(), r.PathValue("fileID"))
	release()
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if s.indexer != nil {
		_, _ = s.indexer.Enqueue(r.Context(), lib.ID, lib.Root, jobs.KindCatalog, map[string]any{"reason": "release"})
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMergeMovies — POST /api/v1/libraries/{id}/movies/{movieID}/merge {others}
func (s *Server) handleMergeMovies(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Others []string `json:"others"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.MergeMovies(r.Context(), r.PathValue("movieID"), body.Others); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMoveEpisode — POST /api/v1/libraries/{id}/episodes/{episodeID}/move {season, number}
func (s *Server) handleMoveEpisode(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Season int `json:"season"`
		Number int `json:"number"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := st.MoveEpisode(r.Context(), r.PathValue("episodeID"), body.Season, body.Number); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReorderSeasons — PUT /api/v1/libraries/{id}/shows/{showID}/order {ids}
func (s *Server) handleReorderSeasons(w http.ResponseWriter, r *http.Request, u *auth.User) {
	s.reorder(w, r, u, func(st *catalog.Store, ids []string) error {
		return st.ReorderSeasons(r.Context(), r.PathValue("showID"), ids)
	})
}

// handleReorderEpisodes — PUT /api/v1/libraries/{id}/seasons/{seasonID}/order {ids}
func (s *Server) handleReorderEpisodes(w http.ResponseWriter, r *http.Request, u *auth.User) {
	s.reorder(w, r, u, func(st *catalog.Store, ids []string) error {
		return st.ReorderEpisodes(r.Context(), r.PathValue("seasonID"), ids)
	})
}

func (s *Server) reorder(w http.ResponseWriter, r *http.Request, u *auth.User, fn func(*catalog.Store, []string) error) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := fn(st, body.IDs); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const maxSubtitleBytes = 10 << 20

var subtitleFormats = map[string]bool{"srt": true, "ass": true, "ssa": true, "vtt": true, "sub": true}

func subtitleInfoFromQuery(r *http.Request) catalog.SubtitleInfo {
	q := r.URL.Query()
	return catalog.SubtitleInfo{Language: probe.NormalizeLanguage(q.Get("language")), Title: q.Get("title"),
		Forced: q.Get("forced") == "1" || q.Get("forced") == "true", SDH: q.Get("sdh") == "1" || q.Get("sdh") == "true"}
}

// handleUploadSubtitle — POST /api/v1/libraries/{id}/files/{fileID}/subtitles?format=&language=&forced=&sdh=&title=
//
// Adds a subtitle file (the request body). It is kept in .popcorn, so the
// media folders are not touched.
func (s *Server) handleUploadSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	format := strings.ToLower(strings.TrimPrefix(r.URL.Query().Get("format"), "."))
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(r.URL.Query().Get("name"))), ".")
	}
	if !subtitleFormats[format] {
		s.badRequest(w, r, "Subtitles must be .srt, .ass, .ssa, .vtt or .sub files.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSubtitleBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		s.badRequest(w, r, "The subtitle file is empty or too large (10 MB at most).")
		return
	}
	id, err := st.AddUploadedSubtitle(r.Context(), popcornDirOf(lib), r.PathValue("fileID"), format, data, subtitleInfoFromQuery(r))
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, map[string]any{"id": id})
}

// handleUpdateSubtitle — PATCH /api/v1/libraries/{id}/subtitles/{subID} {language, title, forced, sdh}
func (s *Server) handleUpdateSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Language string `json:"language"`
		Title    string `json:"title"`
		Forced   bool   `json:"forced"`
		SDH      bool   `json:"sdh"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	info := catalog.SubtitleInfo{Language: probe.NormalizeLanguage(body.Language), Title: body.Title, Forced: body.Forced, SDH: body.SDH}
	if err := st.UpdateSubtitle(r.Context(), r.PathValue("subID"), info); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReplaceSubtitle — PUT /api/v1/libraries/{id}/subtitles/{subID}/content
func (s *Server) handleReplaceSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit)
	if !ok {
		return
	}
	defer release()
	r.Body = http.MaxBytesReader(w, r.Body, maxSubtitleBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		s.badRequest(w, r, "The subtitle file is empty or too large (10 MB at most).")
		return
	}
	if err := st.ReplaceSubtitle(r.Context(), lib.Root, popcornDirOf(lib), r.PathValue("subID"), data); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRenameSubtitle — POST /api/v1/libraries/{id}/subtitles/{subID}/rename {name}
//
// Renames a sidecar subtitle file next to its video.
func (s *Server) handleRenameSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit, authz.CapMove)
	if !ok {
		return
	}
	defer release()
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	p, err := st.RenameSidecar(r.Context(), lib.Root, r.PathValue("subID"), body.Name)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"path": p})
}

// handleDeleteSubtitle — DELETE /api/v1/libraries/{id}/subtitles/{subID}
//
// Removes an uploaded subtitle, or deletes a sidecar file (needs delete).
func (s *Server) handleDeleteSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u, authz.CapEdit, authz.CapDelete)
	if !ok {
		return
	}
	defer release()
	if err := st.RemoveSubtitle(r.Context(), lib.Root, popcornDirOf(lib), r.PathValue("subID")); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
