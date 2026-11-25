// Package config loads Popcorn server configuration from the environment.
//
// Configuration is intentionally environment-driven. A single self-hosted
// process should not require a config file to run; advanced options can be
// added later without breaking the base contract.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	// EnvPrefix is the prefix used for all Popcorn environment variables.
	EnvPrefix = "POPCORN"

	// DefaultHTTPAddr is the default listen address.
	DefaultHTTPAddr = "127.0.0.1:8715"

	// DefaultLogLevel is the default log verbosity.
	DefaultLogLevel = "info"
)

// Config holds the runtime configuration for the Popcorn server process.
type Config struct {
	// HTTPAddr is the listen address for the HTTP/JSON API and web UI.
	HTTPAddr string

	// DataDir is the server-level data directory. It holds the server
	// database and other server-scoped state. Library metadata lives inside
	// each library, never here.
	DataDir string

	// LogLevel is one of debug, info, warn, error.
	LogLevel string

	// WebDistDir overrides the embedded frontend with a directory of built
	// static assets. Empty means "use the embedded frontend". Useful for
	// development and for embedding tools that cannot use go:embed.
	WebDistDir string

	// CookieSecure forces the Secure flag on the session cookie. When false,
	// the flag is still set automatically for HTTPS requests observed by the
	// server. Set to true when Popcorn sits behind a TLS-terminating reverse
	// proxy.
	CookieSecure bool

	// MaxUploadBytes caps the size of a single upload body in bytes (Phase 16
	// hardening). Zero falls back to media.DefaultMaxUploadBytes (2 GiB).
	MaxUploadBytes int64

	// ScanIntervalSeconds is how often every online library is rescanned so
	// changes made outside Popcorn (a file manager, a sync tool) appear on their
	// own. Zero or negative disables the automatic rescan.
	ScanIntervalSeconds int

	// BackupDir is where library and server backups are written. Empty means
	// backups are disabled until configured.
	BackupDir string

	// BackupKeep is how many completed backups are retained. Older backups are
	// pruned automatically after each successful backup.
	BackupKeep int

	// BackupIntervalMinutes controls the scheduled backup cadence. Zero or
	// negative disables the scheduler (manual backups still work).
	BackupIntervalMinutes int

	// BackupPassphrase optionally encrypts every backup payload with the
	// shared AEAD kernel (AES-256-GCM, Phase 14). The passphrase is never
	// stored; a random per-backup salt is derived with argon2id. Backup
	// restore requires the same passphrase.
	BackupPassphrase string

	// EncryptionPassphrase optionally encrypts Popcorn-owned metadata at rest:
	// each library's .popcorn/library.json identity file and generated
	// thumbnails are sealed with AES-256-GCM under this passphrase. The
	// passphrase is never stored; the key is derived with argon2id using a
	// fixed protocol salt. Empty disables encryption. User media files are
	// never modified (see ADR-0004).
	EncryptionPassphrase string

	// MediaWorkers caps how many files each library's background worker
	// probes at once.
	MediaWorkers int

	// TMDbAPIKey and OMDbAPIKey are the default metadata provider keys
	// (POPCORN_TMDB_API_KEY, POPCORN_OMDB_API_KEY). An administrator can set
	// them on the Metadata settings page instead.
	TMDbAPIKey string
	OMDbAPIKey string
	// MetadataLanguage is the TMDB language tag (en-US, hi-IN, ja-JP…).
	MetadataLanguage string
	// MetadataRegion picks the country for age ratings (US, IN, GB…).
	MetadataRegion string

	// MaxTranscodes caps concurrent ffmpeg remux/transcode streams.
	MaxTranscodes int
	// VideoEncoder is the ffmpeg H.264 encoder: libx264 (default),
	// h264_vaapi, h264_qsv, h264_nvenc or h264_videotoolbox.
	VideoEncoder string
	// TranscodePreset is the x264 preset (veryfast by default).
	TranscodePreset string
	// HWDevice is the hardware device for VAAPI/QSV (e.g. /dev/dri/renderD128).
	HWDevice string
}

