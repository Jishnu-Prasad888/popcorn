package catalog

// Art says which artwork an item has; the HTTP layer turns it into URLs.
type Art struct {
	Poster   bool `json:"poster"`
	Backdrop bool `json:"backdrop"`
	Logo     bool `json:"logo"`
	Thumb    bool `json:"thumb"`
	Version  int  `json:"version"`
}

func artFrom(kinds string, version int) Art {
	a := Art{Version: version}
	for _, k := range splitComma(kinds) {
		switch k {
		case "poster":
			a.Poster = true
		case "backdrop":
			a.Backdrop = true
		case "logo":
			a.Logo = true
		case "thumb":
			a.Thumb = true
		}
	}
	return a
}

// UserState is one person's relationship with an item. The catalog never
// fills it; the HTTP layer adds it from the server database.
type UserState struct {
	PositionSecs float64 `json:"position_secs"`
	DurationSecs float64 `json:"duration_secs"`
	Played       bool    `json:"played"`
	PlayCount    int     `json:"play_count"`
	LastPlayedAt string  `json:"last_played_at,omitempty"`
	Favorite     bool    `json:"favorite"`
	Watchlist    bool    `json:"watchlist"`
	// UnplayedCount is the number of a show's or season's episodes not yet
	// watched.
	UnplayedCount int `json:"unplayed_count"`
}

// Card is an item in a grid or row: a movie, show, season or episode.
type Card struct {
	Type          string     `json:"type"`
	ID            string     `json:"id"`
	LibraryID     string     `json:"library_id"`
	Title         string     `json:"title"`
	OriginalTitle string     `json:"original_title,omitempty"`
	SortTitle     string     `json:"-"`
	Year          int        `json:"year,omitempty"`
	ReleaseDate   string     `json:"release_date,omitempty"`
	Rating        float64    `json:"rating,omitempty"`
	IMDbRating    float64    `json:"imdb_rating,omitempty"`
	RuntimeMins   int        `json:"runtime_mins,omitempty"`
	Genres        []string   `json:"genres"`
	ContentRating string     `json:"content_rating,omitempty"`
	Overview      string     `json:"overview,omitempty"`
	Tagline       string     `json:"tagline,omitempty"`
	Category      string     `json:"category,omitempty"`
	AddedAt       string     `json:"added_at"`
	UpdatedAt     string     `json:"updated_at,omitempty"`
	Art           Art        `json:"art"`
	MatchStatus   string     `json:"match_status,omitempty"`
	Resolution    string     `json:"resolution,omitempty"`
	Height        int        `json:"height,omitempty"`
	SizeBytes     int64      `json:"size_bytes,omitempty"`
	DurationSecs  float64    `json:"duration_secs,omitempty"`
	FileCount     int        `json:"file_count,omitempty"`
	Available     bool       `json:"available"`
	SeasonCount   int        `json:"season_count,omitempty"`
	EpisodeCount  int        `json:"episode_count,omitempty"`
	ShowID        string     `json:"show_id,omitempty"`
	ShowTitle     string     `json:"show_title,omitempty"`
	SeasonID      string     `json:"season_id,omitempty"`
	SeasonNumber  int        `json:"season_number"`
	EpisodeNumber int        `json:"episode_number,omitempty"`
	EpisodeEnd    int        `json:"episode_end,omitempty"`
	AirDate       string     `json:"air_date,omitempty"`
	ShowArt       *Art       `json:"show_art,omitempty"`
	State         *UserState `json:"state,omitempty"`
}

// Credit is one person credited on an item.
type Credit struct {
	PersonID   string `json:"person_id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	Character  string `json:"character,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// StreamInfo is a probed stream, as the player and detail pages show it.
type StreamInfo struct {
	Index             int    `json:"index"`
	Kind              string `json:"kind"`
	TypeIndex         int    `json:"type_index"`
	Codec             string `json:"codec"`
	Profile           string `json:"profile,omitempty"`
	Language          string `json:"language,omitempty"`
	LanguageName      string `json:"language_name,omitempty"`
	Title             string `json:"title,omitempty"`
	Channels          int    `json:"channels,omitempty"`
	ChannelLayout     string `json:"channel_layout,omitempty"`
	SampleRate        int    `json:"sample_rate,omitempty"`
	Bitrate           int64  `json:"bitrate,omitempty"`
	Width             int    `json:"width,omitempty"`
	Height            int    `json:"height,omitempty"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
	ImageBased        bool   `json:"image_based,omitempty"`
}

// ExternalSubtitle is a subtitle file that goes with a video.
type ExternalSubtitle struct {
	ID                string `json:"id"`
	Source            string `json:"source"`
	Path              string `json:"path"`
	Format            string `json:"format"`
	Language          string `json:"language,omitempty"`
	LanguageName      string `json:"language_name,omitempty"`
	Title             string `json:"title,omitempty"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
}

// Chapter is a chapter marker of a file.
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title,omitempty"`
}

