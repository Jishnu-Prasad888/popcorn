package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/authz"
	"github.com/Jishnu-Prasad888/popcorn/internal/backups"
	"github.com/Jishnu-Prasad888/popcorn/internal/crypto"
	"github.com/Jishnu-Prasad888/popcorn/internal/indexer"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/mediajobs"
	"github.com/Jishnu-Prasad888/popcorn/internal/metrics"
	"github.com/Jishnu-Prasad888/popcorn/internal/stream"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// Server is the Popcorn HTTP application. It is a small composition of the
// routes and shared dependencies; individual handlers stay in their own
// files.
type Server struct {
	logger         *slog.Logger
	db             *sql.DB
	web            http.Handler
	auth           *auth.Service
	authz          *authz.Service
	libraries      *library.Manager
	indexer        *indexer.IndexManager
	backups        *backups.Manager
	keys           *crypto.Keys
	secureCookies  bool
	ratelimit      *rateLimiter
	maxUploadBytes int64
	metrics        *metrics.Registry
	thumbs         *thumbGate
	userdata       *userdata.Store
	mediaSettings  *mediajobs.Settings
	transcoder     *stream.Transcoder
	dataDir        string
	// ready gates the /ready endpoint: true once startup completes, flipped
	// false by MarkNotReady before graceful shutdown so proxies drain.
	ready atomic.Bool
}

// Dependencies are the services the HTTP layer needs. Keeping them explicit
// here makes the API testable in isolation and prevents hidden globals.
type Dependencies struct {
	Logger *slog.Logger
	DB     *sql.DB
	// Auth resolves principals from sessions. It may be nil, in which case the
	// protected authentication and user-management endpoints return
	// UNAUTHORIZED.
	Auth *auth.Service
	// Authz evaluates resource-based permissions and public shares
	// (ADR-0005). It may be nil, in which case every library content route
	// remains admin-only (the pre-Phase-9 gate).
	Authz *authz.Service
	// Libraries registers and reconciles storage libraries. It may be nil, in
	// which case the library-management endpoints return INTERNAL.
	Libraries *library.Manager
	// Indexer manages per-library scan jobs and index status. It may be nil,
	// in which case the indexing endpoints return SERVICE_UNAVAILABLE.
	Indexer *indexer.IndexManager
	// Backups runs server and library backups. It may be nil, in which case
	// the backup-management endpoints return SERVICE_UNAVAILABLE.
	Backups *backups.Manager
	// Keys is the optional at-rest encryption key (Phase 13) used to seal the
	// .popcorn identity and thumbnails. It may be nil or disabled, in which
	// case metadata is written in the clear.
	Keys *crypto.Keys
	// SecureCookies forces the Secure flag on session cookies even when the
	// server did not observe TLS (e.g. behind a TLS-terminating proxy).
	SecureCookies bool
	// WebUI serves the embedded (or overridden) frontend at "/". It may be
	// nil, in which case API routes still work but the web UI is unavailable.
	WebUI http.Handler
	// MaxUploadBytes caps the total size of single upload bodies; values <= 0
	// fall back to media.DefaultMaxUploadBytes.
	MaxUploadBytes int64
	// Metrics records per-request counters for /api/v1/metrics. It may be
	// nil, in which case the metrics endpoint reports NOT_FOUND and no
	// request statistics are collected.
	Metrics *metrics.Registry
	// ThumbnailWorkers caps thumbnails generated inside requests at once
	// (for files the background worker has not reached yet). Values <= 0
	// mean 2.
	ThumbnailWorkers int
	// UserData holds per-person playback state, lists, collections,
	// playlists and preferences.
	UserData *userdata.Store
	// MediaSettings are the metadata provider settings.
	MediaSettings *mediajobs.Settings
	// Transcoder remuxes and transcodes video for the browser. It may be
	// nil, in which case only direct play works.
	Transcoder *stream.Transcoder
	// DataDir is the server data directory (collection and playlist artwork).
	DataDir string
}

