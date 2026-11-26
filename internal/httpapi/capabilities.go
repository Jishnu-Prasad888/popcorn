package httpapi

import (
	"net/http"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/media"
	"github.com/Jishnu-Prasad888/popcorn/internal/metadata"
	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
)

// handleCapabilities — GET /api/v1/system/capabilities
// Reports optional host tools the server found, so the app can say what is
// missing. Without ffmpeg, video thumbnails depend on the browser decoding
// the video itself.
func (s *Server) handleCapabilities(w http.ResponseWriter, _ *http.Request, _ *auth.User) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"ffmpeg":   metadata.VideoFramesAvailable(),
		"ffprobe":  probe.Available(),
		"metadata": s.metadataConfigured(),
		// The largest single file the upload endpoint accepts, so the app
		// can refuse an oversized file before sending a byte of it.
		"max_upload_bytes": s.uploadLimit(),
	})
}

func (s *Server) uploadLimit() int64 {
	if s.maxUploadBytes <= 0 {
		return media.DefaultMaxUploadBytes
	}
	return s.maxUploadBytes
}
