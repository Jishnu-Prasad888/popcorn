// Package userdata keeps what belongs to a person rather than to a library:
// playback progress and watched state, watch history, favourites and
// watchlists, collections, playlists, and playback preferences. It lives in
// the server database so it follows the user across devices.
package userdata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound means the collection, playlist or entry does not exist (or
// belongs to someone else).
var ErrNotFound = errors.New("not found")

// ErrInvalid is a rejected request.
type ErrInvalid struct{ Msg string }

func (e *ErrInvalid) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ErrInvalid{Msg: fmt.Sprintf(format, a...)} }

// Store is the per-user data store over the server database.
type Store struct{ db *sql.DB }

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db} }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Ref names a library item.
type Ref struct {
	LibraryID string `json:"library_id"`
	Type      string `json:"type"`
	ID        string `json:"id"`
}

func (r Ref) Key() string { return r.LibraryID + "/" + r.Type + "/" + r.ID }

// State is a person's playback state for one movie or episode.
type State struct {
	Ref
	ShowID           string  `json:"show_id,omitempty"`
	FileID           string  `json:"file_id,omitempty"`
	PositionSecs     float64 `json:"position_secs"`
	DurationSecs     float64 `json:"duration_secs"`
	Played           bool    `json:"played"`
	PlayCount        int     `json:"play_count"`
	AudioLanguage    string  `json:"audio_language,omitempty"`
	SubtitleLanguage string  `json:"subtitle_language,omitempty"`
	LastPlayedAt     string  `json:"last_played_at,omitempty"`
}

// WatchedThreshold is how far into an item (as a fraction) playback counts
// as finished when no credits marker says otherwise.
const WatchedThreshold = 0.92

// Progress is a playback progress report from the player.
type Progress struct {
	Ref
	ShowID           string
	FileID           string
	PositionSecs     float64
	DurationSecs     float64
	CreditsStart     float64 // > 0 when known: reaching it finishes the item
	AudioLanguage    string
	SubtitleLanguage string
	Ended            bool
}

// ReportProgress records where a person is in an item. Passing the credits
// (or the watched threshold) marks it played, logs it in the watch history,
// and resets the position so the next play starts at the beginning.
func (s *Store) ReportProgress(ctx context.Context, userID string, p Progress) (*State, error) {
	if p.Type != "movie" && p.Type != "episode" {
		return nil, invalid("only movies and episodes are played")
	}
	if p.PositionSecs < 0 {
		p.PositionSecs = 0
	}
	finished := p.Ended
	if p.DurationSecs > 0 {
		if p.CreditsStart > 0 && p.PositionSecs >= p.CreditsStart {
			finished = true
		}
		if p.PositionSecs >= p.DurationSecs*WatchedThreshold {
			finished = true
		}
	}
	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var wasPlayed bool
	var prevPos float64
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT played, position_secs FROM playback_state
		WHERE user_id = ? AND library_id = ? AND item_type = ? AND item_id = ?`,
		userID, p.LibraryID, p.Type, p.ID).Scan(&wasPlayed, &prevPos)
	switch {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, err
	}
	_ = exists
	pos := p.PositionSecs
	playedInc := 0
	played := 0
	if finished {
		pos = 0
		played = 1
		// One finished play per crossing: a report after finishing (the
		// player still at the end) does not count again.
		if prevPos > 0 || !wasPlayed {
			playedInc = 1
		}
	} else if wasPlayed {
		played = 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO playback_state (user_id, library_id, item_type, item_id, show_id,
			file_id, position_secs, duration_secs, played, play_count, audio_language, subtitle_language,
			last_played_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, library_id, item_type, item_id) DO UPDATE SET
			show_id = excluded.show_id, file_id = excluded.file_id, position_secs = excluded.position_secs,
			duration_secs = CASE WHEN excluded.duration_secs > 0 THEN excluded.duration_secs ELSE playback_state.duration_secs END,
			played = excluded.played, play_count = playback_state.play_count + ?,
			audio_language = COALESCE(excluded.audio_language, playback_state.audio_language),
			subtitle_language = COALESCE(excluded.subtitle_language, playback_state.subtitle_language),
			last_played_at = excluded.last_played_at, updated_at = excluded.updated_at`,
		userID, p.LibraryID, p.Type, p.ID, nullStr(p.ShowID), nullStr(p.FileID), pos, p.DurationSecs,
		played, playedInc, nullStrKeep(p.AudioLanguage), nullStrKeep(p.SubtitleLanguage), t, t, playedInc); err != nil {
		return nil, err
	}
	if playedInc > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO watch_history (user_id, library_id, item_type, item_id, show_id,
			watched_at, watched_secs, completed) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			userID, p.LibraryID, p.Type, p.ID, nullStr(p.ShowID), t, p.PositionSecs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.State(ctx, userID, p.Ref)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullStrKeep stores "off" for an explicit choice of no subtitles; "" means
