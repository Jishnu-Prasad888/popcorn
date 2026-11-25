// Package catalog turns a library's indexed video files into movies, TV shows,
// seasons and episodes, and answers the queries the media server's pages
// need.
//
// The filesystem stays the source of truth: the catalog reads names and
// folders, never renames or moves anything, and every row references files by
// their indexed_files id.
package catalog

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
)

// videoExts are the containers Popcorn catalogues as playable video.
var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".webm": true,
	".mov": true, ".wmv": true, ".ts": true, ".m2ts": true, ".mts": true,
	".mpg": true, ".mpeg": true, ".flv": true, ".ogv": true, ".3gp": true,
	".divx": true, ".vob": true, ".iso": false,
}

// subtitleExts are the external subtitle formats Popcorn understands.
var subtitleExts = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true,
}

// IsVideo reports whether rel names a video file Popcorn plays.
func IsVideo(rel string) bool { return videoExts[strings.ToLower(path.Ext(rel))] }

// IsSubtitle reports whether rel names an external subtitle file.
func IsSubtitle(rel string) bool { return subtitleExts[strings.ToLower(path.Ext(rel))] }

// SubtitleFormat returns the format name ("srt", "ass"...) of a subtitle path.
func SubtitleFormat(rel string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(rel)), ".")
}

// extraFolders are folder names that hold bonus material rather than the main
// feature or episodes. Files in them are not catalogued as movies/episodes.
var extraFolders = map[string]bool{
	"extras": true, "extra": true, "featurettes": true, "behind the scenes": true,
	"deleted scenes": true, "interviews": true, "scenes": true, "shorts": true,
	"trailers": true, "trailer": true, "samples": true, "sample": true,
	"bonus": true, "other": true, "clips": true,
}

var extraSuffix = regexp.MustCompile(`(?i)[-_. ](trailer|sample|featurette|behindthescenes|deleted|interview|scene|short|extra)$`)

// IsExtra reports whether a video is bonus material (trailers, samples,
// featurettes) by its folder or name.
func IsExtra(rel string) bool {
	dir := path.Dir(rel)
	for _, seg := range strings.Split(dir, "/") {
		if extraFolders[strings.ToLower(seg)] {
			return true
		}
	}
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	if strings.EqualFold(base, "sample") || strings.EqualFold(base, "trailer") {
		return true
	}
	return extraSuffix.MatchString(base)
}

// ParsedMovie is what a movie's path says about it.
type ParsedMovie struct {
	Title   string
	Year    int
	Edition string
	// Folder is the movie's own folder (relative path) when the file sits in
	// one named after the movie, else "".
	Folder string
	IMDbID string
	TMDbID int
}

// ParsedEpisode is what an episode's path says about it.
type ParsedEpisode struct {
	ShowTitle  string
	ShowYear   int
	ShowFolder string // relative folder that identifies the show
	Season     int
	Episode    int
	EpisodeEnd int // last episode of a multi-episode file, or 0
	Title      string
	Absolute   bool // numbered without a season (anime style)
	Anime      bool
	Edition    string
	TMDbID     int
}

