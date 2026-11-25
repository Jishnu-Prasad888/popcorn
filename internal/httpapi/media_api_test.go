package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/librarydb"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// newMediaTestServer is a server with user data wired and a library holding
// two movies (one with two versions) and a show with two seasons.
func newMediaTestServer(t *testing.T) (*testClient, string) {
	t.Helper()
	_, client, libID, libRoot := newSearchTestServer(t)
	files := []string{
		"Movies/Blade Runner (1982)/Blade Runner (1982).mkv",
		"Movies/Blade Runner (1982)/Blade Runner (1982) - Final Cut.mkv",
		"Movies/Heat (1995)/Heat.1995.1080p.BluRay.mp4",
		"TV/Dark (2017)/Season 01/Dark.S01E01.Secrets.mkv",
		"TV/Dark (2017)/Season 01/Dark.S01E02.Lies.mkv",
		"TV/Dark (2017)/Season 02/Dark.S02E01.mkv",
	}
	for i, rel := range files {
		full := filepath.Join(libRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := seedLibraryFile(t, libRoot, "f"+string(rune('a'+i)), rel, 5); err != nil {
			t.Fatal(err)
		}
	}
	db, release, err := librarydb.Acquire(filepath.Join(libRoot, ".popcorn"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := catalog.Sync(context.Background(), db, catalog.KindMixed); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return client, libID
}

func decodeBody[T any](t *testing.T, rec interface{ Bytes() []byte }) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Bytes(), err)
	}
	return v
}

