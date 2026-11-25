package httpapi

import (
	"net/http"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
)

// registerMediaRoutes adds the movie and TV server API: browsing, playback,
// per-person lists, collections and playlists, and metadata management.
func (s *Server) registerMediaRoutes(mux *http.ServeMux) {
	h := func(pattern string, policy AllowFunc, fn func(http.ResponseWriter, *http.Request, *auth.User)) {
		mux.Handle(pattern, s.withAuth(policy, fn))
	}

	// Browsing.
	h("GET /api/v1/home", allowAny, s.handleHome)
	h("GET /api/v1/movies", allowAny, s.handleListMovies)
	h("GET /api/v1/shows", allowAny, s.handleListShows)
	h("GET /api/v1/facets", allowAny, s.handleFacets)
	h("GET /api/v1/search", allowAny, s.handleSearch)
	h("GET /api/v1/libraries/{id}/movies/{movieID}", allowAny, s.handleGetMovie)
	h("GET /api/v1/libraries/{id}/shows/{showID}", allowAny, s.handleGetShow)
	h("GET /api/v1/libraries/{id}/seasons/{seasonID}/episodes", allowAny, s.handleSeasonEpisodes)
	h("GET /api/v1/libraries/{id}/episodes/{episodeID}", allowAny, s.handleGetEpisode)
	h("GET /api/v1/libraries/{id}/people/{personID}", allowAny, s.handleGetPerson)
	h("GET /api/v1/libraries/{id}/people/{personID}/image", allowAny, s.handlePersonImage)
	h("GET /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}", allowAny, s.handleGetArtwork)
	h("GET /api/v1/libraries/{id}/stats", allowAny, s.handleLibraryStats)
	h("GET /api/v1/libraries/{id}/catalog/duplicates", allowAny, s.handleCatalogDuplicates)

	// Playback.
	h("GET /api/v1/libraries/{id}/play/{type}/{itemID}", allowAny, s.handlePlayInfo)
	h("GET /api/v1/libraries/{id}/files/{fileID}/decide", allowAny, s.handleDecide)
	h("GET /api/v1/libraries/{id}/files/{fileID}/direct", allowAny, s.handleDirect)
	h("GET /api/v1/libraries/{id}/files/{fileID}/stream", allowAny, s.handleStream)
	h("GET /api/v1/libraries/{id}/files/{fileID}/keyframe", allowAny, s.handleKeyframe)
	h("GET /api/v1/libraries/{id}/files/{fileID}/subtitles/{track}", allowAny, s.handleSubtitle)
	h("POST /api/v1/playback/progress", allowAny, s.handleProgress)
	h("DELETE /api/v1/playback/progress", allowAny, s.handleClearProgress)
	h("POST /api/v1/playback/played", allowAny, s.handleSetPlayed)
	h("POST /api/v1/playback/stop", allowAny, s.handleStopStream)
	h("GET /api/v1/streams", allowAdmin, s.handleActiveStreams)

	// The person's own lists, history and preferences.
	h("GET /api/v1/me/lists/{list}", allowAny, s.handleGetList)
	h("PUT /api/v1/me/lists/{list}/{lib}/{type}/{itemID}", allowAny, s.handleSetListItem)
	h("DELETE /api/v1/me/lists/{list}/{lib}/{type}/{itemID}", allowAny, s.handleSetListItem)
	h("GET /api/v1/me/history", allowAny, s.handleHistory)
	h("DELETE /api/v1/me/history/{entryID}", allowAny, s.handleDeleteHistory)
	h("GET /api/v1/me/preferences", allowAny, s.handleGetPreferences)
	h("PUT /api/v1/me/preferences", allowAny, s.handlePutPreferences)

	// Collections.
	h("GET /api/v1/collections", allowAny, s.handleListCollections)
	h("POST /api/v1/collections", allowAny, s.handleCreateCollection)
	h("GET /api/v1/collections/{cid}", allowAny, s.handleGetCollection)
	h("PATCH /api/v1/collections/{cid}", allowAny, s.handleUpdateCollection)
	h("DELETE /api/v1/collections/{cid}", allowAny, s.handleDeleteCollection)
	h("POST /api/v1/collections/{cid}/duplicate", allowAny, s.handleDuplicateCollection)
	h("POST /api/v1/collections/{cid}/items", allowAny, s.handleCollectionItems)
	h("DELETE /api/v1/collections/{cid}/items", allowAny, s.handleCollectionItems)
	h("PUT /api/v1/collections/{cid}/order", allowAny, s.handleReorderCollection)
	h("GET /api/v1/collections/{cid}/queue", allowAny, s.handleCollectionQueue)
	poster := s.handleUserPoster("collection")
	h("GET /api/v1/collections/{cid}/poster", allowAny, poster)
	h("PUT /api/v1/collections/{cid}/poster", allowAny, poster)
	h("DELETE /api/v1/collections/{cid}/poster", allowAny, poster)

	// Playlists.
	h("GET /api/v1/playlists", allowAny, s.handleListPlaylists)
	h("POST /api/v1/playlists", allowAny, s.handleCreatePlaylist)
	h("GET /api/v1/playlists/{cid}", allowAny, s.handleGetPlaylist)
	h("PATCH /api/v1/playlists/{cid}", allowAny, s.handleUpdatePlaylist)
	h("DELETE /api/v1/playlists/{cid}", allowAny, s.handleDeletePlaylist)
	h("POST /api/v1/playlists/{cid}/items", allowAny, s.handlePlaylistItems)
	h("DELETE /api/v1/playlists/{cid}/items", allowAny, s.handlePlaylistItems)
	h("PUT /api/v1/playlists/{cid}/order", allowAny, s.handleReorderPlaylist)
	h("POST /api/v1/playlists/{cid}/shuffle", allowAny, s.handleShufflePlaylist)
	ppos := s.handleUserPoster("playlist")
	h("GET /api/v1/playlists/{cid}/poster", allowAny, ppos)
	h("PUT /api/v1/playlists/{cid}/poster", allowAny, ppos)
	h("DELETE /api/v1/playlists/{cid}/poster", allowAny, ppos)

	// Metadata and library management. Content edits are gated per library
	// by the edit capability inside each handler.
	h("PATCH /api/v1/libraries/{id}", allowAdmin, s.handleUpdateLibrary)
	h("GET /api/v1/settings/metadata", allowAdmin, s.handleGetMetadataSettings)
	h("PUT /api/v1/settings/metadata", allowAdmin, s.handlePutMetadataSettings)
	h("POST /api/v1/settings/metadata/test", allowAdmin, s.handleTestMetadata)
	h("GET /api/v1/metadata/search", allowAny, s.handleProviderSearch)
	h("POST /api/v1/libraries/{id}/metadata/refresh", allowAny, s.handleRefreshMetadata)
	h("POST /api/v1/libraries/{id}/catalog/sync", allowAny, s.handleRescanCatalog)
	h("GET /api/v1/libraries/{id}/catalog/unassigned", allowAny, s.handleUnassigned)
	h("PATCH /api/v1/libraries/{id}/items/{type}/{itemID}", allowAny, s.handleEditItem)
	h("POST /api/v1/libraries/{id}/items/{type}/{itemID}/unlock", allowAny, s.handleUnlockFields)
	h("POST /api/v1/libraries/{id}/items/{type}/{itemID}/match", allowAny, s.handleMatch)
	h("POST /api/v1/libraries/{id}/items/{type}/{itemID}/unmatch", allowAny, s.handleUnmatch)
	h("PUT /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}", allowAny, s.handlePutArtwork)
	h("DELETE /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}", allowAny, s.handlePutArtwork)
	h("POST /api/v1/libraries/{id}/art/{type}/{itemID}/{kind}/generate", allowAny, s.handleGenerateArtwork)
	h("POST /api/v1/libraries/{id}/movies/{movieID}/files", allowAny, s.handleAttachFile)
	h("POST /api/v1/libraries/{id}/movies/{movieID}/merge", allowAny, s.handleMergeMovies)
	h("PUT /api/v1/libraries/{id}/files/{fileID}/edition", allowAny, s.handleFileEdition)
	h("POST /api/v1/libraries/{id}/files/{fileID}/split", allowAny, s.handleSplitFile)
	h("POST /api/v1/libraries/{id}/files/{fileID}/release", allowAny, s.handleReleaseFile)
	h("POST /api/v1/libraries/{id}/episodes/{episodeID}/move", allowAny, s.handleMoveEpisode)
	h("PUT /api/v1/libraries/{id}/shows/{showID}/order", allowAny, s.handleReorderSeasons)
	h("PUT /api/v1/libraries/{id}/seasons/{seasonID}/order", allowAny, s.handleReorderEpisodes)
	h("POST /api/v1/libraries/{id}/files/{fileID}/subtitles", allowAny, s.handleUploadSubtitle)
	h("PATCH /api/v1/libraries/{id}/subtitles/{subID}", allowAny, s.handleUpdateSubtitle)
	h("PUT /api/v1/libraries/{id}/subtitles/{subID}/content", allowAny, s.handleReplaceSubtitle)
	h("POST /api/v1/libraries/{id}/subtitles/{subID}/rename", allowAny, s.handleRenameSubtitle)
	h("DELETE /api/v1/libraries/{id}/subtitles/{subID}", allowAny, s.handleDeleteSubtitle)
}