var (
	// S01E02, s1e2, S01E02E03, S01E02-E03, S01E02-03
	reSxE = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,3})[ ._-]?e(\d{1,4})(?:(?:[ ._-]?e|-e?)(\d{1,4}))?`)
	// 1x02, 01x02-03
	reNxN = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{1,2})x(\d{2,3})(?:-(\d{2,3}))?(?:[^a-z0-9]|$)`)
	// "Episode 3", "Ep 03", "E03"
	reEp = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:episode|ep|e)[ ._-]?(\d{1,4})(?:[^a-z0-9]|$)`)
	// Season folder: "Season 01", "S01", "Series 2", "Staffel 3", "Saison 1"
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|staffel|saison|temporada|s)[ ._-]*(\d{1,3})$`)
	reSpecials  = regexp.MustCompile(`(?i)^(?:specials?|season[ ._-]*0+|extras? episodes)$`)
	// Anime absolute numbering: "Show - 012", "[Group] Show - 12 [1080p]"
	reAbsolute = regexp.MustCompile(`(?:^|[ _.])-[ _.]?(\d{1,4})(?:v\d)?(?:[ _.\[(]|$)`)
	reYear     = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d{2})(?:[^0-9]|$)`)
	reYearPar  = regexp.MustCompile(`[(\[]((?:19|20)\d{2})[)\]]`)
	reIMDb     = regexp.MustCompile(`(?i)\b(tt\d{7,9})\b`)
	reTMDb     = regexp.MustCompile(`(?i)[\[{](?:tmdb|tmdbid)[-=: ](\d+)[\]}]`)
	reEdition  = regexp.MustCompile(`(?i)\{edition-([^}]+)\}`)
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reSpaces   = regexp.MustCompile(`\s+`)
)

// releaseTokens end the title part of a scene-style release name
// ("Movie.Name.2010.1080p.BluRay.x264-GROUP").
var releaseTokens = map[string]bool{
	"2160p": true, "1080p": true, "1080i": true, "720p": true, "576p": true, "480p": true,
	"4k": true, "uhd": true, "hdr": true, "hdr10": true, "dv": true, "dovi": true, "sdr": true,
	"bluray": true, "blu-ray": true, "bdrip": true, "brrip": true, "bdremux": true, "remux": true,
	"webrip": true, "web-dl": true, "webdl": true, "web": true, "hdtv": true, "dvdrip": true,
	"dvd": true, "dvdscr": true, "hdrip": true, "hdcam": true, "cam": true, "ts": true,
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true, "avc": true,
	"xvid": true, "divx": true, "av1": true, "10bit": true, "8bit": true,
	"aac": true, "ac3": true, "eac3": true, "dts": true, "dts-hd": true, "truehd": true,
	"atmos": true, "ddp5": true, "dd5": true, "flac": true, "mp3": true, "opus": true,
	"proper": true, "repack": true, "extended": true, "unrated": true, "remastered": true,
	"directors": true, "imax": true, "multi": true, "dual": true, "subbed": true, "dubbed": true,
	"amzn": true, "nf": true, "dsnp": true, "hmax": true, "atvp": true, "hulu": true,
	"internal": true, "limited": true, "complete": true,
}

// editionWords pick an edition label out of a file name.
var editionWords = []struct {
	re    *regexp.Regexp
	label string
}{
	{regexp.MustCompile(`(?i)director'?s[ ._-]?cut`), "Director's Cut"},
	{regexp.MustCompile(`(?i)extended([ ._-]?(cut|edition))?`), "Extended"},
	{regexp.MustCompile(`(?i)theatrical([ ._-]?(cut|edition))?`), "Theatrical"},
	{regexp.MustCompile(`(?i)unrated`), "Unrated"},
	{regexp.MustCompile(`(?i)ultimate[ ._-]?(cut|edition)`), "Ultimate Edition"},
	{regexp.MustCompile(`(?i)final[ ._-]?cut`), "Final Cut"},
	{regexp.MustCompile(`(?i)\bimax\b`), "IMAX"},
	{regexp.MustCompile(`(?i)remastered`), "Remastered"},
	{regexp.MustCompile(`(?i)special[ ._-]?edition`), "Special Edition"},
}

// ParseMovie reads a movie's title, year and edition from its relative path.
// The folder name wins over the file name when it carries a year, because
// "Title (Year)/" folders are the common convention and file names are often
// release names.
func ParseMovie(rel string) ParsedMovie {
	dir := path.Dir(rel)
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	var pm ParsedMovie

	if m := reIMDb.FindStringSubmatch(rel); m != nil {
		pm.IMDbID = strings.ToLower(m[1])
	}
	if m := reTMDb.FindStringSubmatch(rel); m != nil {
		pm.TMDbID, _ = strconv.Atoi(m[1])
	}
	pm.Edition = editionOf(base)

	fileTitle, fileYear := cleanTitle(base)
	if dir != "." {
		folder := path.Base(dir)
		folderTitle, folderYear := cleanTitle(folder)
		if !extraFolders[strings.ToLower(folder)] && folderTitle != "" &&
			(folderYear != 0 || sameTitle(folderTitle, fileTitle)) {
			pm.Title, pm.Year, pm.Folder = folderTitle, folderYear, dir
			if pm.Year == 0 {
				pm.Year = fileYear
			}
			return pm
		}
	}
	pm.Title, pm.Year = fileTitle, fileYear
	if pm.Title == "" {
		pm.Title = base
	}
	return pm
}