// FileInfo is one video file of a movie or episode.
type FileInfo struct {
	ID           string             `json:"id"`
	RelPath      string             `json:"rel_path"`
	Name         string             `json:"name"`
	Edition      string             `json:"edition,omitempty"`
	SizeBytes    int64              `json:"size_bytes"`
	ModTime      string             `json:"mod_time"`
	Status       string             `json:"status"`
	Manual       bool               `json:"manual"`
	Container    string             `json:"container,omitempty"`
	DurationSecs float64            `json:"duration_secs,omitempty"`
	Bitrate      int64              `json:"bitrate,omitempty"`
	Width        int                `json:"width,omitempty"`
	Height       int                `json:"height,omitempty"`
	Resolution   string             `json:"resolution,omitempty"`
	VideoCodec   string             `json:"video_codec,omitempty"`
	VideoProfile string             `json:"video_profile,omitempty"`
	PixFmt       string             `json:"pix_fmt,omitempty"`
	FrameRate    float64            `json:"frame_rate,omitempty"`
	HDR          string             `json:"hdr,omitempty"`
	ProbeError   string             `json:"probe_error,omitempty"`
	Probed       bool               `json:"probed"`
	Streams      []StreamInfo       `json:"streams"`
	Subtitles    []ExternalSubtitle `json:"subtitles"`
	Chapters     []Chapter          `json:"chapters"`
}

// Movie is a movie's detail page.
type Movie struct {
	Card
	OriginalLanguage string     `json:"original_language,omitempty"`
	Languages        []string   `json:"languages"`
	Countries        []string   `json:"countries"`
	Studios          []string   `json:"studios"`
	IMDbID           string     `json:"imdb_id,omitempty"`
	TMDbID           int        `json:"tmdb_id,omitempty"`
	VoteCount        int        `json:"vote_count,omitempty"`
	IMDbVotes        int        `json:"imdb_votes,omitempty"`
	LockedFields     []string   `json:"locked_fields"`
	MetadataAt       string     `json:"metadata_at,omitempty"`
	Folder           string     `json:"folder,omitempty"`
	Credits          []Credit   `json:"credits"`
	Files            []FileInfo `json:"files"`
}

// Season is a season of a show.
type Season struct {
	ID           string     `json:"id"`
	ShowID       string     `json:"show_id"`
	LibraryID    string     `json:"library_id"`
	Number       int        `json:"season_number"`
	Title        string     `json:"title"`
	Overview     string     `json:"overview,omitempty"`
	AirDate      string     `json:"air_date,omitempty"`
	EpisodeCount int        `json:"episode_count"`
	EpisodeTotal int        `json:"episode_total,omitempty"`
	SortOrder    int        `json:"sort_order"`
	Art          Art        `json:"art"`
	LockedFields []string   `json:"locked_fields"`
	State        *UserState `json:"state,omitempty"`
}

// Show is a show's detail page.
type Show struct {
	Card
	FirstAirDate     string   `json:"first_air_date,omitempty"`
	LastAirDate      string   `json:"last_air_date,omitempty"`
	Status           string   `json:"status,omitempty"`
	OriginalLanguage string   `json:"original_language,omitempty"`
	Languages        []string `json:"languages"`
	Countries        []string `json:"countries"`
	Studios          []string `json:"studios"`
	IMDbID           string   `json:"imdb_id,omitempty"`
	TMDbID           int      `json:"tmdb_id,omitempty"`
	VoteCount        int      `json:"vote_count,omitempty"`
	IMDbVotes        int      `json:"imdb_votes,omitempty"`
	LockedFields     []string `json:"locked_fields"`
	MetadataAt       string   `json:"metadata_at,omitempty"`
	Folder           string   `json:"folder,omitempty"`
	Credits          []Credit `json:"credits"`
	Seasons          []Season `json:"seasons"`
}

// Episode is an episode's detail.
type Episode struct {
	Card
	IntroStart   *float64   `json:"intro_start,omitempty"`
	IntroEnd     *float64   `json:"intro_end,omitempty"`
	CreditsStart *float64   `json:"credits_start,omitempty"`
	SortOrder    int        `json:"sort_order"`
	LockedFields []string   `json:"locked_fields"`
	Credits      []Credit   `json:"credits"`
	Files        []FileInfo `json:"files"`
}

// Person is a cast or crew member and what they appear in.
type Person struct {
	ID         string              `json:"id"`
	LibraryID  string              `json:"library_id"`
	Name       string              `json:"name"`
	TMDbID     int                 `json:"tmdb_id,omitempty"`
	ProfileURL string              `json:"profile_url,omitempty"`
	Items      []Card              `json:"items"`
	Roles      map[string][]string `json:"roles"` // item id → roles/characters
}

// Filter narrows a listing.
type Filter struct {
	Query      string
	Genre      string
	YearFrom   int
	YearTo     int
	MinRating  float64
	Language   string
	Director   string // person id or name
	Actor      string // person id or name
	Person     string // anyone credited
	Resolution string // 4k, 1080p, 720p, sd
	Category   string // tv, anime, web (shows)
	Studio     string
	Unmatched  bool
}

// Resolution labels a frame height.
func Resolution(height int) string {
	switch {
	case height >= 1700:
		return "4K"
	case height >= 1000:
		return "1080p"
	case height >= 700:
		return "720p"
	case height >= 470:
		return "480p"
	case height > 0:
		return "SD"
	}
	return ""
}

func resolutionRange(label string) (lo, hi int, ok bool) {
	switch label {
	case "4k", "4K", "2160p":
		return 1700, 100000, true
	case "1080p":
		return 1000, 1699, true
	case "720p":
		return 700, 999, true
	case "480p", "sd", "SD":
		return 1, 699, true
	}
	return 0, 0, false
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