// Load builds a Config from the process environment and platform defaults.
//
// Popcorn settings follow the POPCORN_<NAME> naming scheme, e.g. POPCORN_HTTP_ADDR,
// POPCORN_DATA_DIR. Standard platform variables (XDG_DATA_HOME, HOME) are
// honored for the default data directory.
func Load() Config {
	return Config{
		HTTPAddr:              envOrDefault("HTTP_ADDR", DefaultHTTPAddr),
		DataDir:               dataDir(),
		LogLevel:              envOrDefaultTrim("LOG_LEVEL", DefaultLogLevel),
		WebDistDir:            envOrDefaultTrim("WEB_DIST", ""),
		CookieSecure:          envOrDefaultBool("COOKIE_SECURE", false),
		MaxUploadBytes:        envOrDefaultInt64("MAX_UPLOAD_BYTES", 2<<30),
		ScanIntervalSeconds:   envOrDefaultInt("SCAN_INTERVAL_SEC", 60),
		BackupDir:             envOrDefaultTrim("BACKUP_DIR", ""),
		BackupKeep:            envOrDefaultInt("BACKUP_KEEP", 4),
		BackupIntervalMinutes: envOrDefaultInt("BACKUP_INTERVAL_MIN", 0),
		BackupPassphrase:      os.Getenv(EnvPrefix + "_BACKUP_PASSPHRASE"),
		EncryptionPassphrase:  os.Getenv(EnvPrefix + "_ENCRYPTION_PASSPHRASE"),
		MediaWorkers:          envOrDefaultInt("MEDIA_WORKERS", defaultMediaWorkers()),
		TMDbAPIKey:            envOrDefaultTrim("TMDB_API_KEY", ""),
		OMDbAPIKey:            envOrDefaultTrim("OMDB_API_KEY", ""),
		MetadataLanguage:      envOrDefault("METADATA_LANGUAGE", "en-US"),
		MetadataRegion:        envOrDefault("METADATA_REGION", "US"),
		MaxTranscodes:         envOrDefaultInt("MAX_TRANSCODES", 3),
		VideoEncoder:          envOrDefault("VIDEO_ENCODER", "libx264"),
		TranscodePreset:       envOrDefault("TRANSCODE_PRESET", "veryfast"),
		HWDevice:              envOrDefault("HW_DEVICE", ""),
	}
}

// defaultMediaWorkers is half the CPUs, clamped to 1..4.
func defaultMediaWorkers() int {
	return min(max(runtime.NumCPU()/2, 1), 4)
}

// Validate returns an error if the configuration is not usable.
func (c Config) Validate() error {
	if c.HTTPAddr == "" {
		return errors.New("HTTPAddr must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("DataDir must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid LogLevel %q: must be one of debug, info, warn, error", c.LogLevel)
	}
	return nil
}

// DatabasePath returns the path of the server-level SQLite database file.
func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "popcorn.db")
}

// dataDir resolves the default data directory, honoring the POPCORN_DATA_DIR
// environment variable, then XDG_DATA_HOME and HOME on Unix-like systems. It
// returns a relative fallback only as a last resort so that unrelated files
// are never created in the working tree.
func dataDir() string {
	if v := envOrDefaultTrim("DATA_DIR", ""); v != "" {
		return v
	}

	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "popcorn")
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "popcorn")
	}

	return filepath.Join("popcorn-data")
}

// envOrDefault returns the value of the POPCORN_<name> environment variable or
// the given default. Whitespace is preserved.
func envOrDefault(name, def string) string {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok && v != "" {
		return v
	}
	return def
}

// envOrDefaultTrim is like envOrDefault but trims surrounding whitespace.
func envOrDefaultTrim(name, def string) string {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return def
}

// envOrDefaultBool parses a POPCORN_<name> boolean setting. Only the literal
// values accepted by strconv.ParseBool are honored; anything else is treated
// as an explicit "false" so a malformed value never silently enables a
// security flag.
func envOrDefaultBool(name string, def bool) bool {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// envOrDefaultInt parses a POPCORN_<name> integer setting. Only valid integers
// are honored; anything else falls back to the default so a malformed value
// never disables an outage-prone safety setting.
func envOrDefaultInt(name string, def int) int {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// envOrDefaultFloat parses a POPCORN_<name> floating-point setting, falling
// back to the default when unset or malformed.
func envOrDefaultFloat(name string, def float64) float64 {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

// envOrDefaultInt64 parses a POPCORN_<name> 64-bit integer setting, falling
// back to the default when unset or malformed.
func envOrDefaultInt64(name string, def int64) int64 {
	if v, ok := os.LookupEnv(EnvPrefix + "_" + name); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}
