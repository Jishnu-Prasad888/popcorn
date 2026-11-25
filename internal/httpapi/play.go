package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
	"github.com/Jishnu-Prasad888/popcorn/internal/stream"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// subtitleTrack is a subtitle the player can show.
type subtitleTrack struct {
	ID                string `json:"id"`     // e<N> embedded, x<id> external
	Source            string `json:"source"` // embedded, sidecar, upload
	Language          string `json:"language,omitempty"`
	LanguageName      string `json:"language_name"`
	Title             string `json:"title,omitempty"`
	Format            string `json:"format"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
	IsDefault         bool   `json:"is_default"`
	ImageBased        bool   `json:"image_based"`
	TypeIndex         int    `json:"type_index"`
	URL               string `json:"url,omitempty"`
}

func subtitleTracks(libID string, f *catalog.FileInfo) []subtitleTrack {
	out := []subtitleTrack{}
	base := fmt.Sprintf("/api/v1/libraries/%s/files/%s/subtitles/", libID, f.ID)
	for _, st := range f.Streams {
		if st.Kind != "subtitle" {
			continue
		}
		t := subtitleTrack{ID: "e" + strconv.Itoa(st.TypeIndex), Source: "embedded", Language: st.Language,
			LanguageName: st.LanguageName, Title: st.Title, Format: st.Codec, IsForced: st.IsForced,
			IsHearingImpaired: st.IsHearingImpaired, IsDefault: st.IsDefault, ImageBased: st.ImageBased,
			TypeIndex: st.TypeIndex}
		if !t.ImageBased {
			t.URL = base + t.ID + ".vtt"
		}
		out = append(out, t)
	}
	for _, x := range f.Subtitles {
		out = append(out, subtitleTrack{ID: "x" + x.ID, Source: x.Source, Language: x.Language,
			LanguageName: x.LanguageName, Title: x.Title, Format: x.Format, IsForced: x.IsForced,
			IsHearingImpaired: x.IsHearingImpaired, TypeIndex: -1, URL: base + "x" + x.ID + ".vtt"})
	}
	return out
}

func containsLang(list []string, lang string) bool {
	if lang == "" {
		return false
	}
	for _, l := range list {
		if probe.NormalizeLanguage(l) == lang {
			return true
		}
	}
	return false
}

// chooseAudio picks the audio track to start with: the language last chosen
// for this item or show, the original language when preferred, then the
// person's languages in order, then the file's default track.
func chooseAudio(f *catalog.FileInfo, prefs userdata.Playback, remembered, original string) int {
	var audio []catalog.StreamInfo
	for _, st := range f.Streams {
		if st.Kind == "audio" {
			audio = append(audio, st)
		}
	}
	if len(audio) == 0 {
		return 0
	}
	find := func(lang string) int {
		if lang == "" {
			return -1
		}
		first := -1
		for _, a := range audio {
			if a.Language != lang {
				continue
			}
			// The main track wins over commentary and description tracks.
			t := strings.ToLower(a.Title)
			if !strings.Contains(t, "comment") && !strings.Contains(t, "descri") {
				return a.TypeIndex
			}
			if first < 0 {
				first = a.TypeIndex
			}
		}
		return first
	}
	if i := find(remembered); i >= 0 {
		return i
	}
	if prefs.PreferOriginal {
		if i := find(probe.NormalizeLanguage(original)); i >= 0 {
			return i
		}
	}
	for _, l := range prefs.AudioLanguages {
		if i := find(probe.NormalizeLanguage(l)); i >= 0 {
			return i
		}
	}
	for _, a := range audio {
		if a.IsDefault {
			return a.TypeIndex
		}
	}
	return audio[0].TypeIndex
}

// chooseSubtitle picks the subtitle to start with ("" for none), following
// the person's subtitle mode and languages, or the choice remembered for
// this item or show ("off" means none).
func chooseSubtitle(tracks []subtitleTrack, prefs userdata.Playback, audioLang, remembered string) string {
	pick := func(lang string, forced *bool) string {
		var best *subtitleTrack
		for i := range tracks {
			t := &tracks[i]
			if t.Language != lang {
				continue
			}
			if forced != nil && t.IsForced != *forced {
				continue
			}
			if t.ImageBased && best != nil {
				continue
			}
			score := func(x *subtitleTrack) int {
				s := 0
				if !x.ImageBased {
					s += 4
				}
				if x.IsHearingImpaired == prefs.PreferSDH {
					s += 2
				}
				if x.IsDefault {
					s++
				}
				return s
			}
			if best == nil || score(t) > score(best) {
				best = t
			}
		}
		if best == nil {
			return ""
		}
		return best.ID
	}
	yes, no := true, false
	if remembered == "off" {
		if prefs.SubtitleMode != "off" && audioLang != "" {
			return pick(audioLang, &yes) // forced subtitles still show foreign dialogue
		}
		return ""
	}
	if remembered != "" {
		if id := pick(remembered, &no); id != "" {
			return id
		}
		if id := pick(remembered, nil); id != "" {
			return id
		}
	}
	switch prefs.SubtitleMode {
	case "off":
		return ""
	case "forced":
		return pick(audioLang, &yes)
	case "always":
		for _, l := range prefs.SubtitleLanguages {
			if id := pick(probe.NormalizeLanguage(l), &no); id != "" {
				return id
			}
		}
	default: // auto
		if audioLang == "" || containsLang(prefs.AudioLanguages, audioLang) {
			return pick(audioLang, &yes)
		}
		for _, l := range prefs.SubtitleLanguages {
			if id := pick(probe.NormalizeLanguage(l), &no); id != "" {
				return id
			}
		}
	}
	return pick(audioLang, &yes)
}

// handlePlayInfo — GET /api/v1/libraries/{id}/play/{type}/{itemID}
//
// Everything the player needs to start an item: its versions and their
// streams, where to resume, intro/credits markers, the next item, and which
// audio and subtitle tracks to start with.
func (s *Server) handlePlayInfo(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	defer release()
	ctx := r.Context()
	itemType, itemID := r.PathValue("type"), r.PathValue("itemID")
	var card catalog.Card
	var files []catalog.FileInfo
	var markers = map[string]*float64{}
	var nextID, prevID, originalLang string
	switch itemType {
	case "movie":
		m, err := st.Movie(ctx, itemID)
		if err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		card, files, originalLang = m.Card, m.Files, m.OriginalLanguage
	case "episode":
		e, err := st.Episode(ctx, itemID)
		if err != nil {
			s.writeCatalogError(w, r, err)
			return
		}
		card, files = e.Card, e.Files
		markers["intro_start"], markers["intro_end"], markers["credits_start"] = e.IntroStart, e.IntroEnd, e.CreditsStart
		prevID, nextID, _ = st.AdjacentEpisodes(ctx, e.ShowID, e.ID)
		if sh, err := st.Show(ctx, e.ShowID); err == nil {
			originalLang = sh.OriginalLanguage
		}
	default:
		s.badRequest(w, r, "Only movies and episodes can be played.")
		return
	}
	var playable []catalog.FileInfo
	for _, f := range files {
		if f.Status == "present" {
			playable = append(playable, f)
		}
	}
	if len(playable) == 0 {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusConflict, CodeConflict,
			"No file of this title is available right now. Its disk may be disconnected.")
		return
	}
	chosen := &playable[0]
	if want := r.URL.Query().Get("file"); want != "" {
		for i := range playable {
			if playable[i].ID == want {
				chosen = &playable[i]
			}
		}
	}
	prefs := userdata.DefaultPlayback()
	var state *userdata.State
	rememberedAudio, rememberedSub := "", ""
	if s.userdata != nil {
		prefs, _ = s.userdata.Playback(ctx, u.ID)
		state, _ = s.userdata.State(ctx, u.ID, userdata.Ref{LibraryID: lib.ID, Type: itemType, ID: itemID})
		if state != nil {
			rememberedAudio, rememberedSub = state.AudioLanguage, state.SubtitleLanguage
		}
		// The choice made on the previous episode carries to the next.
		if itemType == "episode" && rememberedAudio == "" && rememberedSub == "" {
			if all, err := s.userdata.States(ctx, u.ID); err == nil {
				latest := ""
				for _, v := range all {
					if v.LibraryID == lib.ID && v.ShowID == card.ShowID && v.LastPlayedAt > latest {
						latest = v.LastPlayedAt
						rememberedAudio, rememberedSub = v.AudioLanguage, v.SubtitleLanguage
					}
				}
			}
		}
	}
	audio := chooseAudio(chosen, prefs, rememberedAudio, originalLang)
	audioLang := ""
	for _, a := range chosen.Streams {
		if a.Kind == "audio" && a.TypeIndex == audio {
			audioLang = a.Language
		}
	}
	tracks := subtitleTracks(lib.ID, chosen)
	sub := chooseSubtitle(tracks, prefs, audioLang, rememberedSub)
	resume := 0.0
	if state != nil && !state.Played && state.PositionSecs > 30 {
		resume = state.PositionSecs
	}
	var next, prev *catalog.Card
	if nextID != "" {
		if c, err := st.EpisodeCard(ctx, nextID); err == nil {
			next = c
		}
	}
	if prevID != "" {
		if c, err := st.EpisodeCard(ctx, prevID); err == nil {
			prev = c
		}
	}
	us := s.loadUserState(ctx, u.ID)
	card.State = us.stateFor(lib.ID, itemType, itemID, 0)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"item":         card,
		"files":        playable,
		"file_id":      chosen.ID,
		"subtitles":    tracks,
		"audio":        audio,
		"subtitle":     sub,
		"resume_secs":  resume,
		"markers":      markers,
		"next":         next,
		"previous":     prev,
		"preferences":  prefs,
		"ffmpeg":       stream.Available(),
		"direct_url":   fmt.Sprintf("/api/v1/libraries/%s/files/%s/direct", lib.ID, chosen.ID),
		"stream_url":   fmt.Sprintf("/api/v1/libraries/%s/files/%s/stream", lib.ID, chosen.ID),
		"decide_url":   fmt.Sprintf("/api/v1/libraries/%s/files/%s/decide", lib.ID, chosen.ID),
		"file_summary": fileSummary(chosen),
	})
}

func fileSummary(f *catalog.FileInfo) map[string]any {
	return map[string]any{"container": f.Container, "video_codec": f.VideoCodec, "resolution": f.Resolution,
		"width": f.Width, "height": f.Height, "bitrate": f.Bitrate, "hdr": f.HDR, "duration_secs": f.DurationSecs,
		"size_bytes": f.SizeBytes, "name": f.Name}
}

// playFile loads a file's stream description, checking it belongs to a
// movie or episode of the library and is on disk.
func (s *Server) playFile(w http.ResponseWriter, r *http.Request, u *auth.User) (*library.Library, *catalog.FileInfo, stream.Source, string, bool) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return nil, nil, stream.Source{}, "", false
	}
	defer release()
	fileID := r.PathValue("fileID")
	itemType, itemID, err := st.ItemForFile(r.Context(), fileID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return nil, nil, stream.Source{}, "", false
	}
	files, err := st.Files(r.Context(), itemType, itemID)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return nil, nil, stream.Source{}, "", false
	}
	var f *catalog.FileInfo
	for i := range files {
		if files[i].ID == fileID {
			f = &files[i]
		}
	}
	if f == nil || f.Status != "present" {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusNotFound, CodeNotFound, "The file is not available.")
		return nil, nil, stream.Source{}, "", false
	}
	src := stream.Source{Container: f.Container, Bitrate: f.Bitrate}
	for _, x := range f.Streams {
		src.Tracks = append(src.Tracks, stream.Track{Kind: x.Kind, TypeIndex: x.TypeIndex, Codec: x.Codec,
			Profile: x.Profile, Channels: x.Channels, Height: x.Height, IsDefault: x.IsDefault})
	}
	if v := src.Video(); v != nil {
		v.PixFmt = f.PixFmt
	}
	full := filepath.Join(lib.Root, filepath.FromSlash(f.RelPath))
	return lib, f, src, full, true
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.ToLower(s), ",")
}

func clientFromQuery(r *http.Request) stream.Client {
	q := r.URL.Query()
	c := stream.DefaultClient
	if v := splitList(q.Get("vc")); v != nil {
		c.VideoCodecs = v
	}
	if v := splitList(q.Get("ac")); v != nil {
		c.AudioCodecs = v
	}
	if v := splitList(q.Get("ct")); v != nil {
		c.Containers = v
	}
	c.Passthrough = splitList(q.Get("pt"))
	return c
}

func heightFor(q string) int {
	switch q {
	case "2160p":
		return 2160
	case "1080p":
		return 1080
	case "720p":
		return 720
	case "480p":
		return 480
	case "360p":
		return 360
	}
	return 0
}

func requestFromQuery(r *http.Request) stream.Request {
	q := r.URL.Query()
	req := stream.Request{Audio: intParam(r, "audio", -1), BurnSub: intParam(r, "burn", -1),
		MaxHeight: heightFor(q.Get("quality")), MaxBitrate: intParam(r, "bitrate", 0),
		Force: q.Get("method"), AudioDelay: intParam(r, "adelay", 0)}
	if req.Force != stream.MethodRemux && req.Force != stream.MethodTranscode {
		req.Force = ""
	}
	if req.AudioDelay > 10000 || req.AudioDelay < -10000 {
		req.AudioDelay = 0
	}
	return req
}

// handleDecide — GET /api/v1/libraries/{id}/files/{fileID}/decide
//
// How the file will play for this browser and these choices: direct play,
// remux or transcode, with the reasons and the URL to load.
func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, f, src, _, ok := s.playFile(w, r, u)
	if !ok {
		return
	}
	req := requestFromQuery(r)
	if req.Audio < 0 {
		req.Audio = src.DefaultAudio()
	}
	d := stream.Decide(src, req, clientFromQuery(r))
	if d.Method != stream.MethodDirect && !stream.Available() {
		d.Reasons = append(d.Reasons, "ffmpeg is not installed on the server, so the original file is tried directly")
		d.Method = stream.MethodDirect
	}
	u2 := fmt.Sprintf("/api/v1/libraries/%s/files/%s/direct", lib.ID, f.ID)
	if d.Method != stream.MethodDirect {
		v := url.Values{}
		v.Set("audio", strconv.Itoa(req.Audio))
		if req.BurnSub >= 0 {
			v.Set("burn", strconv.Itoa(req.BurnSub))
		}
		if q := r.URL.Query().Get("quality"); q != "" {
			v.Set("quality", q)
		}
		if req.MaxBitrate > 0 {
			v.Set("bitrate", strconv.Itoa(req.MaxBitrate))
		}
		if req.AudioDelay != 0 {
			v.Set("adelay", strconv.Itoa(req.AudioDelay))
		}
		v.Set("method", d.Method)
		for _, k := range []string{"vc", "ac", "ct", "pt"} {
			if x := r.URL.Query().Get(k); x != "" {
				v.Set(k, x)
			}
		}
		u2 = fmt.Sprintf("/api/v1/libraries/%s/files/%s/stream?%s", lib.ID, f.ID, v.Encode())
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"decision": d, "url": u2, "audio": req.Audio,
		"duration_secs": f.DurationSecs})
}

var directTypes = map[string]string{
	"mp4": "video/mp4", "m4v": "video/mp4", "mov": "video/quicktime", "webm": "video/webm",
	"mkv": "video/x-matroska", "avi": "video/x-msvideo", "ts": "video/mp2t", "ogv": "video/ogg",
}

// handleDirect — GET /api/v1/libraries/{id}/files/{fileID}/direct
//
// The original file, with range requests, for direct play.
func (s *Server) handleDirect(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, f, _, full, ok := s.playFile(w, r, u)
	if !ok {
		return
	}
	fh, err := os.Open(full)
	if err != nil {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusNotFound, CodeNotFound, "The file is not available.")
		return
	}
	defer func() { _ = fh.Close() }()
	info, err := fh.Stat()
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(full)), ".")
	ct := directTypes[ext]
	if ct == "" {
		ct = mime.TypeByExtension("." + ext)
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, no-transform")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = f
	http.ServeContent(w, r, "", info.ModTime(), fh)
}

// handleStream — GET /api/v1/libraries/{id}/files/{fileID}/stream
//
// A remuxed or transcoded fragmented-MP4 stream from ?start= seconds. Seeking
// past what has been sent restarts the stream at the new position; the
// session parameter lets a new request replace the old one at once.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, f, src, full, ok := s.playFile(w, r, u)
	if !ok {
		return
	}
	if s.transcoder == nil || !stream.Available() {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable,
			"ffmpeg is not installed on the server, so this video cannot be converted for the browser.")
		return
	}
	req := requestFromQuery(r)
	if req.Audio < 0 {
		req.Audio = src.DefaultAudio()
	}
	if req.BurnSub >= 0 {
		if t := src.Subtitle(req.BurnSub); t == nil || !stream.ImageSubtitle(t.Codec) {
			req.BurnSub = -1
		}
	}
	d := stream.Decide(src, req, clientFromQuery(r))
	if d.Method == stream.MethodDirect {
		d.Method = stream.MethodRemux
	}
	if req.Force == stream.MethodTranscode && d.VideoCopy {
		d.Method, d.VideoCopy = stream.MethodTranscode, false
	}
	start := floatParam(r, "start")
	if start < 0 || (f.DurationSecs > 0 && start > f.DurationSecs) {
		start = 0
	}
	// A stream that copies video starts on a keyframe. Aim just past the one
	// the seek lands on (see probe.SeekPastKeyframe), so ffmpeg starts on the
	// same keyframe /keyframe reported and the player's clock stays true.
	seekTo, zero := start, start
	if d.VideoCopy && start > 0 {
		if l, err := probe.SeekLanding(r.Context(), full, start); err == nil {
			seekTo, zero = l.Keyframe+probe.SeekPastKeyframe, l.Zero
			if f.DurationSecs > 0 && seekTo > f.DurationSecs {
				seekTo = l.Keyframe
			}
		}
	}
	session := r.URL.Query().Get("session")
	if len(session) > 64 {
		session = session[:64]
	}
	if session != "" {
		session = u.ID + ":" + session
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Popcorn-Method", d.Method)
	w.Header().Set("X-Popcorn-Start", strconv.FormatFloat(zero, 'f', 3, 64))
	// A transcode can run longer than the server's write timeout.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(zeroTime)
	err := s.transcoder.Run(r.Context(), stream.Job{
		Path: full, Start: seekTo, Source: src, Request: req, Decision: d,
		Session: stream.SessionInfo{Session: session, UserID: u.ID, Username: u.Username, Title: f.Name},
	}, flushWriter{w: w, rc: rc})
	if errors.Is(err, stream.ErrBusy) {
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusServiceUnavailable, CodeServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		s.logger.Warn("stream ended with error", "file", f.RelPath, "error", err)
	}
}

// handleKeyframe reports where a stream that copies video really starts
// when asked to start at ?at=: the keyframe it lands on, and the source time
// that becomes 0 in the stream (zero), which the player offsets its clock by.
func (s *Server) handleKeyframe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	_, f, _, full, ok := s.playFile(w, r, u)
	if !ok {
		return
	}
	at := floatParam(r, "at")
	if at < 0 || (f.DurationSecs > 0 && at > f.DurationSecs) {
		at = 0
	}
	l, err := probe.SeekLanding(r.Context(), full, at)
	if err != nil {
		s.logger.Debug("seek landing lookup failed", "file", f.RelPath, "error", err)
	}
	writeJSON(w, s.logger, http.StatusOK, l)
}

// flushWriter flushes after every write so the browser gets fragments as
// ffmpeg produces them.
type flushWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err == nil {
		_ = f.rc.Flush()
	}
	return n, err
}

// handleSubtitle — GET /api/v1/libraries/{id}/files/{fileID}/subtitles/{track}
//
// A subtitle track as WebVTT: an embedded text track (e<N>) extracted from
// the file, or an external subtitle (x<id>) converted from SRT/ASS/SSA/SUB.
// Conversions are cached in .popcorn/cache; originals are only read.
func (s *Server) handleSubtitle(w http.ResponseWriter, r *http.Request, u *auth.User) {
	lib, st, release, ok := s.withLibrary(w, r, u)
	if !ok {
		return
	}
	fileID := r.PathValue("fileID")
	track := strings.TrimSuffix(r.PathValue("track"), ".vtt")
	rel, status, size, mtime, err := st.FileOf(r.Context(), fileID)
	if err != nil {
		release()
		s.writeCatalogError(w, r, err)
		return
	}
	var srcPath string
	typeIndex := -1
	switch {
	case strings.HasPrefix(track, "e"):
		typeIndex, err = strconv.Atoi(track[1:])
		if err != nil || typeIndex < 0 {
			release()
			s.badRequest(w, r, "Unknown subtitle track.")
			return
		}
		if status != "present" {
			release()
			s.writeCatalogError(w, r, catalog.ErrNotFound)
			return
		}
		srcPath = filepath.Join(lib.Root, filepath.FromSlash(rel))
	case strings.HasPrefix(track, "x"):
		sub, owner, err := st.Subtitle(r.Context(), track[1:])
		if err != nil || owner != fileID {
			release()
			s.writeCatalogError(w, r, catalog.ErrNotFound)
			return
		}
		if sub.Source == "sidecar" {
			srcPath = filepath.Join(lib.Root, filepath.FromSlash(sub.Path))
		} else {
			srcPath = filepath.Join(popcornDirOf(lib), filepath.FromSlash(sub.Path))
		}
		if info, err := os.Stat(srcPath); err == nil {
			size, mtime = info.Size(), info.ModTime().String()
		}
	default:
		release()
		s.badRequest(w, r, "Unknown subtitle track.")
		return
	}
	release()
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s", fileID, track, size, mtime)))
	cache := filepath.Join(popcornDirOf(lib), "cache", "subtitles", hex.EncodeToString(sum[:12])+".vtt")
	var data []byte
	if typeIndex < 0 && strings.EqualFold(filepath.Ext(srcPath), ".vtt") {
		data, err = os.ReadFile(srcPath)
	} else {
		data, err = stream.SubtitleToVTT(r.Context(), srcPath, typeIndex, cache)
	}
	if err != nil {
		s.logger.Warn("subtitle conversion", "file", rel, "track", track, "error", err)
		writeError(w, s.logger, requestIDOrEmpty(r), http.StatusUnprocessableEntity, CodeBadRequest,
			"This subtitle could not be converted for the player.")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

type progressBody struct {
	LibraryID        string  `json:"library_id"`
	Type             string  `json:"type"`
	ID               string  `json:"id"`
	FileID           string  `json:"file_id"`
	PositionSecs     float64 `json:"position_secs"`
	DurationSecs     float64 `json:"duration_secs"`
	AudioLanguage    string  `json:"audio_language"`
	SubtitleLanguage string  `json:"subtitle_language"`
	Ended            bool    `json:"ended"`
	Session          string  `json:"session"`
	PlaylistID       string  `json:"playlist_id"`
	PlaylistEntry    string  `json:"playlist_entry"`
}

// handleProgress — POST /api/v1/playback/progress
//
// The player reports where it is every few seconds and when it stops. The
// item counts as watched once the credits start (or near the end).
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body progressBody
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	lib, err := s.libraries.Get(r.Context(), body.LibraryID)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	if !s.canReadLibrary(r, u, lib.ID) && u.Role != auth.RoleAdmin {
		s.writeForbidden(w, r)
		return
	}
	p := userdata.Progress{Ref: userdata.Ref{LibraryID: lib.ID, Type: body.Type, ID: body.ID},
		FileID: body.FileID, PositionSecs: body.PositionSecs, DurationSecs: body.DurationSecs,
		AudioLanguage: body.AudioLanguage, SubtitleLanguage: body.SubtitleLanguage, Ended: body.Ended}
	if st, release, err := s.openCatalog(lib); err == nil {
		if body.Type == "episode" {
			if e, err := st.Episode(r.Context(), body.ID); err == nil {
				p.ShowID = e.ShowID
				if e.CreditsStart != nil {
					p.CreditsStart = *e.CreditsStart
				}
			}
		}
		release()
	}
	state, err := s.userdata.ReportProgress(r.Context(), u.ID, p)
	if err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	if body.PlaylistID != "" && body.PlaylistEntry != "" {
		pos := body.PositionSecs
		if state.Played && state.PositionSecs == 0 {
			pos = 0
		}
		_ = s.userdata.SetEntryPosition(r.Context(), u.ID, body.PlaylistID, body.PlaylistEntry, pos)
	}
	writeJSON(w, s.logger, http.StatusOK, state)
}

// handleStopStream — POST /api/v1/playback/stop {session}
func (s *Server) handleStopStream(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Session string `json:"session"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if s.transcoder != nil && body.Session != "" {
		s.transcoder.Stop(u.ID + ":" + body.Session)
	}
	w.WriteHeader(http.StatusNoContent)
}