// LooksLikeEpisode reports whether a path reads as a TV episode: an SxxEyy or
// NxNN marker, or a file inside a "Season N"/"Specials" folder.
func LooksLikeEpisode(rel string) bool {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	if reSxE.MatchString(base) || reNxN.MatchString(base) {
		return true
	}
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if reSeasonDir.MatchString(seg) || reSpecials.MatchString(seg) {
			return true
		}
	}
	return false
}

// ParseEpisode reads show, season and episode from a relative path. ok is
// false when no episode number can be found.
//
// Recognised layouts:
//
//	Show/Season 01/Show S01E01.mkv
//	Show/S01E01 - Title.mkv
//	Show (2019)/Specials/Show S00E01.mkv
//	Show.Name.S02E05.1080p.mkv               (show from the file name)
//	Anime/Show/[Group] Show - 012 [1080p].mkv (absolute numbering)
func ParseEpisode(rel string) (ParsedEpisode, bool) {
	var pe ParsedEpisode
	dir := path.Dir(rel)
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	segs := []string{}
	if dir != "." {
		segs = strings.Split(dir, "/")
	}
	for _, s := range segs {
		if strings.EqualFold(s, "anime") {
			pe.Anime = true
		}
	}
	if m := reTMDb.FindStringSubmatch(rel); m != nil {
		pe.TMDbID, _ = strconv.Atoi(m[1])
	}
	pe.Edition = editionOf(base)

	// Season from the folder the file is in.
	seasonFromDir := -1
	showSegs := segs
	if n := len(segs); n > 0 {
		last := segs[n-1]
		if reSpecials.MatchString(last) {
			seasonFromDir = 0
			showSegs = segs[:n-1]
		} else if m := reSeasonDir.FindStringSubmatch(last); m != nil {
			seasonFromDir, _ = strconv.Atoi(m[1])
			showSegs = segs[:n-1]
		}
	}

	// Episode numbers from the file name.
	nameNoBrackets := reBrackets.ReplaceAllString(base, " ")
	var titleStart int = -1
	var prefix string
	switch {
	case reSxE.MatchString(nameNoBrackets):
		m := reSxE.FindStringSubmatchIndex(nameNoBrackets)
		pe.Season = atoi(nameNoBrackets[m[2]:m[3]])
		pe.Episode = atoi(nameNoBrackets[m[4]:m[5]])
		if m[6] >= 0 {
			pe.EpisodeEnd = atoi(nameNoBrackets[m[6]:m[7]])
		}
		prefix = nameNoBrackets[:m[0]]
		titleStart = m[1]
	case reNxN.MatchString(nameNoBrackets):
		m := reNxN.FindStringSubmatchIndex(nameNoBrackets)
		pe.Season = atoi(nameNoBrackets[m[2]:m[3]])
		pe.Episode = atoi(nameNoBrackets[m[4]:m[5]])
		if m[6] >= 0 {
			pe.EpisodeEnd = atoi(nameNoBrackets[m[6]:m[7]])
		}
		prefix = nameNoBrackets[:m[0]]
		titleStart = m[1]
	case reAbsolute.MatchString(nameNoBrackets) && seasonFromDir <= 0:
		m := reAbsolute.FindStringSubmatchIndex(nameNoBrackets)
		pe.Episode = atoi(nameNoBrackets[m[2]:m[3]])
		pe.Season = 1
		if seasonFromDir == 0 {
			pe.Season = 0
		}
		pe.Absolute = seasonFromDir < 0
		if pe.Absolute {
			pe.Anime = true
		}
		prefix = nameNoBrackets[:m[0]]
		titleStart = m[1]
	case seasonFromDir >= 0 && reEp.MatchString(nameNoBrackets):
		m := reEp.FindStringSubmatchIndex(nameNoBrackets)
		pe.Season = seasonFromDir
		pe.Episode = atoi(nameNoBrackets[m[2]:m[3]])
		prefix = nameNoBrackets[:m[0]]
		titleStart = m[1]
	case seasonFromDir >= 0:
		// "Season 1/03 - Title.mkv" or "Season 1/03.mkv"
		lead := strings.TrimLeftFunc(nameNoBrackets, unicode.IsSpace)
		i := 0
		for i < len(lead) && i < 4 && lead[i] >= '0' && lead[i] <= '9' {
			i++
		}
		if i == 0 {
			return pe, false
		}
		pe.Season = seasonFromDir
		pe.Episode = atoi(lead[:i])
		titleStart = len(nameNoBrackets) - len(lead) + i
	default:
		return pe, false
	}
	if seasonFromDir >= 0 && !pe.Absolute {
		// The folder is authoritative for specials kept as S00 in "Specials/".
		if seasonFromDir == 0 {
			pe.Season = 0
		}
	}
	if pe.EpisodeEnd != 0 && pe.EpisodeEnd <= pe.Episode {
		pe.EpisodeEnd = 0
	}
	if titleStart >= 0 && titleStart < len(nameNoBrackets) {
		pe.Title = cleanEpisodeTitle(nameNoBrackets[titleStart:])
	}

	// The show: the nearest folder above the season folder, else the part of
	// the file name before the episode marker.
	for i := len(showSegs) - 1; i >= 0; i-- {
		seg := showSegs[i]
		if extraFolders[strings.ToLower(seg)] || strings.EqualFold(seg, "anime") ||
			strings.EqualFold(seg, "tv") || strings.EqualFold(seg, "shows") ||
			strings.EqualFold(seg, "tv shows") || strings.EqualFold(seg, "series") {
			continue
		}
		t, y := cleanTitle(seg)
		if t == "" {
			continue
		}
		pe.ShowTitle, pe.ShowYear = t, y
		pe.ShowFolder = strings.Join(showSegs[:i+1], "/")
		break
	}
	if pe.ShowTitle == "" {
		t, y := cleanTitle(prefix)
		if t == "" {
			return pe, false
		}
		pe.ShowTitle, pe.ShowYear = t, y
		pe.ShowFolder = ""
	}
	return pe, true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func editionOf(base string) string {
	if m := reEdition.FindStringSubmatch(base); m != nil {
		return strings.TrimSpace(m[1])
	}
	// "Title (2010) - Director's Cut"
	for _, e := range editionWords {
		if e.re.MatchString(base) {
			return e.label
		}
	}
	return ""
}

// cleanTitle turns a folder or file name into a display title and year:
// separators become spaces, bracketed tags and release tokens are dropped,
// and a trailing year is taken out.
func cleanTitle(s string) (string, int) {
	year := 0
	if m := reYearPar.FindStringSubmatchIndex(s); m != nil {
		year = atoi(s[m[2]:m[3]])
		s = s[:m[0]] + " " + s[m[1]:]
		// Anything after a "(Year)" is edition or release noise.
		s = s[:m[0]]
	}
	s = reBrackets.ReplaceAllString(s, " ")
	// Scene names use dots or underscores for spaces; keep dots in
	// abbreviations ("S.W.A.T.") by only replacing when there are no spaces.
	if !strings.Contains(s, " ") {
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	} else {
		s = strings.ReplaceAll(s, "_", " ")
	}
	words := strings.Fields(s)
	out := make([]string, 0, len(words))
	for i, w := range words {
		lw := strings.ToLower(strings.Trim(w, "()-"))
		if releaseTokens[lw] {
			break
		}
		if year == 0 && i > 0 {
			if m := reYear.FindStringSubmatch(w); m != nil && len(strings.Trim(w, "()[]")) == 4 {
				year = atoi(m[1])
				break
			}
		}
		out = append(out, w)
	}
	t := strings.TrimSpace(strings.Join(out, " "))
	t = strings.Trim(t, " -–_.,")
	// "Title - Director's Cut" → "Title"
	if i := strings.Index(t, " - "); i > 0 {
		t = t[:i]
	}
	t = reSpaces.ReplaceAllString(t, " ")
	return t, year
}

func cleanEpisodeTitle(s string) string {
	s = strings.TrimLeft(s, " -–_.")
	if !strings.Contains(s, " ") {
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	}
	words := strings.Fields(s)
	out := []string{}
	for _, w := range words {
		if releaseTokens[strings.ToLower(strings.Trim(w, "()-"))] {
			break
		}
		out = append(out, w)
	}
	t := strings.Trim(strings.Join(out, " "), " -–_.")
	return t
}

func sameTitle(a, b string) bool { return NormalizeKey(a) == NormalizeKey(b) }

// NormalizeKey reduces a title to a comparison key: lower case, letters and
// digits only, "the/a/an" prefixes and "&"/"and" folded.
func NormalizeKey(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) > 1 && (words[0] == "the" || words[0] == "a" || words[0] == "an") {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// SortTitle is the title used for alphabetical order: leading articles move
// out of the way ("The Matrix" sorts under M).
func SortTitle(title string) string {
	t := strings.TrimSpace(title)
	lt := strings.ToLower(t)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lt, a) && len(t) > len(a) {
			return strings.ToLower(t[len(a):])
		}
	}
	return lt
}

