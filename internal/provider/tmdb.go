package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDB is The Movie Database API (v3). It accepts either a v3 API key or a v4
// read access token (a JWT, sent as a bearer token).
type TMDB struct {
	key      string
	language string
	region   string
	client   *http.Client
	base     string
	imgBase  string

	// limiter spaces requests out; TMDB allows roughly 50 per second, and
	// Popcorn stays far below that so a big first scan is polite.
	mu   sync.Mutex
	last time.Time
}

// NewTMDB returns a TMDB client. language is a TMDB language tag such as
// "en-US"; region picks the age-rating country (e.g. "US", "IN").
func NewTMDB(key, language, region string) *TMDB {
	if language == "" {
		language = "en-US"
	}
	if region == "" {
		region = "US"
	}
	return &TMDB{
		key: strings.TrimSpace(key), language: language, region: strings.ToUpper(region),
		client:  &http.Client{Timeout: 20 * time.Second},
		base:    "https://api.themoviedb.org/3",
		imgBase: "https://image.tmdb.org/t/p/",
	}
}

// Name implements Provider.
func (t *TMDB) Name() string { return "tmdb" }

func (t *TMDB) wait() {
	t.mu.Lock()
	defer t.mu.Unlock()
	const gap = 40 * time.Millisecond
	if d := time.Since(t.last); d < gap {
		time.Sleep(gap - d)
	}
	t.last = time.Now()
}