type refBody struct {
	LibraryID string `json:"library_id"`
	Type      string `json:"type"`
	ID        string `json:"id"`
}

// expandPlayable turns references to movies, episodes, seasons and shows
// into the movies and episodes they stand for, in viewing order.
func (s *Server) expandPlayable(r *http.Request, u *auth.User, refs []refBody) ([]userdata.Ref, map[string]string, error) {
	var out []userdata.Ref
	showOf := map[string]string{}
	for _, ref := range refs {
		lib, err := s.libraries.Get(r.Context(), ref.LibraryID)
		if err != nil {
			return nil, nil, err
		}
		if u.Role != auth.RoleAdmin && !s.canReadLibrary(r, u, lib.ID) {
			return nil, nil, catalog.ErrNotFound
		}
		switch ref.Type {
		case "movie":
			out = append(out, userdata.Ref{LibraryID: lib.ID, Type: "movie", ID: ref.ID})
		case "episode", "season", "show":
			st, release, err := s.openCatalog(lib)
			if err != nil {
				return nil, nil, err
			}
			var eps []catalog.Card
			switch ref.Type {
			case "episode":
				var c *catalog.Card
				c, err = st.EpisodeCard(r.Context(), ref.ID)
				if c != nil {
					eps = []catalog.Card{*c}
				}
			case "season":
				eps, err = st.Episodes(r.Context(), "", ref.ID, "")
			case "show":
				eps, err = st.Episodes(r.Context(), ref.ID, "", "")
			}
			release()
			if err != nil {
				return nil, nil, err
			}
			for _, e := range eps {
				if ref.Type == "show" && e.SeasonNumber == 0 {
					continue // specials are opt-in when a whole show is queued
				}
				out = append(out, userdata.Ref{LibraryID: lib.ID, Type: "episode", ID: e.ID})
				showOf[e.ID] = e.ShowID
			}
		}
	}
	return out, showOf, nil
}