// SubtitleInfo is what an external subtitle's file name says about it.
type SubtitleInfo struct {
	Language string
	Title    string
	Forced   bool
	SDH      bool
}

// ParseSubtitleName reads language and flags from a sidecar subtitle name
// relative to its video's base name: "Movie.en.forced.srt", "Movie.eng.sdh.srt",
// "Movie.English.srt".
func ParseSubtitleName(videoBase, subRel string) SubtitleInfo {
	var info SubtitleInfo
	base := strings.TrimSuffix(path.Base(subRel), path.Ext(subRel))
	rest := base
	if strings.HasPrefix(strings.ToLower(base), strings.ToLower(videoBase)) {
		rest = base[len(videoBase):]
	}
	parts := strings.FieldsFunc(rest, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' ' || r == '[' || r == ']' || r == '(' || r == ')'
	})
	var extra []string
	for _, p := range parts {
		lp := strings.ToLower(p)
		switch {
		case lp == "forced" || lp == "foreign":
			info.Forced = true
		case lp == "sdh" || lp == "hi" && info.Language != "" || lp == "cc":
			info.SDH = true
		case info.Language == "" && probe.IsLanguage(lp):
			info.Language = probe.NormalizeLanguage(lp)
		case lp == "default":
		default:
			extra = append(extra, p)
		}
	}
	if len(extra) > 0 {
		info.Title = strings.Join(extra, " ")
	}
	return info
}

// LocalArtworkKind maps a local artwork file name (poster.jpg, fanart.jpg,
// folder.jpg, logo.png, season01-poster.jpg) to an artwork kind. season is
// set for season posters, -1 otherwise.
func LocalArtworkKind(rel string) (kind string, season int, ok bool) {
	ext := strings.ToLower(path.Ext(rel))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		return "", -1, false
	}
	name := strings.ToLower(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	switch name {
	case "poster", "folder", "cover", "movie", "show":
		return "poster", -1, true
	case "fanart", "backdrop", "background", "art":
		return "backdrop", -1, true
	case "logo", "clearlogo":
		return "logo", -1, true
	case "season-specials-poster":
		return "poster", 0, true
	}
	if strings.HasPrefix(name, "season") && strings.HasSuffix(name, "-poster") {
		num := strings.TrimSuffix(strings.TrimPrefix(name, "season"), "-poster")
		if n, err := strconv.Atoi(num); err == nil {
			return "poster", n, true
		}
	}
	return "", -1, false
}