func (t *TMDB) get(ctx context.Context, path string, q url.Values, out any) error {
	if t.key == "" {
		return ErrNotConfigured
	}
	if q == nil {
		q = url.Values{}
	}
	if q.Get("language") == "" {
		q.Set("language", t.language)
	}
	bearer := strings.Count(t.key, ".") == 2 && len(t.key) > 60
	if !bearer {
		q.Set("api_key", t.key)
	}
	for attempt := 0; attempt < 3; attempt++ {
		t.wait()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if bearer {
			req.Header.Set("Authorization", "Bearer "+t.key)
		}
		resp, err := t.client.Do(req)
		if err != nil {
			if attempt < 2 && ctx.Err() == nil {
				time.Sleep(time.Second)
				continue
			}
			return fmt.Errorf("tmdb: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("tmdb: %w", err)
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := 2 * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusUnauthorized:
			return fmt.Errorf("tmdb: the API key was rejected")
		case resp.StatusCode >= 300:
			return fmt.Errorf("tmdb: HTTP %d", resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("tmdb: decode %s: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("tmdb: too many retries")
}

func (t *TMDB) img(size, p string) string {
	if p == "" {
		return ""
	}
	return t.imgBase + size + p
}

func yearOf(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

type tmdbSearch struct {
	Results []struct {
		ID            int     `json:"id"`
		Title         string  `json:"title"`
		Name          string  `json:"name"`
		OriginalTitle string  `json:"original_title"`
		OriginalName  string  `json:"original_name"`
		ReleaseDate   string  `json:"release_date"`
		FirstAirDate  string  `json:"first_air_date"`
		Overview      string  `json:"overview"`
		PosterPath    string  `json:"poster_path"`
		VoteAverage   float64 `json:"vote_average"`
		Popularity    float64 `json:"popularity"`
	} `json:"results"`
}

func (t *TMDB) search(ctx context.Context, kind, query string, year int) ([]SearchResult, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	path := "/search/movie"
	if kind == "show" {
		path = "/search/tv"
		if year > 0 {
			q.Set("first_air_date_year", strconv.Itoa(year))
		}
	} else if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var res tmdbSearch
	if err := t.get(ctx, path, q, &res); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(res.Results))
	for _, r := range res.Results {
		title, orig, date := r.Title, r.OriginalTitle, r.ReleaseDate
		if kind == "show" {
			title, orig, date = r.Name, r.OriginalName, r.FirstAirDate
		}
		out = append(out, SearchResult{
			Provider: "tmdb", ID: strconv.Itoa(r.ID), Kind: kind, Title: title,
			OriginalTitle: orig, Year: yearOf(date), Overview: r.Overview,
			PosterURL: t.img("w342", r.PosterPath), Rating: r.VoteAverage, Popularity: r.Popularity,
		})
	}
	return out, nil
}

// SearchMovies implements Provider.
func (t *TMDB) SearchMovies(ctx context.Context, query string, year int) ([]SearchResult, error) {
	return t.search(ctx, "movie", query, year)
}

// SearchShows implements Provider.
func (t *TMDB) SearchShows(ctx context.Context, query string, year int) ([]SearchResult, error) {
	return t.search(ctx, "show", query, year)
}

type tmdbName struct {
	Name     string `json:"name"`
	ISO31661 string `json:"iso_3166_1"`
	ISO6391  string `json:"iso_639_1"`
	English  string `json:"english_name"`
}

type tmdbCredits struct {
	Cast []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Character   string `json:"character"`
		Order       int    `json:"order"`
		ProfilePath string `json:"profile_path"`
		Roles       []struct {
			Character string `json:"character"`
		} `json:"roles"`
	} `json:"cast"`
	Crew []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Job         string `json:"job"`
		Department  string `json:"department"`
		ProfilePath string `json:"profile_path"`
		Jobs        []struct {
			Job string `json:"job"`
		} `json:"jobs"`
	} `json:"crew"`
}

type tmdbImages struct {
	Logos []struct {
		FilePath string  `json:"file_path"`
		Lang     string  `json:"iso_639_1"`
		Vote     float64 `json:"vote_average"`
	} `json:"logos"`
}

func (t *TMDB) people(c tmdbCredits, castLimit int) []Person {
	var out []Person
	for i, p := range c.Cast {
		if i >= castLimit {
			break
		}
		ch := p.Character
		if ch == "" && len(p.Roles) > 0 {
			ch = p.Roles[0].Character
		}
		out = append(out, Person{ProviderID: p.ID, Name: p.Name, Role: "cast", Character: ch, Order: i,
			ProfileURL: t.img("w185", p.ProfilePath)})
	}
	seen := map[string]bool{}
	for i, p := range c.Crew {
		jobs := []string{p.Job}
		for _, j := range p.Jobs {
			jobs = append(jobs, j.Job)
		}
		for _, job := range jobs {
			role := ""
			switch job {
			case "Director":
				role = "director"
			case "Screenplay", "Writer", "Story", "Novel", "Teleplay", "Author":
				role = "writer"
			case "Producer", "Executive Producer":
				role = "producer"
			}
			if role == "" || role == "producer" && job != "Producer" {
				continue
			}
			k := role + "/" + strconv.Itoa(p.ID)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, Person{ProviderID: p.ID, Name: p.Name, Role: role, Order: 1000 + i,
				ProfileURL: t.img("w185", p.ProfilePath)})
		}
	}
	return out
}

func (t *TMDB) bestLogo(imgs tmdbImages) string {
	lang := strings.SplitN(t.language, "-", 2)[0]
	best, bestScore := "", -1.0
	for _, l := range imgs.Logos {
		score := l.Vote
		switch l.Lang {
		case lang:
			score += 100
		case "en":
			score += 50
		case "":
			score += 10
		}
		if score > bestScore {
			best, bestScore = l.FilePath, score
		}
	}
	return t.img("w500", best)
}

func names(in []tmdbName, english bool) []string {
	out := []string{}
	for _, n := range in {
		v := n.Name
		if english && n.English != "" {
			v = n.English
		}
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func isAnimation(genres []tmdbName) bool {
	for _, g := range genres {
		if g.Name == "Animation" {
			return true
		}
	}
	return false
}

// Movie implements Provider.
func (t *TMDB) Movie(ctx context.Context, id string) (*Movie, error) {
	var r struct {
		ID                  int        `json:"id"`
		IMDbID              string     `json:"imdb_id"`
		Title               string     `json:"title"`
		OriginalTitle       string     `json:"original_title"`
		ReleaseDate         string     `json:"release_date"`
		Overview            string     `json:"overview"`
		Tagline             string     `json:"tagline"`
		Runtime             int        `json:"runtime"`
		VoteAverage         float64    `json:"vote_average"`
		VoteCount           int        `json:"vote_count"`
		Genres              []tmdbName `json:"genres"`
		SpokenLanguages     []tmdbName `json:"spoken_languages"`
		OriginalLanguage    string     `json:"original_language"`
		ProductionCountries []tmdbName `json:"production_countries"`
		ProductionCompanies []tmdbName `json:"production_companies"`
		PosterPath          string     `json:"poster_path"`
		BackdropPath        string     `json:"backdrop_path"`
		Credits             tmdbCredits `json:"credits"`
		Images              tmdbImages `json:"images"`
		ReleaseDates        struct {
			Results []struct {
				ISO31661     string `json:"iso_3166_1"`
				ReleaseDates []struct {
					Certification string `json:"certification"`
					Type          int    `json:"type"`
				} `json:"release_dates"`
			} `json:"results"`
		} `json:"release_dates"`
	}
	q := url.Values{
		"append_to_response":     {"credits,release_dates,images"},
		"include_image_language": {strings.SplitN(t.language, "-", 2)[0] + ",en,null"},
	}
	if err := t.get(ctx, "/movie/"+url.PathEscape(id), q, &r); err != nil {
		return nil, err
	}
	m := &Movie{
		ProviderID: strconv.Itoa(r.ID), TMDbID: r.ID, IMDbID: r.IMDbID, Title: r.Title,
		OriginalTitle: r.OriginalTitle, Year: yearOf(r.ReleaseDate), ReleaseDate: r.ReleaseDate,
		Overview: r.Overview, Tagline: r.Tagline, RuntimeMins: r.Runtime, Rating: r.VoteAverage,
		VoteCount: r.VoteCount, Genres: names(r.Genres, false), Languages: names(r.SpokenLanguages, true),
		OriginalLanguage: r.OriginalLanguage, Countries: names(r.ProductionCountries, false),
		Studios: names(r.ProductionCompanies, false), Credits: t.people(r.Credits, 30),
		Images: Images{Poster: t.img("w780", r.PosterPath), Backdrop: t.img("w1280", r.BackdropPath),
			Logo: t.bestLogo(r.Images)},
		Animation: isAnimation(r.Genres),
	}
	// Age rating: the configured region first, then the US.
	for _, region := range []string{t.region, "US"} {
		for _, rd := range r.ReleaseDates.Results {
			if rd.ISO31661 != region {
				continue
			}
			for _, d := range rd.ReleaseDates {
				if d.Certification != "" {
					m.ContentRating = d.Certification
					break
				}
			}
		}
		if m.ContentRating != "" {
			break
		}
	}
	return m, nil
}

// Show implements Provider.
func (t *TMDB) Show(ctx context.Context, id string) (*Show, error) {
	var r struct {
		ID                  int        `json:"id"`
		Name                string     `json:"name"`
		OriginalName        string     `json:"original_name"`
		FirstAirDate        string     `json:"first_air_date"`
		LastAirDate         string     `json:"last_air_date"`
		Status              string     `json:"status"`
		Overview            string     `json:"overview"`
		Tagline             string     `json:"tagline"`
		VoteAverage         float64    `json:"vote_average"`
		VoteCount           int        `json:"vote_count"`
		Genres              []tmdbName `json:"genres"`
		SpokenLanguages     []tmdbName `json:"spoken_languages"`
		OriginalLanguage    string     `json:"original_language"`
		OriginCountry       []string   `json:"origin_country"`
		Networks            []tmdbName `json:"networks"`
		ProductionCompanies []tmdbName `json:"production_companies"`
		PosterPath          string     `json:"poster_path"`
		BackdropPath        string     `json:"backdrop_path"`
		CreatedBy           []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			ProfilePath string `json:"profile_path"`
		} `json:"created_by"`
		Seasons []struct {
			SeasonNumber int    `json:"season_number"`
			Name         string `json:"name"`
			Overview     string `json:"overview"`
			AirDate      string `json:"air_date"`
			EpisodeCount int    `json:"episode_count"`
			PosterPath   string `json:"poster_path"`
		} `json:"seasons"`
		AggregateCredits tmdbCredits `json:"aggregate_credits"`
		ExternalIDs      struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
		ContentRatings struct {
			Results []struct {
				ISO31661 string `json:"iso_3166_1"`
				Rating   string `json:"rating"`
			} `json:"results"`
		} `json:"content_ratings"`
		Images tmdbImages `json:"images"`
	}
	q := url.Values{
		"append_to_response":     {"aggregate_credits,external_ids,content_ratings,images"},
		"include_image_language": {strings.SplitN(t.language, "-", 2)[0] + ",en,null"},
	}
	if err := t.get(ctx, "/tv/"+url.PathEscape(id), q, &r); err != nil {
		return nil, err
	}
	studios := names(r.Networks, false)
	for _, c := range names(r.ProductionCompanies, false) {
		if len(studios) >= 6 {
			break
		}
		studios = append(studios, c)
	}
	s := &Show{
		ProviderID: strconv.Itoa(r.ID), TMDbID: r.ID, IMDbID: r.ExternalIDs.IMDbID, Title: r.Name,
		OriginalTitle: r.OriginalName, Year: yearOf(r.FirstAirDate), FirstAirDate: r.FirstAirDate,
		LastAirDate: r.LastAirDate, Status: r.Status, Overview: r.Overview, Tagline: r.Tagline,
		Rating: r.VoteAverage, VoteCount: r.VoteCount, Genres: names(r.Genres, false),
		Languages: names(r.SpokenLanguages, true), OriginalLanguage: r.OriginalLanguage,
		Countries: r.OriginCountry, Studios: studios, Credits: t.people(r.AggregateCredits, 30),
		Images: Images{Poster: t.img("w780", r.PosterPath), Backdrop: t.img("w1280", r.BackdropPath),
			Logo: t.bestLogo(r.Images)},
		Animation: isAnimation(r.Genres),
	}
	if s.Countries == nil {
		s.Countries = []string{}
	}
	for i, c := range r.CreatedBy {
		s.Credits = append(s.Credits, Person{ProviderID: c.ID, Name: c.Name, Role: "creator", Order: 2000 + i,
			ProfileURL: t.img("w185", c.ProfilePath)})
	}
	for _, region := range []string{t.region, "US"} {
		for _, cr := range r.ContentRatings.Results {
			if cr.ISO31661 == region && cr.Rating != "" {
				s.ContentRating = cr.Rating
				break
			}
		}
		if s.ContentRating != "" {
			break
		}
	}
	for _, se := range r.Seasons {
		s.Seasons = append(s.Seasons, SeasonSummary{Number: se.SeasonNumber, Title: se.Name, Overview: se.Overview,
			AirDate: se.AirDate, EpisodeCount: se.EpisodeCount, PosterURL: t.img("w500", se.PosterPath)})
	}
	sort.Slice(s.Seasons, func(i, j int) bool { return s.Seasons[i].Number < s.Seasons[j].Number })
	return s, nil
}

// Season implements Provider.
func (t *TMDB) Season(ctx context.Context, showID string, number int) (*Season, error) {
	var r struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		Overview     string `json:"overview"`
		AirDate      string `json:"air_date"`
		PosterPath   string `json:"poster_path"`
		Episodes     []struct {
			ID            int     `json:"id"`
			EpisodeNumber int     `json:"episode_number"`
			SeasonNumber  int     `json:"season_number"`
			Name          string  `json:"name"`
			Overview      string  `json:"overview"`
			AirDate       string  `json:"air_date"`
			Runtime       int     `json:"runtime"`
			VoteAverage   float64 `json:"vote_average"`
			VoteCount     int     `json:"vote_count"`
			StillPath     string  `json:"still_path"`
			Crew          []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
			GuestStars []struct {
				ID          int    `json:"id"`
				Name        string `json:"name"`
				Character   string `json:"character"`
				Order       int    `json:"order"`
				ProfilePath string `json:"profile_path"`
			} `json:"guest_stars"`
		} `json:"episodes"`
	}
	if err := t.get(ctx, fmt.Sprintf("/tv/%s/season/%d", url.PathEscape(showID), number), nil, &r); err != nil {
		return nil, err
	}
	s := &Season{Number: r.SeasonNumber, Title: r.Name, Overview: r.Overview, AirDate: r.AirDate,
		PosterURL: t.img("w500", r.PosterPath)}
	for _, e := range r.Episodes {
		ep := Episode{Season: e.SeasonNumber, Number: e.EpisodeNumber, TMDbID: e.ID, Title: e.Name,
			Overview: e.Overview, AirDate: e.AirDate, RuntimeMins: e.Runtime, Rating: e.VoteAverage,
			VoteCount: e.VoteCount, StillURL: t.img("w780", e.StillPath)}
		for i, c := range e.Crew {
			role := ""
			switch c.Job {
			case "Director":
				role = "director"
			case "Writer", "Teleplay", "Screenplay", "Story":
				role = "writer"
			}
			if role != "" {
				ep.Credits = append(ep.Credits, Person{ProviderID: c.ID, Name: c.Name, Role: role, Order: 1000 + i})
			}
		}
		for i, g := range e.GuestStars {
			if i >= 10 {
				break
			}
			ep.Credits = append(ep.Credits, Person{ProviderID: g.ID, Name: g.Name, Role: "cast",
				Character: g.Character, Order: 500 + g.Order, ProfileURL: t.img("w185", g.ProfilePath)})
		}
		s.Episodes = append(s.Episodes, ep)
	}
	return s, nil
}

// FindIMDb implements Provider.
func (t *TMDB) FindIMDb(ctx context.Context, imdbID string) (string, string, error) {
	var r struct {
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	if err := t.get(ctx, "/find/"+url.PathEscape(imdbID), url.Values{"external_source": {"imdb_id"}}, &r); err != nil {
		return "", "", err
	}
	if len(r.MovieResults) > 0 {
		return "movie", strconv.Itoa(r.MovieResults[0].ID), nil
	}
	if len(r.TVResults) > 0 {
		return "show", strconv.Itoa(r.TVResults[0].ID), nil
	}
	return "", "", ErrNotFound
}