// handleSetPlayed — POST /api/v1/playback/played {items, played}
//
// Marks movies, episodes, whole seasons or whole shows watched or unwatched.
func (s *Server) handleSetPlayed(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Items  []refBody `json:"items"`
		Played bool      `json:"played"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	refs, showOf, err := s.expandPlayable(r, u, body.Items)
	if err != nil {
		if errors.Is(err, library.ErrNotFound) {
			s.writeLibraryError(w, r, err)
			return
		}
		s.writeCatalogError(w, r, err)
		return
	}
	if err := s.userdata.SetPlayed(r.Context(), u.ID, refs, showOf, body.Played); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"updated": len(refs)})
}

// handleClearProgress — DELETE /api/v1/playback/progress {library_id,type,id}
//
// Removes an item from Continue Watching.
func (s *Server) handleClearProgress(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body refBody
	if err := readJSON(w, r, &body); err != nil {
		writeDomainError(w, s.logger, requestIDOrEmpty(r), err)
		return
	}
	if err := s.userdata.ClearProgress(r.Context(), u.ID, userdata.Ref{LibraryID: body.LibraryID, Type: body.Type, ID: body.ID}); err != nil {
		s.writeCatalogError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleActiveStreams — GET /api/v1/streams (admin)
func (s *Server) handleActiveStreams(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	out := []stream.SessionInfo{}
	if s.transcoder != nil {
		out = append(out, s.transcoder.Active()...)
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"streams": out})
}

var zeroTime time.Time
