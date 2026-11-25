package catalog

import "testing"

func TestParseMovie(t *testing.T) {
	cases := []struct {
		rel, title string
		year       int
		edition    string
		folder     string
	}{
		{"Inception (2010)/Inception (2010).mkv", "Inception", 2010, "", "Inception (2010)"},
		{"Inception (2010)/Inception.2010.1080p.BluRay.x264-GRP.mkv", "Inception", 2010, "", "Inception (2010)"},
		{"The.Dark.Knight.2008.2160p.UHD.BluRay.x265.mkv", "The Dark Knight", 2008, "", ""},
		{"Movies/Blade Runner (1982)/Blade Runner (1982) - Final Cut.mkv", "Blade Runner", 1982, "Final Cut", "Movies/Blade Runner (1982)"},
		{"Alien (1979) {edition-Director's Cut}.mkv", "Alien", 1979, "Director's Cut", ""},
		{"2001 A Space Odyssey (1968).mp4", "2001 A Space Odyssey", 1968, "", ""},
		{"Up.mkv", "Up", 0, "", ""},
		{"Blade Runner 2049 (2017)/movie.mkv", "Blade Runner 2049", 2017, "", "Blade Runner 2049 (2017)"},
	}
	for _, c := range cases {
		got := ParseMovie(c.rel)
		if got.Title != c.title || got.Year != c.year || got.Edition != c.edition || got.Folder != c.folder {
			t.Errorf("ParseMovie(%q) = %+v; want title=%q year=%d edition=%q folder=%q",
				c.rel, got, c.title, c.year, c.edition, c.folder)
		}
	}
}

func TestParseEpisode(t *testing.T) {
	cases := []struct {
		rel        string
		show       string
		folder     string
		season, ep int
		end        int
		title      string
	}{
		{"Show Name/Season 01/Show Name S01E01.mkv", "Show Name", "Show Name", 1, 1, 0, ""},
		{"Show Name/Season 02/Show Name S02E02 - The Return.mkv", "Show Name", "Show Name", 2, 2, 0, "The Return"},
		{"Show Name/Specials/Show Name S00E01.mkv", "Show Name", "Show Name", 0, 1, 0, ""},
		{"TV/Breaking Bad (2008)/Season 5/Breaking.Bad.S05E14.Ozymandias.1080p.mkv", "Breaking Bad", "TV/Breaking Bad (2008)", 5, 14, 0, "Ozymandias"},
		{"The.Office.US.S03E01E02.720p.mkv", "The Office US", "", 3, 1, 2, ""},
		{"Lost/1x05 - White Rabbit.avi", "Lost", "Lost", 1, 5, 0, "White Rabbit"},
		{"Anime/Frieren/[SubsPlease] Frieren - 12 (1080p) [ABCD].mkv", "Frieren", "Anime/Frieren", 1, 12, 0, ""},
		{"Dark/Season 1/03 - Past and Present.mkv", "Dark", "Dark", 1, 3, 0, "Past and Present"},
		{"Dark/Season 1/Episode 4.mkv", "Dark", "Dark", 1, 4, 0, ""},
	}
	for _, c := range cases {
		got, ok := ParseEpisode(c.rel)
		if !ok {
			t.Errorf("ParseEpisode(%q) not ok", c.rel)
			continue
		}
		if got.ShowTitle != c.show || got.ShowFolder != c.folder || got.Season != c.season ||
			got.Episode != c.ep || got.EpisodeEnd != c.end || got.Title != c.title {
			t.Errorf("ParseEpisode(%q) = %+v", c.rel, got)
		}
	}
	if _, ok := ParseEpisode("Movies/Inception (2010).mkv"); ok {
		t.Error("movie parsed as episode")
	}
}

func TestLooksLikeEpisode(t *testing.T) {
	if !LooksLikeEpisode("x/Season 1/a.mkv") || !LooksLikeEpisode("Show.S01E01.mkv") {
		t.Error("episode not detected")
	}
	if LooksLikeEpisode("Inception (2010)/Inception.mkv") {
		t.Error("movie detected as episode")
	}
}

func TestIsExtra(t *testing.T) {
	for _, rel := range []string{"Movie (2010)/Extras/Making of.mkv", "Movie (2010)/movie-trailer.mp4", "Movie/sample.mkv"} {
		if !IsExtra(rel) {
			t.Errorf("IsExtra(%q) = false", rel)
		}
	}
	if IsExtra("Movie (2010)/Movie.mkv") {
		t.Error("main feature flagged as extra")
	}
}

func TestParseSubtitleName(t *testing.T) {
	got := ParseSubtitleName("Movie (2010)", "Movie (2010).en.forced.srt")
	if got.Language != "eng" || !got.Forced {
		t.Errorf("got %+v", got)
	}
	got = ParseSubtitleName("Movie", "Movie.Hindi.sdh.srt")
	if got.Language != "hin" || !got.SDH {
		t.Errorf("got %+v", got)
	}
	got = ParseSubtitleName("Movie", "Movie.ja.ass")
	if got.Language != "jpn" {
		t.Errorf("got %+v", got)
	}
}

func TestSortTitle(t *testing.T) {
	if SortTitle("The Matrix") != "matrix" || SortTitle("A") != "a" {
		t.Error("sort title")
	}
}