func TestMediaAPI_BrowsePlayAndTrack(t *testing.T) {
	client, libID := newMediaTestServer(t)
	rec := client.do(t, http.MethodGet, "/api/v1/movies?sort=title", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("movies = %d %s", rec.Code, rec.Body.String())
	}
	movies := decodeBody[struct {
		Items []catalog.Card `json:"items"`
		Total int            `json:"total"`
	}](t, rec.Body)
	if movies.Total != 2 || movies.Items[0].Title != "Blade Runner" || movies.Items[1].Title != "Heat" {
		t.Fatalf("movies = %+v", movies.Items)
	}
	if movies.Items[0].FileCount != 2 {
		t.Fatalf("Blade Runner versions = %d, want 2", movies.Items[0].FileCount)
	}

	rec = client.do(t, http.MethodGet, "/api/v1/shows", nil)
	shows := decodeBody[struct {
		Items []catalog.Card `json:"items"`
	}](t, rec.Body)
	if len(shows.Items) != 1 || shows.Items[0].Title != "Dark" || shows.Items[0].EpisodeCount != 3 {
		t.Fatalf("shows = %+v", shows.Items)
	}
	showID := shows.Items[0].ID

	rec = client.do(t, http.MethodGet, "/api/v1/libraries/"+libID+"/shows/"+showID, nil)
	detail := decodeBody[struct {
		Show   catalog.Show  `json:"show"`
		NextUp *catalog.Card `json:"next_up"`
	}](t, rec.Body)
	if len(detail.Show.Seasons) != 2 || detail.NextUp == nil || detail.NextUp.EpisodeNumber != 1 || detail.NextUp.SeasonNumber != 1 {
		t.Fatalf("show detail = %s", rec.Body.String())
	}
	first := detail.NextUp.ID

	// Watching the first episode to the end moves Next Up to the second.
	rec = client.do(t, http.MethodPost, "/api/v1/playback/progress", map[string]any{
		"library_id": libID, "type": "episode", "id": first, "position_secs": 2700, "duration_secs": 2800,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("progress = %d %s", rec.Code, rec.Body.String())
	}
	rec = client.do(t, http.MethodGet, "/api/v1/libraries/"+libID+"/shows/"+showID, nil)
	detail = decodeBody[struct {
		Show   catalog.Show  `json:"show"`
		NextUp *catalog.Card `json:"next_up"`
	}](t, rec.Body)
	if detail.NextUp == nil || detail.NextUp.EpisodeNumber != 2 {
		t.Fatalf("next up after watching E01 = %+v", detail.NextUp)
	}
	if detail.Show.State == nil || detail.Show.State.UnplayedCount != 2 {
		t.Fatalf("show state = %+v", detail.Show.State)
	}

	// Part-way through a movie lands it in Continue Watching.
	heat := movies.Items[1]
	client.do(t, http.MethodPost, "/api/v1/playback/progress", map[string]any{
		"library_id": libID, "type": "movie", "id": heat.ID, "position_secs": 600, "duration_secs": 10000,
	})
	rec = client.do(t, http.MethodGet, "/api/v1/home", nil)
	home := decodeBody[struct {
		Rows []homeRow `json:"rows"`
	}](t, rec.Body)
	var cont *homeRow
	for i := range home.Rows {
		if home.Rows[i].ID == "continue" {
			cont = &home.Rows[i]
		}
	}
	if cont == nil || len(cont.Items) != 1 || cont.Items[0].ID != heat.ID {
		t.Fatalf("continue watching = %+v", cont)
	}

	// Play info resumes where the movie was left.
	rec = client.do(t, http.MethodGet, "/api/v1/libraries/"+libID+"/play/movie/"+heat.ID, nil)
	play := decodeBody[struct {
		Resume float64 `json:"resume_secs"`
		FileID string  `json:"file_id"`
	}](t, rec.Body)
	if play.Resume != 600 || play.FileID == "" {
		t.Fatalf("play info = %s", rec.Body.String())
	}

	// A collection mixing a movie and a show, and its play queue.
	rec = client.do(t, http.MethodPost, "/api/v1/collections", map[string]any{
		"name": "Neo-noir",
		"items": []map[string]string{
			{"library_id": libID, "type": "movie", "id": movies.Items[0].ID},
			{"library_id": libID, "type": "show", "id": showID},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection = %d %s", rec.Code, rec.Body.String())
	}
	col := decodeBody[userdata.Collection](t, rec.Body)
	rec = client.do(t, http.MethodGet, "/api/v1/collections/"+col.ID+"/queue", nil)
	queue := decodeBody[struct {
		Queue []userdata.Ref `json:"queue"`
	}](t, rec.Body)
	if len(queue.Queue) != 4 || queue.Queue[0].Type != "movie" || queue.Queue[1].Type != "episode" {
		t.Fatalf("collection queue = %+v", queue.Queue)
	}

	// A playlist from a season.
	rec = client.do(t, http.MethodPost, "/api/v1/playlists", map[string]any{
		"name":  "Season one",
		"items": []map[string]string{{"library_id": libID, "type": "season", "id": detail.Show.Seasons[0].ID}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create playlist = %d %s", rec.Code, rec.Body.String())
	}
	pl := decodeBody[userdata.Playlist](t, rec.Body)
	rec = client.do(t, http.MethodGet, "/api/v1/playlists/"+pl.ID, nil)
	plv := decodeBody[struct {
		Entries []struct {
			Item *catalog.Card `json:"item"`
		} `json:"entries"`
	}](t, rec.Body)
	if len(plv.Entries) != 2 || plv.Entries[0].Item == nil || plv.Entries[0].Item.EpisodeNumber != 1 {
		t.Fatalf("playlist entries = %s", rec.Body.String())
	}

	// Search finds titles.
	rec = client.do(t, http.MethodGet, "/api/v1/search?q=blade", nil)
	sr := decodeBody[struct {
		Movies []catalog.Card `json:"movies"`
	}](t, rec.Body)
	if len(sr.Movies) != 1 {
		t.Fatalf("search = %s", rec.Body.String())
	}

	// Favourites.
	if rec := client.do(t, http.MethodPut, "/api/v1/me/lists/favorite/"+libID+"/movie/"+heat.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("favorite = %d %s", rec.Code, rec.Body.String())
	}
	rec = client.do(t, http.MethodGet, "/api/v1/collections/favorites", nil)
	fav := decodeBody[struct {
		Items []catalog.Card `json:"items"`
	}](t, rec.Body)
	if len(fav.Items) != 1 || fav.Items[0].State == nil || !fav.Items[0].State.Favorite {
		t.Fatalf("favourites = %s", rec.Body.String())
	}
}
