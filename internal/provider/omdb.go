package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OMDb is the Open Movie Database API, used for IMDb ratings.
type OMDb struct {
	key    string
	client *http.Client
	base   string
}

// NewOMDb returns an OMDb client.
func NewOMDb(key string) *OMDb {
	return &OMDb{key: strings.TrimSpace(key), client: &http.Client{Timeout: 15 * time.Second},
		base: "https://www.omdbapi.com/"}
}

// IMDbRating implements Ratings.
func (o *OMDb) IMDbRating(ctx context.Context, imdbID string) (float64, int, string, error) {
	if o.key == "" {
		return 0, 0, "", ErrNotConfigured
	}
	q := url.Values{"i": {imdbID}, "apikey": {o.key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base+"?"+q.Encode(), nil)
	if err != nil {
		return 0, 0, "", err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("omdb: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, 0, "", fmt.Errorf("omdb: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, 0, "", fmt.Errorf("omdb: the API key was rejected")
	}
	var r struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		IMDbVotes  string `json:"imdbVotes"`
		Rated      string `json:"Rated"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, 0, "", fmt.Errorf("omdb: decode: %w", err)
	}
	if r.Response != "True" {
		if strings.Contains(strings.ToLower(r.Error), "not found") {
			return 0, 0, "", ErrNotFound
		}
		return 0, 0, "", fmt.Errorf("omdb: %s", r.Error)
	}
	rating, _ := strconv.ParseFloat(r.IMDbRating, 64)
	votes, _ := strconv.Atoi(strings.ReplaceAll(r.IMDbVotes, ",", ""))
	rated := r.Rated
	if rated == "N/A" {
		rated = ""
	}
	return rating, votes, rated, nil
}