// "keep the previous choice".
func nullStrKeep(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// State returns a person's state for one item (zero values when none).
func (s *Store) State(ctx context.Context, userID string, r Ref) (*State, error) {
	st := &State{Ref: r}
	var show, file, audio, sub, last sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT show_id, file_id, position_secs, duration_secs, played, play_count,
			audio_language, subtitle_language, last_played_at FROM playback_state
		WHERE user_id = ? AND library_id = ? AND item_type = ? AND item_id = ?`, userID, r.LibraryID, r.Type, r.ID).
		Scan(&show, &file, &st.PositionSecs, &st.DurationSecs, &st.Played, &st.PlayCount, &audio, &sub, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	st.ShowID, st.FileID, st.AudioLanguage, st.SubtitleLanguage, st.LastPlayedAt =
		show.String, file.String, audio.String, sub.String, last.String
	return st, nil
}

// States returns all of a person's playback states, keyed by Ref.Key().
func (s *Store) States(ctx context.Context, userID string) (map[string]*State, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT library_id, item_type, item_id, show_id, file_id, position_secs,
			duration_secs, played, play_count, audio_language, subtitle_language, last_played_at
		FROM playback_state WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*State{}
	for rows.Next() {
		st := &State{}
		var show, file, audio, sub, last sql.NullString
		if err := rows.Scan(&st.LibraryID, &st.Type, &st.ID, &show, &file, &st.PositionSecs, &st.DurationSecs,
			&st.Played, &st.PlayCount, &audio, &sub, &last); err != nil {
			return nil, err
		}
		st.ShowID, st.FileID, st.AudioLanguage, st.SubtitleLanguage, st.LastPlayedAt =
			show.String, file.String, audio.String, sub.String, last.String
		out[st.Key()] = st
	}
	return out, rows.Err()
}

// SetPlayed marks items watched or unwatched by hand. Marking watched logs
// a history entry; marking unwatched clears progress too.
func (s *Store) SetPlayed(ctx context.Context, userID string, refs []Ref, showIDs map[string]string, played bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	t := now()
	for _, r := range refs {
		if r.Type != "movie" && r.Type != "episode" {
			continue
		}
		show := showIDs[r.ID]
		if played {
			if _, err := tx.ExecContext(ctx, `INSERT INTO playback_state (user_id, library_id, item_type, item_id, show_id,
					position_secs, played, play_count, last_played_at, updated_at)
				VALUES (?, ?, ?, ?, ?, 0, 1, 1, ?, ?)
				ON CONFLICT(user_id, library_id, item_type, item_id) DO UPDATE SET played = 1, position_secs = 0,
					play_count = CASE WHEN playback_state.played = 1 THEN playback_state.play_count ELSE playback_state.play_count + 1 END,
					show_id = COALESCE(excluded.show_id, playback_state.show_id), updated_at = excluded.updated_at`,
				userID, r.LibraryID, r.Type, r.ID, nullStr(show), t, t); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO watch_history (user_id, library_id, item_type, item_id, show_id,
				watched_at, completed) VALUES (?, ?, ?, ?, ?, ?, 1)`, userID, r.LibraryID, r.Type, r.ID, nullStr(show), t); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE playback_state SET played = 0, position_secs = 0, updated_at = ?
				WHERE user_id = ? AND library_id = ? AND item_type = ? AND item_id = ?`,
				t, userID, r.LibraryID, r.Type, r.ID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ClearProgress forgets where a person stopped in an item ("remove from
// Continue Watching") without changing whether it is watched.
func (s *Store) ClearProgress(ctx context.Context, userID string, r Ref) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_state SET position_secs = 0, updated_at = ?
		WHERE user_id = ? AND library_id = ? AND item_type = ? AND item_id = ?`, now(), userID, r.LibraryID, r.Type, r.ID)
	return err
}

