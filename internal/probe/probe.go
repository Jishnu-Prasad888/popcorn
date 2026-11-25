// Package probe inspects video files with ffprobe: the container, duration,
// every video, audio and subtitle stream, and the chapter markers.
//
// It never writes to the file. The result is plain data the catalog stores in
// the library database (video_files, media_streams) and the player uses to
// decide between direct play, remuxing and transcoding.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stream is one stream inside a media file.
type Stream struct {
	Index             int    `json:"index"`
	Kind              string `json:"kind"` // video, audio, subtitle, attachment, data
	TypeIndex         int    `json:"type_index"`
	Codec             string `json:"codec"`
	Profile           string `json:"profile,omitempty"`
	Language          string `json:"language,omitempty"`
	Title             string `json:"title,omitempty"`
	Channels          int    `json:"channels,omitempty"`
	ChannelLayout     string `json:"channel_layout,omitempty"`
	SampleRate        int    `json:"sample_rate,omitempty"`
	Bitrate           int64  `json:"bitrate,omitempty"`
	Width             int    `json:"width,omitempty"`
	Height            int    `json:"height,omitempty"`
	PixFmt            string `json:"pix_fmt,omitempty"`
	FrameRate         float64 `json:"frame_rate,omitempty"`
	ColorTransfer     string `json:"color_transfer,omitempty"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
}

// Chapter is a chapter marker.
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title,omitempty"`
}

// Result is everything ffprobe reports about a file that Popcorn uses.
type Result struct {
	Container string    `json:"container"`
	Duration  float64   `json:"duration"`
	Bitrate   int64     `json:"bitrate"`
	Streams   []Stream  `json:"streams"`
	Chapters  []Chapter `json:"chapters"`
}

// Video returns the main video stream, if there is one. Cover art attached as
// a video stream (mjpeg/png pictures) is skipped.
func (r *Result) Video() *Stream {
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.Kind == "video" && !isPictureCodec(s.Codec) {
			return s
		}
	}
	return nil
}

// HDR names the HDR transfer of the main video stream, or "".
func (r *Result) HDR() string {
	v := r.Video()
	if v == nil {
		return ""
	}
	switch v.ColorTransfer {
	case "smpte2084":
		return "HDR10"
	case "arib-std-b67":
		return "HLG"
	}
	return ""
}

func isPictureCodec(c string) bool {
	switch c {
	case "mjpeg", "png", "bmp", "gif", "webp":
		return true
	}
	return false
}

var (
	binOnce sync.Once
	binPath string
)

func ffprobePath() string {
	binOnce.Do(func() {
		if p, err := exec.LookPath("ffprobe"); err == nil {
			binPath = p
		}
	})
	return binPath
}

// Available reports whether ffprobe is installed.
func Available() bool { return ffprobePath() != "" }

// ErrUnavailable is returned when ffprobe is not installed.
var ErrUnavailable = errors.New("ffprobe is not installed")

// File probes the media file at path.
func File(ctx context.Context, path string) (*Result, error) {
	bin := ffprobePath()
	if bin == "" {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters",
		"--", path)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffprobe: %s", msg)
	}
	return Parse(out.Bytes())
}

type rawOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index         int               `json:"index"`
		CodecType     string            `json:"codec_type"`
		CodecName     string            `json:"codec_name"`
		Profile       string            `json:"profile"`
		Channels      int               `json:"channels"`
		ChannelLayout string            `json:"channel_layout"`
		SampleRate    string            `json:"sample_rate"`
		BitRate       string            `json:"bit_rate"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		PixFmt        string            `json:"pix_fmt"`
		AvgFrameRate  string            `json:"avg_frame_rate"`
		RFrameRate    string            `json:"r_frame_rate"`
		ColorTransfer string            `json:"color_transfer"`
		Tags          map[string]string `json:"tags"`
		Disposition   map[string]int    `json:"disposition"`
	} `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

// Parse decodes ffprobe's JSON output.
func Parse(data []byte) (*Result, error) {
	var raw rawOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode ffprobe output: %w", err)
	}
	res := &Result{
		Container: normalizeContainer(raw.Format.FormatName),
		Duration:  parseFloat(raw.Format.Duration),
		Bitrate:   int64(parseFloat(raw.Format.BitRate)),
		Streams:   []Stream{},
		Chapters:  []Chapter{},
	}
	counts := map[string]int{}
	for _, s := range raw.Streams {
		kind := s.CodecType
		switch kind {
		case "video", "audio", "subtitle", "attachment":
		default:
			kind = "data"
		}
		tag := func(k string) string {
			for key, v := range s.Tags {
				if strings.EqualFold(key, k) {
					return strings.TrimSpace(v)
				}
			}
			return ""
		}
		st := Stream{
			Index:         s.Index,
			Kind:          kind,
			TypeIndex:     counts[kind],
			Codec:         s.CodecName,
			Profile:       s.Profile,
			Language:      normalizeLanguage(tag("language")),
			Title:         tag("title"),
			Channels:      s.Channels,
			ChannelLayout: s.ChannelLayout,
			SampleRate:    int(parseFloat(s.SampleRate)),
			Bitrate:       int64(parseFloat(s.BitRate)),
			Width:         s.Width,
			Height:        s.Height,
			PixFmt:        s.PixFmt,
			FrameRate:     parseRate(s.AvgFrameRate),
			ColorTransfer: s.ColorTransfer,
			IsDefault:     s.Disposition["default"] == 1,
			IsForced:      s.Disposition["forced"] == 1,
			IsHearingImpaired: s.Disposition["hearing_impaired"] == 1 ||
				strings.Contains(strings.ToLower(tag("title")), "sdh"),
		}
		if st.FrameRate == 0 {
			st.FrameRate = parseRate(s.RFrameRate)
		}
		if st.Kind == "subtitle" && !st.IsForced && strings.Contains(strings.ToLower(st.Title), "forced") {
			st.IsForced = true
		}
		counts[kind]++
		res.Streams = append(res.Streams, st)
	}
	for _, c := range raw.Chapters {
		title := ""
		for k, v := range c.Tags {
			if strings.EqualFold(k, "title") {
				title = v
			}
		}
		res.Chapters = append(res.Chapters, Chapter{
			Start: parseFloat(c.StartTime),
			End:   parseFloat(c.EndTime),
			Title: title,
		})
	}
	return res, nil
}

// normalizeContainer turns ffprobe's format list ("matroska,webm",
// "mov,mp4,m4a,3gp,3g2,mj2") into one short name.
func normalizeContainer(f string) string {
	switch {
	case strings.HasPrefix(f, "matroska"):
		return "mkv"
	case strings.HasPrefix(f, "mov,mp4"):
		return "mp4"
	case f == "avi":
		return "avi"
	case f == "mpegts":
		return "ts"
	case strings.HasPrefix(f, "mpeg"):
		return "mpeg"
	case f == "asf":
		return "wmv"
	case f == "flv":
		return "flv"
	}
	if i := strings.IndexByte(f, ','); i > 0 {
		return f[:i]
	}
	return f
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		return parseFloat(s)
	}
	n, d := parseFloat(num), parseFloat(den)
	if d == 0 {
		return 0
	}
	return n / d
}

// normalizeLanguage maps a stream language tag to an ISO 639-2 code, or "" for
// "und".
func normalizeLanguage(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if l == "" || l == "und" || l == "unk" {
		return ""
	}
	return NormalizeLanguage(l)
}