// New returns a Server built from the given dependencies. The caller must
// call Handler() to obtain the http.Handler.
func New(deps Dependencies) *Server {
	s := &Server{
		logger:         deps.Logger,
		db:             deps.DB,
		web:            deps.WebUI,
		auth:           deps.Auth,
		authz:          deps.Authz,
		libraries:      deps.Libraries,
		indexer:        deps.Indexer,
		backups:        deps.Backups,
		keys:           deps.Keys,
		secureCookies:  deps.SecureCookies,
		ratelimit:      newRateLimiter(),
		maxUploadBytes: deps.MaxUploadBytes,
		metrics:        deps.Metrics,
		thumbs:         newThumbGate(thumbWorkers(deps.ThumbnailWorkers)),
		userdata:       deps.UserData,
		mediaSettings:  deps.MediaSettings,
		transcoder:     deps.Transcoder,
		dataDir:        deps.DataDir,
	}
	s.ready.Store(true)
	return s
}

func thumbWorkers(n int) int {
	if n <= 0 {
		return 2
	}
	return n
}

// MarkNotReady flips the /ready endpoint to 503. Call it before the graceful
// shutdown begins so load balancers and orchestrators stop routing traffic to
// this instance while its remaining requests drain.
func (s *Server) MarkNotReady() {
	s.ready.Store(false)
}

