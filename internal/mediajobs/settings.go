package mediajobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/provider"
)

// MetadataSettings configure the online metadata providers. They are an
// administrator setting stored in the server database; environment
// variables give the defaults.
type MetadataSettings struct {
	TMDbKey  string `json:"tmdb_key"`
	OMDbKey  string `json:"omdb_key"`
	Language string `json:"language"` // TMDB language tag, e.g. en-US, hi-IN
	Region   string `json:"region"`   // age-rating country, e.g. US, IN
	// Auto fetches metadata for new items as soon as they are found.
	Auto bool `json:"auto"`
	// GenerateThumbs grabs frames for episodes and movies without artwork.
	GenerateThumbs bool `json:"generate_thumbs"`
}

const settingsKey = "metadata.settings"

// Settings loads and caches the metadata settings and builds enrichers from
// them.
type Settings struct {
	db       *sql.DB
	defaults MetadataSettings

	mu     sync.Mutex
	cached *MetadataSettings
}

// NewSettings returns a Settings store with environment defaults.
func NewSettings(db *sql.DB, defaults MetadataSettings) *Settings {
	if defaults.Language == "" {
		defaults.Language = "en-US"
	}
	if defaults.Region == "" {
		defaults.Region = "US"
	}
	return &Settings{db: db, defaults: defaults}
}

// Get returns the current settings.
func (s *Settings) Get(ctx context.Context) MetadataSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil {
		return *s.cached
	}
	out := s.defaults
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key = ?`, settingsKey).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &out)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out
	}
	if out.TMDbKey == "" {
		out.TMDbKey = s.defaults.TMDbKey
	}
	if out.OMDbKey == "" {
		out.OMDbKey = s.defaults.OMDbKey
	}
	s.cached = &out
	return out
}

// Set stores new settings.
func (s *Settings) Set(ctx context.Context, v MetadataSettings) error {
	v.TMDbKey = strings.TrimSpace(v.TMDbKey)
	v.OMDbKey = strings.TrimSpace(v.OMDbKey)
	if v.Language == "" {
		v.Language = "en-US"
	}
	if v.Region == "" {
		v.Region = "US"
	}
	v.Region = strings.ToUpper(v.Region)
	b, _ := json.Marshal(v)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO server_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingsKey, string(b), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	s.mu.Lock()
	s.cached = &v
	s.mu.Unlock()
	return nil
}

// Configured reports whether a metadata provider has an API key.
func (s *Settings) Configured(ctx context.Context) bool { return s.Get(ctx).TMDbKey != "" }

// Provider returns the configured metadata provider, or nil.
func (s *Settings) Provider(ctx context.Context) provider.Provider {
	v := s.Get(ctx)
	if v.TMDbKey == "" {
		return nil
	}
	return provider.NewTMDB(v.TMDbKey, v.Language, v.Region)
}

// Enricher returns an enricher for the current settings (its Provider is nil
// when none is configured).
func (s *Settings) Enricher(ctx context.Context, logger *slog.Logger) *catalog.Enricher {
	v := s.Get(ctx)
	e := &catalog.Enricher{Logger: logger}
	if v.TMDbKey != "" {
		e.Provider = provider.NewTMDB(v.TMDbKey, v.Language, v.Region)
	}
	if v.OMDbKey != "" {
		e.Ratings = provider.NewOMDb(v.OMDbKey)
	}
	return e
}
