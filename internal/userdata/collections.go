package userdata

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
)

// Collection is a person's hand-made group of movies and shows.
type Collection struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	PosterVersion int    `json:"poster_version"`
	ItemCount     int    `json:"item_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	// Preview holds the first few items, for a cover mosaic.
	Preview []Ref `json:"preview"`
}

// CollectionItem is a member of a collection, in order.
type CollectionItem struct {
	Ref
	Position int    `json:"position"`
	AddedAt  string `json:"added_at"`
}

// Collections lists a person's collections by name.
func (s *Store) Collections(ctx context.Context, userID string) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.name, c.description, c.poster_version, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM collection_items i WHERE i.collection_id = c.id)
		FROM collections c WHERE c.owner_id = ? ORDER BY c.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	out := []Collection{}
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.PosterVersion, &c.CreatedAt, &c.UpdatedAt, &c.ItemCount); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	_ = rows.Close()
	for i := range out {
		items, err := s.CollectionItems(ctx, userID, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Preview = []Ref{}
		for j, it := range items {
			if j >= 4 {
				break
			}
			out[i].Preview = append(out[i].Preview, it.Ref)
		}
	}
	return out, rows.Err()
}

// Collection returns one of a person's collections.
func (s *Store) Collection(ctx context.Context, userID, id string) (*Collection, error) {
	var c Collection
	err := s.db.QueryRowContext(ctx, `SELECT id, name, description, poster_version, created_at, updated_at,
			(SELECT COUNT(*) FROM collection_items i WHERE i.collection_id = c.id)
		FROM collections c WHERE id = ? AND owner_id = ?`, id, userID).
		Scan(&c.ID, &c.Name, &c.Description, &c.PosterVersion, &c.CreatedAt, &c.UpdatedAt, &c.ItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// CreateCollection makes a new collection.
func (s *Store) CreateCollection(ctx context.Context, userID, name, description string) (*Collection, error) {
	name, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	id := newID()
	t := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO collections (id, owner_id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, userID, name, description, t, t); err != nil {
		return nil, err
	}
	return s.Collection(ctx, userID, id)
}

// UpdateCollection renames or re-describes a collection; nil leaves a field.
func (s *Store) UpdateCollection(ctx context.Context, userID, id string, name, description *string) error {
	if _, err := s.Collection(ctx, userID, id); err != nil {
		return err
	}
	if name != nil {
		n, err := cleanName(*name)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE collections SET name = ?, updated_at = ? WHERE id = ?`, n, now(), id); err != nil {
			return err
		}
	}
	if description != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE collections SET description = ?, updated_at = ? WHERE id = ?`, *description, now(), id); err != nil {
			return err
		}
	}
	return nil
}

// BumpCollectionPoster records a new uploaded poster.
func (s *Store) BumpCollectionPoster(ctx context.Context, userID, id string, delta int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE collections SET poster_version = CASE WHEN ? < 0 THEN 0 ELSE poster_version + 1 END,
		updated_at = ? WHERE id = ? AND owner_id = ?`, delta, now(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCollection deletes a collection (never its movies).
func (s *Store) DeleteCollection(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE id = ? AND owner_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DuplicateCollection copies a collection and its items under a new name.
func (s *Store) DuplicateCollection(ctx context.Context, userID, id, name string) (*Collection, error) {
	src, err := s.Collection(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = src.Name + " (copy)"
	}
	c, err := s.CreateCollection(ctx, userID, name, src.Description)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO collection_items (collection_id, library_id, item_type, item_id, position, added_at)
		SELECT ?, library_id, item_type, item_id, position, ? FROM collection_items WHERE collection_id = ?`, c.ID, now(), id); err != nil {
		return nil, err
	}
	return s.Collection(ctx, userID, c.ID)
}

// CollectionItems lists a collection's items in order.
func (s *Store) CollectionItems(ctx context.Context, userID, id string) ([]CollectionItem, error) {
	if _, err := s.Collection(ctx, userID, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT library_id, item_type, item_id, position, added_at FROM collection_items
		WHERE collection_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []CollectionItem{}
	for rows.Next() {
		var it CollectionItem
		if err := rows.Scan(&it.LibraryID, &it.Type, &it.ID, &it.Position, &it.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AddToCollection appends items (movies or shows) to a collection.
func (s *Store) AddToCollection(ctx context.Context, userID, id string, refs []Ref) error {
	if _, err := s.Collection(ctx, userID, id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var max int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) FROM collection_items WHERE collection_id = ?`, id).Scan(&max); err != nil {
		return err
	}
	for _, r := range refs {
		if r.Type != "movie" && r.Type != "show" {
			return invalid("collections hold movies and shows")
		}
		max++
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_items (collection_id, library_id, item_type, item_id, position, added_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, id, r.LibraryID, r.Type, r.ID, max, now()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE collections SET updated_at = ? WHERE id = ?`, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveFromCollection takes an item out of a collection.
func (s *Store) RemoveFromCollection(ctx context.Context, userID, id string, r Ref) error {
	if _, err := s.Collection(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM collection_items WHERE collection_id = ? AND library_id = ? AND item_type = ? AND item_id = ?`,
		id, r.LibraryID, r.Type, r.ID)
	return err
}

// ReorderCollection sets the order of a collection's items.
func (s *Store) ReorderCollection(ctx context.Context, userID, id string, order []Ref) error {
	if _, err := s.Collection(ctx, userID, id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, r := range order {
		if _, err := tx.ExecContext(ctx, `UPDATE collection_items SET position = ? WHERE collection_id = ? AND library_id = ?
			AND item_type = ? AND item_id = ?`, i, id, r.LibraryID, r.Type, r.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Playlist is an ordered play queue.
type Playlist struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Shuffle       bool   `json:"shuffle"`
	Repeat        string `json:"repeat"`
	CurrentEntry  string `json:"current_entry,omitempty"`
	PosterVersion int    `json:"poster_version"`
	ItemCount     int    `json:"item_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	Preview       []Ref  `json:"preview"`
}

// PlaylistEntry is one entry of a playlist.
type PlaylistEntry struct {
	EntryID      string  `json:"entry_id"`
	Ref
	Position     int     `json:"position"`
	PositionSecs float64 `json:"position_secs"`
	AddedAt      string  `json:"added_at"`
}

func scanPlaylist(row interface{ Scan(...any) error }) (*Playlist, error) {
	var p Playlist
	var cur sql.NullString
	var shuffle int
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &shuffle, &p.Repeat, &cur, &p.PosterVersion,
		&p.CreatedAt, &p.UpdatedAt, &p.ItemCount); err != nil {
		return nil, err
	}
	p.Shuffle, p.CurrentEntry = shuffle == 1, cur.String
	return &p, nil
}

const playlistSelect = `SELECT id, name, description, shuffle, repeat_mode, current_entry, poster_version,
	created_at, updated_at, (SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id) FROM playlists p`

// Playlists lists a person's playlists.
func (s *Store) Playlists(ctx context.Context, userID string) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, playlistSelect+` WHERE owner_id = ? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	out := []Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, *p)
	}
	_ = rows.Close()
	for i := range out {
		entries, err := s.PlaylistEntries(ctx, userID, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Preview = []Ref{}
		for j, e := range entries {
			if j >= 4 {
				break
			}
			out[i].Preview = append(out[i].Preview, e.Ref)
		}
	}
	return out, nil
}

// Playlist returns one playlist.
func (s *Store) Playlist(ctx context.Context, userID, id string) (*Playlist, error) {
	p, err := scanPlaylist(s.db.QueryRowContext(ctx, playlistSelect+` WHERE id = ? AND owner_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// CreatePlaylist makes a playlist.
func (s *Store) CreatePlaylist(ctx context.Context, userID, name, description string) (*Playlist, error) {
	name, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	id := newID()
	t := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO playlists (id, owner_id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, userID, name, description, t, t); err != nil {
		return nil, err
	}
	return s.Playlist(ctx, userID, id)
}

// PlaylistPatch changes a playlist; nil fields are left alone.
type PlaylistPatch struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Shuffle      *bool   `json:"shuffle"`
	Repeat       *string `json:"repeat"`
	CurrentEntry *string `json:"current_entry"`
}

// UpdatePlaylist applies a patch.
func (s *Store) UpdatePlaylist(ctx context.Context, userID, id string, p PlaylistPatch) error {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return err
	}
	set := func(col string, v any) error {
		_, err := s.db.ExecContext(ctx, `UPDATE playlists SET `+col+` = ?, updated_at = ? WHERE id = ?`, v, now(), id)
		return err
	}
	if p.Name != nil {
		n, err := cleanName(*p.Name)
		if err != nil {
			return err
		}
		if err := set("name", n); err != nil {
			return err
		}
	}
	if p.Description != nil {
		if err := set("description", *p.Description); err != nil {
			return err
		}
	}
	if p.Shuffle != nil {
		v := 0
		if *p.Shuffle {
			v = 1
		}
		if err := set("shuffle", v); err != nil {
			return err
		}
	}
	if p.Repeat != nil {
		if *p.Repeat != "off" && *p.Repeat != "all" && *p.Repeat != "one" {
			return invalid("repeat must be off, all or one")
		}
		if err := set("repeat_mode", *p.Repeat); err != nil {
			return err
		}
	}
	if p.CurrentEntry != nil {
		if err := set("current_entry", nullStr(*p.CurrentEntry)); err != nil {
			return err
		}
	}
	return nil
}

// BumpPlaylistPoster records a new uploaded poster (or its removal).
func (s *Store) BumpPlaylistPoster(ctx context.Context, userID, id string, delta int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE playlists SET poster_version = CASE WHEN ? < 0 THEN 0 ELSE poster_version + 1 END,
		updated_at = ? WHERE id = ? AND owner_id = ?`, delta, now(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePlaylist deletes a playlist.
func (s *Store) DeletePlaylist(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id = ? AND owner_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PlaylistEntries lists a playlist's entries in order.
func (s *Store) PlaylistEntries(ctx context.Context, userID, id string) ([]PlaylistEntry, error) {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, library_id, item_type, item_id, position, position_secs, added_at
		FROM playlist_items WHERE playlist_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []PlaylistEntry{}
	for rows.Next() {
		var e PlaylistEntry
		if err := rows.Scan(&e.EntryID, &e.LibraryID, &e.Type, &e.ID, &e.Position, &e.PositionSecs, &e.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddToPlaylist appends playable items (movies, episodes) to a playlist,
// optionally after the entry `after` ("" appends at the end).
func (s *Store) AddToPlaylist(ctx context.Context, userID, id string, refs []Ref) error {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var max int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) FROM playlist_items WHERE playlist_id = ?`, id).Scan(&max); err != nil {
		return err
	}
	for _, r := range refs {
		if r.Type != "movie" && r.Type != "episode" {
			return invalid("playlists hold movies and episodes")
		}
		max++
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items (id, playlist_id, library_id, item_type, item_id, position, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, newID(), id, r.LibraryID, r.Type, r.ID, max, now()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = ? WHERE id = ?`, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveFromPlaylist removes entries.
func (s *Store) RemoveFromPlaylist(ctx context.Context, userID, id string, entryIDs []string) error {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return err
	}
	for _, e := range entryIDs {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id = ? AND id = ?`, id, e); err != nil {
			return err
		}
	}
	return nil
}

// ReorderPlaylist sets the order of a playlist's entries.
func (s *Store) ReorderPlaylist(ctx context.Context, userID, id string, entryIDs []string) error {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, e := range entryIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE playlist_items SET position = ? WHERE playlist_id = ? AND id = ?`, i, id, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ShufflePlaylist puts a playlist's entries in random order.
func (s *Store) ShufflePlaylist(ctx context.Context, userID, id string) error {
	entries, err := s.PlaylistEntries(ctx, userID, id)
	if err != nil {
		return err
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.EntryID
	}
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	return s.ReorderPlaylist(ctx, userID, id, ids)
}

// SetEntryPosition saves the playback position inside a playlist entry.
func (s *Store) SetEntryPosition(ctx context.Context, userID, id, entryID string, secs float64) error {
	if _, err := s.Playlist(ctx, userID, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE playlist_items SET position_secs = ? WHERE playlist_id = ? AND id = ?`,
		secs, id, entryID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE playlists SET current_entry = ?, updated_at = ? WHERE id = ?`, entryID, now(), id)
	return err
}