// Handler builds the fully-middleware-wrapped http.Handler for this Server.
//
// Route policy: everything under /api/v1/ is registered explicitly; health,
// version, auth bootstrap/status/login are public, and the remaining auth and
// user-management routes require a session (admin role where noted). Unknown
// /api/ routes still receive the JSON 404 envelope.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/ready", s.handleReady)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/metrics", s.handleMetrics)

	// Public authentication surface.
	mux.HandleFunc("GET /api/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", s.handleBootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Session-required surface.
	mux.HandleFunc("POST /api/v1/auth/logout", s.requireSession(s.handleLogout))
	mux.Handle("GET /api/v1/auth/me", s.withAuth(allowAny, s.handleMe))

	mux.Handle("GET /api/v1/system/capabilities", s.withAuth(allowAny, s.handleCapabilities))

	// Admin surface.
	mux.Handle("GET /api/v1/users", s.withAuth(allowAdmin, s.handleListUsers))
	mux.Handle("POST /api/v1/users", s.withAuth(allowAdmin, s.handleCreateUser))
	mux.Handle("PATCH /api/v1/users/{id}", s.withAuth(allowAdmin, s.handleUpdateUser))
	mux.Handle("POST /api/v1/users/{id}/sessions/revoke", s.withAuth(allowAdmin, s.handleRevokeUserSessions))

	// Library surface. Listing is open to any session and filtered by the
	// caller's read capability, so a member can find the libraries it was
	// granted; registering, probing, refreshing, and unregistering are
	// server-level administration and stay admin-only. Resource-based
	// authorization governs access to content within a library.
	if s.libraries != nil {
		mux.Handle("GET /api/v1/libraries", s.withAuth(allowAny, s.handleListLibraries))
		mux.Handle("POST /api/v1/libraries", s.withAuth(allowAdmin, s.handleCreateLibrary))
		mux.Handle("POST /api/v1/libraries/probe", s.withAuth(allowAdmin, s.handleProbeLibrary))
		mux.Handle("GET /api/v1/fs/dirs", s.withAuth(allowAdmin, s.handleBrowseDirs))
		mux.Handle("GET /api/v1/libraries/{id}", s.withAuth(allowAny, s.handleGetLibrary))
		mux.Handle("POST /api/v1/libraries/{id}/refresh", s.withAuth(allowAdmin, s.handleRefreshLibrary))
		mux.Handle("DELETE /api/v1/libraries/{id}", s.withAuth(allowAdmin, s.handleDeleteLibrary))
		// Indexing surface (admin): scanning and index status are server
		// operations, not per-resource capabilities.
		mux.Handle("POST /api/v1/libraries/{id}/index", s.withAuth(allowAdmin, s.handleTriggerIndex))
		mux.Handle("GET /api/v1/libraries/{id}/index/status", s.withAuth(allowAdmin, s.handleIndexStatus))
		mux.Handle("GET /api/v1/libraries/{id}/index/progress", s.withAuth(allowAdmin, s.handleIndexProgress))
		mux.Handle("GET /api/v1/libraries/{id}/ignore", s.withAuth(allowAdmin, s.handleGetIgnoreRules))
		mux.Handle("PUT /api/v1/libraries/{id}/ignore", s.withAuth(allowAdmin, s.handlePutIgnoreRules))
		mux.Handle("GET /api/v1/libraries/{id}/fs", s.withAuth(allowAdmin, s.handleBrowseLibrary))

		// Permissions and shares administration (library scope).
		mux.Handle("GET /api/v1/libraries/{id}/permissions", s.withAuth(allowAny, s.handleListGrants))
		mux.Handle("POST /api/v1/libraries/{id}/permissions", s.withAuth(allowAny, s.handleCreateGrant))
		mux.Handle("DELETE /api/v1/libraries/{id}/permissions/{grantID}", s.withAuth(allowAny, s.handleRevokeGrant))
		mux.Handle("GET /api/v1/libraries/{id}/shares", s.withAuth(allowAny, s.handleListShares))
		mux.Handle("POST /api/v1/libraries/{id}/shares", s.withAuth(allowAny, s.handleCreateShare))
		mux.Handle("DELETE /api/v1/libraries/{id}/shares/{shareID}", s.withAuth(allowAny, s.handleRevokeShare))

		// Media/files surface. Capabilities are enforced per resource inside
		// each handler via requireCap.
		mux.Handle("GET /api/v1/libraries/{id}/files", s.withAuth(allowAny, s.handleListFiles))
		mux.Handle("GET /api/v1/libraries/{id}/files/counts", s.withAuth(allowAny, s.handleFileCounts))
		mux.Handle("GET /api/v1/libraries/{id}/files/duplicates", s.withAuth(allowAny, s.handleListDuplicates))
		mux.Handle("PUT /api/v1/libraries/{id}/duplicates/ignored/{hash}", s.withAuth(allowAny, s.handleIgnoreDuplicate))
		mux.Handle("DELETE /api/v1/libraries/{id}/duplicates/ignored/{hash}", s.withAuth(allowAny, s.handleUnignoreDuplicate))
		mux.Handle("GET /api/v1/libraries/{id}/files/{fileID}", s.withAuth(allowAny, s.handleGetFile))
		mux.Handle("GET /api/v1/libraries/{id}/files/{fileID}/download", s.withAuth(allowAny, s.handleDownloadFile))
		mux.Handle("POST /api/v1/libraries/{id}/files/upload", s.withAuth(allowAny, s.handleUploadFile))
		mux.Handle("POST /api/v1/libraries/{id}/files/{fileID}/rename", s.withAuth(allowAny, s.handleRenameFile))
		mux.Handle("POST /api/v1/libraries/{id}/files/{fileID}/move", s.withAuth(allowAny, s.handleMoveFile))
		mux.Handle("POST /api/v1/libraries/{id}/files/{fileID}/copy", s.withAuth(allowAny, s.handleCopyFile))
		mux.Handle("DELETE /api/v1/libraries/{id}/files/{fileID}", s.withAuth(allowAny, s.handleDeleteFile))
		mux.Handle("POST /api/v1/libraries/{id}/files/{fileID}/restore", s.withAuth(allowAny, s.handleRestoreFile))
		mux.Handle("DELETE /api/v1/libraries/{id}/files/{fileID}/permanent", s.withAuth(allowAny, s.handlePermanentDeleteFile))
		mux.Handle("GET /api/v1/libraries/{id}/files/{fileID}/metadata", s.withAuth(allowAny, s.handleGetFileMetadata))
		mux.Handle("GET /api/v1/libraries/{id}/files/{fileID}/thumbnail", s.withAuth(allowAny, s.handleGetThumbnail))
		mux.Handle("GET /api/v1/libraries/{id}/thumbnails", s.withAuth(allowAny, s.handleThumbnailBatch))
		mux.Handle("PUT /api/v1/libraries/{id}/files/{fileID}/thumbnail", s.withAuth(allowAny, s.handlePutThumbnail))
		mux.Handle("GET /api/v1/libraries/{id}/files/{fileID}/preview", s.withAuth(allowAny, s.handleGetPreview))

		// Folders and trash.
		mux.Handle("GET /api/v1/libraries/{id}/folders", s.withAuth(allowAny, s.handleListFolders))
		mux.Handle("POST /api/v1/libraries/{id}/folders", s.withAuth(allowAny, s.handleCreateFolder))
		mux.Handle("POST /api/v1/libraries/{id}/folders/move", s.withAuth(allowAny, s.handleMoveFolder))
		mux.Handle("POST /api/v1/libraries/{id}/folders/trash", s.withAuth(allowAny, s.handleTrashFolder))
		mux.Handle("GET /api/v1/libraries/{id}/folders/download", s.withAuth(allowAny, s.handleDownloadFolder))
		mux.Handle("GET /api/v1/libraries/{id}/trash", s.withAuth(allowAny, s.handleListTrash))

		s.registerMediaRoutes(mux)

		// Backups surface (admin). Backup policy is server-wide: where they
		// are written, when they run, and restore destinations. All serverside.
		if s.backups != nil {
			mux.Handle("POST /api/v1/backups", s.withAuth(allowAdmin, s.handleRunBackup))
			mux.Handle("GET /api/v1/backups", s.withAuth(allowAdmin, s.handleListBackups))
			mux.Handle("GET /api/v1/backups/settings", s.withAuth(allowAdmin, s.handleGetBackupSettings))
			mux.Handle("PUT /api/v1/backups/settings", s.withAuth(allowAdmin, s.handlePutBackupSettings))
			mux.Handle("POST /api/v1/backups/destination/test", s.withAuth(allowAdmin, s.handleTestBackupDestination))
			mux.Handle("GET /api/v1/backups/status", s.withAuth(allowAdmin, s.handleBackupStatus))
			mux.Handle("GET /api/v1/backups/drives", s.withAuth(allowAdmin, s.handleBackupDrives))
			mux.Handle("GET /api/v1/backups/{id}", s.withAuth(allowAdmin, s.handleGetBackup))
			mux.Handle("POST /api/v1/backups/{id}/verify", s.withAuth(allowAdmin, s.handleVerifyBackup))
			mux.Handle("POST /api/v1/backups/{id}/restore", s.withAuth(allowAdmin, s.handleRestoreBackup))
		}

	}

	mux.Handle("/api/", s.handleAPIUnknown())

	if s.web != nil {
		mux.Handle("/", s.web)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, s.logger, requestIDOrEmpty(r), http.StatusNotFound, CodeNotFound,
				"No web frontend is configured. Build the frontend or point POPCORN_WEB_DIST at a built web/dist directory.")
		})
	}

	return WithMiddleware(s.logger, metricsRecording(s.metrics, gzipJSON(mux)))
}

// requireSession wraps a plain http.HandlerFunc with authentication. The
// session is authenticated and discarded; handlers that need the principal use
// withAuth instead.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUser(w, r); !ok {
			return
		}
		next(w, r)
	}
}

// handleAPIUnknown returns a JSON 404 for any unmatched /api/ request,
// keeping API errors consistent regardless of the path.
func (s *Server) handleAPIUnknown() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorf(w, s.logger, requestIDOrEmpty(r), http.StatusNotFound, CodeNotFound,
			"No such API route: %s %s.", r.Method, r.URL.Path)
	})
}
