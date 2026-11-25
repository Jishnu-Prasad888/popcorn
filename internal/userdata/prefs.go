package userdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SubtitleStyle is how subtitles look in the player.
type SubtitleStyle struct {
	Size       int    `json:"size"`       // percent of the default, 50–250
	Color      string `json:"color"`      // CSS colour
	Background string `json:"background"` // none, shadow, box
	Opacity    int    `json:"opacity"`    // background opacity percent
	Position   int    `json:"position"`   // percent from the bottom, 0–40
	Font       string `json:"font"`       // sans, serif, mono, rounded
}

// Playback holds a person's playback preferences.
type Playback struct {
	AudioLanguages    []string      `json:"audio_languages"`
	SubtitleLanguages []string      `json:"subtitle_languages"`
	// SubtitleMode: auto (show when the audio is not in a preferred
	// language), always, forced (forced subtitles only), off.
	SubtitleMode      string        `json:"subtitle_mode"`
	PreferOriginal    bool          `json:"prefer_original_audio"`
	PreferSDH         bool          `json:"prefer_sdh"`
	AutoplayNext      bool          `json:"autoplay_next"`
	NextCountdown     int           `json:"next_countdown"` // seconds
	SkipIntro         string        `json:"skip_intro"`     // off, button, auto
	SkipCredits       string        `json:"skip_credits"`   // off, button, auto
	Quality           string        `json:"quality"`        // original, 2160p, 1080p, 720p, 480p
	MaxBitrate        int           `json:"max_bitrate"`    // kbit/s, 0 = no cap
	Speed             float64       `json:"speed"`
	SkipForward       int           `json:"skip_forward"`  // seconds
	SkipBackward      int           `json:"skip_backward"` // seconds
	Passthrough       []string      `json:"passthrough"`   // ac3, eac3, dts, truehd
	SubtitleStyle     SubtitleStyle `json:"subtitle_style"`
}

// DefaultPlayback is what a new account starts with.
func DefaultPlayback() Playback {
	return Playback{
		AudioLanguages: []string{"eng"}, SubtitleLanguages: []string{"eng"}, SubtitleMode: "auto",
		AutoplayNext: true, NextCountdown: 10, SkipIntro: "button", SkipCredits: "button",
		Quality: "original", Speed: 1, SkipForward: 10, SkipBackward: 10, Passthrough: []string{},
		SubtitleStyle: SubtitleStyle{Size: 100, Color: "#ffffff", Background: "shadow", Opacity: 60, Position: 8, Font: "sans"},
	}
}

// Interface holds a person's look-and-feel preferences.
type Interface struct {
	Density  string `json:"density"`   // compact, comfortable, spacious
	CardSize string `json:"card_size"` // small, medium, large
	View     string `json:"view"`      // grid, list
	Motion   string `json:"motion"`    // full, reduced
	Accent   string `json:"accent"`    // purple, pink, cyan, green
	HomeRows []string `json:"home_rows"`
}

// DefaultInterface is the default look.
func DefaultInterface() Interface {
	return Interface{Density: "comfortable", CardSize: "medium", View: "grid", Motion: "full", Accent: "purple",
		HomeRows: []string{"continue", "next_up", "recent_movies", "recent_episodes", "collections", "watchlist", "favorites", "genres"}}
}

func (s *Store) loadJSON(ctx context.Context, userID, key string, out any) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), out)
}

func (s *Store) saveJSON(ctx context.Context, userID, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		userID, key, string(b), now())
	return err
}

// Playback returns a person's playback preferences.
func (s *Store) Playback(ctx context.Context, userID string) (Playback, error) {
	p := DefaultPlayback()
	if err := s.loadJSON(ctx, userID, "playback", &p); err != nil {
		return p, err
	}
	return normalizePlayback(p), nil
}

func normalizePlayback(p Playback) Playback {
	d := DefaultPlayback()
	if p.AudioLanguages == nil {
		p.AudioLanguages = []string{}
	}
	if p.SubtitleLanguages == nil {
		p.SubtitleLanguages = []string{}
	}
	if p.Passthrough == nil {
		p.Passthrough = []string{}
	}
	switch p.SubtitleMode {
	case "auto", "always", "forced", "off":
	default:
		p.SubtitleMode = d.SubtitleMode
	}
	for _, f := range []*string{&p.SkipIntro, &p.SkipCredits} {
		switch *f {
		case "off", "button", "auto":
		default:
			*f = "button"
		}
	}
	switch p.Quality {
	case "original", "2160p", "1080p", "720p", "480p", "360p":
	default:
		p.Quality = "original"
	}
	if p.NextCountdown < 0 || p.NextCountdown > 60 {
		p.NextCountdown = d.NextCountdown
	}
	if p.Speed < 0.25 || p.Speed > 3 {
		p.Speed = 1
	}
	if p.SkipForward <= 0 || p.SkipForward > 300 {
		p.SkipForward = d.SkipForward
	}
	if p.SkipBackward <= 0 || p.SkipBackward > 300 {
		p.SkipBackward = d.SkipBackward
	}
	st := &p.SubtitleStyle
	if st.Size < 50 || st.Size > 250 {
		st.Size = 100
	}
	if st.Color == "" {
		st.Color = "#ffffff"
	}
	switch st.Background {
	case "none", "shadow", "box", "outline":
	default:
		st.Background = "shadow"
	}
	if st.Opacity < 0 || st.Opacity > 100 {
		st.Opacity = 60
	}
	if st.Position < 0 || st.Position > 40 {
		st.Position = 8
	}
	switch st.Font {
	case "sans", "serif", "mono", "rounded", "techno":
	default:
		st.Font = "sans"
	}
	return p
}

// SetPlayback stores a person's playback preferences.
func (s *Store) SetPlayback(ctx context.Context, userID string, p Playback) (Playback, error) {
	p = normalizePlayback(p)
	return p, s.saveJSON(ctx, userID, "playback", p)
}

// Interface returns a person's interface preferences.
func (s *Store) Interface(ctx context.Context, userID string) (Interface, error) {
	p := DefaultInterface()
	if err := s.loadJSON(ctx, userID, "interface", &p); err != nil {
		return p, err
	}
	return normalizeInterface(p), nil
}

func normalizeInterface(p Interface) Interface {
	d := DefaultInterface()
	check := func(v *string, def string, allowed ...string) {
		for _, a := range allowed {
			if *v == a {
				return
			}
		}
		*v = def
	}
	check(&p.Density, d.Density, "compact", "comfortable", "spacious")
	check(&p.CardSize, d.CardSize, "small", "medium", "large")
	check(&p.View, d.View, "grid", "list")
	check(&p.Motion, d.Motion, "full", "reduced")
	check(&p.Accent, d.Accent, "purple", "pink", "cyan", "green", "orange")
	if p.HomeRows == nil {
		p.HomeRows = d.HomeRows
	}
	return p
}

// SetInterface stores a person's interface preferences.
func (s *Store) SetInterface(ctx context.Context, userID string, p Interface) (Interface, error) {
	p = normalizeInterface(p)
	return p, s.saveJSON(ctx, userID, "interface", p)
}