// InProgress lists a person's partly watched items, most recent first.
func (s *Store) InProgress(ctx context.Context, userID string, limit int) ([]State, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT library_id, item_type, item_id, COALESCE(show_id, ''),
			position_secs, duration_secs, COALESCE(last_played_at, '')
		FROM playback_state WHERE user_id = ? AND position_secs > 30 AND played = 0
		ORDER BY last_played_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []State
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.LibraryID, &st.Type, &st.ID, &st.ShowID, &st.PositionSecs, &st.DurationSecs,
			&st.LastPlayedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// RecentShows lists the shows a person watched recently (any episode
// activity), most recent first, as (library id, show id) pairs.
func (s *Store) RecentShows(ctx context.Context, userID string, limit int) ([]Ref, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT library_id, show_id, MAX(last_played_at) AS last
		FROM playback_state WHERE user_id = ? AND item_type = 'episode' AND show_id IS NOT NULL
		GROUP BY library_id, show_id ORDER BY last DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Ref
	for rows.Next() {
		var r Ref
		var last sql.NullString
		if err := rows.Scan(&r.LibraryID, &r.ID, &last); err != nil {
			return nil, err
		}
		r.Type = "show"
		out = append(out, r)
	}
	return out, rows.Err()
}

// HistoryEntry is one watch history row.
type HistoryEntry struct {
	EntryID int64 `json:"entry_id"`
	Ref
	ShowID      string  `json:"show_id,omitempty"`
	WatchedAt   string  `json:"watched_at"`
	WatchedSecs float64 `json:"watched_secs"`
}

// History lists a person's watch history, newest first.
func (s *Store) History(ctx context.Context, userID string, limit, offset int) ([]HistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, library_id, item_type, item_id, COALESCE(show_id, ''),
		watched_at, watched_secs FROM watch_history WHERE user_id = ? ORDER BY watched_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.EntryID, &h.LibraryID, &h.Type, &h.Ref.ID, &h.ShowID, &h.WatchedAt, &h.WatchedSecs); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteHistory removes one history entry.
func (s *Store) DeleteHistory(ctx context.Context, userID string, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM watch_history WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

// PlayCounts returns how many times items were finished, across everyone
// (for "Most Watched").
func (s *Store) PlayCounts(ctx context.Context, userID string, limit int) ([]struct {
	Ref
	Count int
}, error) {
	q := `SELECT library_id, item_type, COALESCE(show_id, item_id), item_type = 'episode', COUNT(*) AS n
		FROM watch_history WHERE user_id = ? GROUP BY library_id, COALESCE(show_id, item_id) ORDER BY n DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []struct {
		Ref
		Count int
	}
	for rows.Next() {
		var r struct {
			Ref
			Count int
		}
		var isEp bool
		if err := rows.Scan(&r.LibraryID, &r.Type, &r.ID, &isEp, &r.Count); err != nil {
			return nil, err
		}
		if isEp {
			r.Type = "show"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Lists.

// ListItem is an entry in a favourites or watchlist list.
type ListItem struct {
	Ref
	CreatedAt string `json:"created_at"`
}

func checkList(list string) error {
	if list != "favorite" && list != "watchlist" {
		return invalid("unknown list %q", list)
	}
	return nil
}

// SetInList adds or removes an item from a person's favourites or watchlist.
func (s *Store) SetInList(ctx context.Context, userID, list string, r Ref, in bool) error {
	if err := checkList(list); err != nil {
		return err
	}
	if r.Type != "movie" && r.Type != "show" && r.Type != "episode" {
		return invalid("unknown item type")
	}
	if in {
		_, err := s.db.ExecContext(ctx, `INSERT INTO user_items (user_id, list, library_id, item_type, item_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, userID, list, r.LibraryID, r.Type, r.ID, now())
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_items WHERE user_id = ? AND list = ? AND library_id = ?
		AND item_type = ? AND item_id = ?`, userID, list, r.LibraryID, r.Type, r.ID)
	return err
}

// List returns a person's favourites or watchlist, newest first.
func (s *Store) List(ctx context.Context, userID, list string) ([]ListItem, error) {
	if err := checkList(list); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT library_id, item_type, item_id, created_at FROM user_items
		WHERE user_id = ? AND list = ? ORDER BY created_at DESC`, userID, list)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ListItem{}
	for rows.Next() {
		var li ListItem
		if err := rows.Scan(&li.LibraryID, &li.Type, &li.ID, &li.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, li)
	}
	return out, rows.Err()
}

// ListMembership returns which items are in a person's lists, keyed by
// Ref.Key() → set of list names.
func (s *Store) ListMembership(ctx context.Context, userID string) (map[string]map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT list, library_id, item_type, item_id FROM user_items WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var list string
		var r Ref
		if err := rows.Scan(&list, &r.LibraryID, &r.Type, &r.ID); err != nil {
			return nil, err
		}
		k := r.Key()
		if out[k] == nil {
			out[k] = map[string]bool{}
		}
		out[k][list] = true
	}
	return out, rows.Err()
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("a name is required")
	}
	if len(name) > 200 {
		return "", invalid("the name is too long")
	}
	return name, nil
}
