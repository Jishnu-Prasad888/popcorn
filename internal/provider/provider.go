// Package provider fetches movie and TV metadata from online sources.
//
// The rest of Popcorn depends only on the Provider interface and the plain
// structs here, never on one service: TMDB is the built-in implementation for
// titles, credits, seasons, episodes and artwork, and OMDb optionally adds
// IMDb ratings. Another provider can be added without touching the catalog.
package provider

import (
	"context"
	"errors"
)

// ErrNotConfigured means no API key is set for the provider.
var ErrNotConfigured = errors.New("metadata provider not configured")

// ErrNotFound means the provider has no such title.
var ErrNotFound = errors.New("title not found at provider")

// SearchResult is one candidate match for a title search.
type SearchResult struct {
	Provider      string  `json:"provider"`
	ID            string  `json:"id"`
	Kind          string  `json:"kind"` // movie or show
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title,omitempty"`
	Year          int     `json:"year,omitempty"`
	Overview      string  `json:"overview,omitempty"`
	PosterURL     string  `json:"poster_url,omitempty"`
	Rating        float64 `json:"rating,omitempty"`
	Popularity    float64 `json:"popularity,omitempty"`
}

// Person is one credit.
type Person struct {
	ProviderID int    `json:"provider_id"`
	Name       string `json:"name"`
	Role       string `json:"role"` // cast, director, writer, producer, creator
	Character  string `json:"character,omitempty"`
	Order      int    `json:"order"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// Images are artwork URLs; empty when the provider has none.
type Images struct {
	Poster   string
	Backdrop string
	Logo     string
}

// Movie is a movie's full metadata.
type Movie struct {
	ProviderID       string
	TMDbID           int
	IMDbID           string
	Title            string
	OriginalTitle    string
	Year             int
	ReleaseDate      string
	Overview         string
	Tagline          string
	RuntimeMins      int
	ContentRating    string
	Rating           float64
	VoteCount        int
	Genres           []string
	Languages        []string
	OriginalLanguage string
	Countries        []string
	Studios          []string
	Credits          []Person
	Images           Images
	Animation        bool
}

// Show is a TV show's metadata, without its episodes.
type Show struct {
	ProviderID       string
	TMDbID           int
	IMDbID           string
	Title            string
	OriginalTitle    string
	Year             int
	FirstAirDate     string
	LastAirDate      string
	Status           string
	Overview         string
	Tagline          string
	ContentRating    string
	Rating           float64
	VoteCount        int
	Genres           []string
	Languages        []string
	OriginalLanguage string
	Countries        []string
	Studios          []string
	Credits          []Person
	Images           Images
	Seasons          []SeasonSummary
	Animation        bool
}

// SeasonSummary is a season as listed on its show.
type SeasonSummary struct {
	Number       int
	Title        string
	Overview     string
	AirDate      string
	EpisodeCount int
	PosterURL    string
}

// Season is a season with its episodes.
type Season struct {
	Number    int
	Title     string
	Overview  string
	AirDate   string
	PosterURL string
	Episodes  []Episode
}

// Episode is one episode's metadata.
type Episode struct {
	Season      int
	Number      int
	TMDbID      int
	Title       string
	Overview    string
	AirDate     string
	RuntimeMins int
	Rating      float64
	VoteCount   int
	StillURL    string
	Credits     []Person
}

// Provider is a source of movie and TV metadata.
type Provider interface {
	Name() string
	SearchMovies(ctx context.Context, query string, year int) ([]SearchResult, error)
	SearchShows(ctx context.Context, query string, year int) ([]SearchResult, error)
	Movie(ctx context.Context, id string) (*Movie, error)
	Show(ctx context.Context, id string) (*Show, error)
	Season(ctx context.Context, showID string, number int) (*Season, error)
	// FindIMDb resolves an IMDb id to this provider's id and kind.
	FindIMDb(ctx context.Context, imdbID string) (kind, id string, err error)
}

// Ratings looks up IMDb ratings by IMDb id.
type Ratings interface {
	IMDbRating(ctx context.Context, imdbID string) (rating float64, votes int, contentRating string, err error)
}
