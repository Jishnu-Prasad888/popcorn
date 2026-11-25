package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoFFmpeg means ffmpeg is not installed.
var ErrNoFFmpeg = errors.New("ffmpeg is not installed")

// ErrBusy means every transcode slot is in use.
var ErrBusy = errors.New("the server is already transcoding as many streams as it allows")

var ffmpegBin = sync.OnceValue(func() string {
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return p
})

// Available reports whether ffmpeg is installed.
func Available() bool { return ffmpegBin() != "" }

// Config tunes transcoding.
type Config struct {
	// MaxTranscodes caps concurrent remux/transcode processes.
	MaxTranscodes int
	// VideoEncoder is the ffmpeg encoder for H.264 output: libx264 (default),
	// h264_nvenc, h264_qsv, h264_vaapi, h264_videotoolbox, h264_v4l2m2m.
	VideoEncoder string
	// Preset is the libx264 speed preset (default veryfast).
	Preset string
	// HWDevice is the VAAPI render node, used with h264_vaapi.
	HWDevice string
}

// Transcoder runs ffmpeg streams with a bound on concurrency. A new stream
// for the same playback session replaces the old one immediately, so seeking
// never leaves a stale ffmpeg running.
type Transcoder struct {
	cfg    Config
	logger *slog.Logger
	slots  chan struct{}

	mu       sync.Mutex
	sessions map[string]*running
}

type running struct {
	cancel  context.CancelFunc
	started time.Time
	info    SessionInfo
}

// SessionInfo describes an active stream, for the dashboard.
type SessionInfo struct {
	Session   string    `json:"session"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Title     string    `json:"title"`
	Method    string    `json:"method"`
	StartedAt time.Time `json:"started_at"`
}

// NewTranscoder returns a Transcoder.
func NewTranscoder(cfg Config, logger *slog.Logger) *Transcoder {
	if cfg.MaxTranscodes <= 0 {
		cfg.MaxTranscodes = 3
	}
	if cfg.VideoEncoder == "" {
		cfg.VideoEncoder = "libx264"
	}
	if cfg.Preset == "" {
		cfg.Preset = "veryfast"
	}
	if cfg.HWDevice == "" {
		cfg.HWDevice = "/dev/dri/renderD128"
	}
	return &Transcoder{cfg: cfg, logger: logger, slots: make(chan struct{}, cfg.MaxTranscodes),
		sessions: map[string]*running{}}
}

// Active lists running streams.
func (t *Transcoder) Active() []SessionInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]SessionInfo, 0, len(t.sessions))
	for _, r := range t.sessions {
		out = append(out, r.info)
	}
	return out
}

// Stop ends a session's stream, if one is running.
func (t *Transcoder) Stop(session string) {
	t.mu.Lock()
	r := t.sessions[session]
	delete(t.sessions, session)
	t.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

// StopAll ends every running stream (at shutdown).
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	all := t.sessions
	t.sessions = map[string]*running{}
	t.mu.Unlock()
	for _, r := range all {
		r.cancel()
	}
}

// Job is one stream to produce.
type Job struct {
	Path     string
	Start    float64
	Source   Source
	Request  Request
	Decision Decision
	Session  SessionInfo
}

// Args builds the ffmpeg command line for a job. Output is fragmented MP4 on
// stdout, which browsers play progressively from a plain <video> element.
func (t *Transcoder) Args(j Job) []string {
	d := j.Decision
	req := j.Request
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	vaapi := !d.VideoCopy && t.cfg.VideoEncoder == "h264_vaapi"
	if vaapi {
		args = append(args, "-vaapi_device", t.cfg.HWDevice)
	}
	ss := func() []string {
		if j.Start > 0 {
			return []string{"-ss", strconv.FormatFloat(j.Start, 'f', 3, 64)}
		}
		return nil
	}
	args = append(args, ss()...)
	args = append(args, "-i", j.Path)
	audioInput := 0
	if req.AudioDelay != 0 {
		// A second read of the same file, shifted, supplies the audio.
		args = append(args, "-itsoffset", strconv.FormatFloat(float64(req.AudioDelay)/1000, 'f', 3, 64))
		args = append(args, ss()...)
		args = append(args, "-i", j.Path)
		audioInput = 1
	}

	v := j.Source.Video()
	if v != nil {
		if d.VideoCopy {
			args = append(args, "-map", "0:v:0", "-c:v", "copy")
			if v.Codec == "hevc" {
				args = append(args, "-tag:v", "hvc1")
			}
		} else {
			filters := []string{}
			if req.MaxHeight > 0 && v.Height > req.MaxHeight {
				filters = append(filters, fmt.Sprintf("scale=-2:%d", req.MaxHeight))
			}
			if v.PixFmt != "yuv420p" {
				filters = append(filters, "format=yuv420p")
			}
			if vaapi {
				filters = append(filters, "format=nv12", "hwupload")
			}
			if req.BurnSub >= 0 || len(filters) > 0 {
				head := "[0:v:0]"
				if req.BurnSub >= 0 {
					head = fmt.Sprintf("[0:v:0][0:s:%d]overlay=eof_action=pass", req.BurnSub)
					if len(filters) > 0 {
						head += ","
					}
				}
				args = append(args, "-filter_complex", head+strings.Join(filters, ",")+"[vout]", "-map", "[vout]")
			} else {
				args = append(args, "-map", "0:v:0")
			}
			args = append(args, "-c:v", t.cfg.VideoEncoder)
			switch t.cfg.VideoEncoder {
			case "libx264":
				args = append(args, "-preset", t.cfg.Preset, "-profile:v", "high", "-level", "4.1",
					"-crf", "21")
			case "h264_nvenc":
				args = append(args, "-preset", "p4", "-profile:v", "high")
			}
			rate := req.MaxBitrate
			if rate == 0 {
				rate = defaultBitrate(req.MaxHeight, v.Height)
			}
			args = append(args, "-maxrate", strconv.Itoa(rate)+"k", "-bufsize", strconv.Itoa(rate*2)+"k",
				"-g", "48", "-keyint_min", "48", "-sc_threshold", "0")
		}
	}
	if a := j.Source.Audio(req.Audio); a != nil {
		args = append(args, "-map", fmt.Sprintf("%d:a:%d", audioInput, req.Audio))
		if d.AudioCopy {
			args = append(args, "-c:a", "copy")
		} else {
			ch := a.Channels
			if ch <= 0 || ch > 6 {
				ch = 2
			}
			if ch > 2 && ch != 6 {
				ch = 2
			}
			args = append(args, "-c:a", "aac", "-ac", strconv.Itoa(ch), "-b:a", strconv.Itoa(64*ch+64)+"k")
		}
	}
	args = append(args,
		"-sn", "-dn",
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-avoid_negative_ts", "make_zero",
		"-max_muxing_queue_size", "2048",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-frag_duration", "2000000",
		"-f", "mp4", "pipe:1")
	return args
}

func defaultBitrate(maxHeight, srcHeight int) int {
	h := srcHeight
	if maxHeight > 0 && maxHeight < h {
		h = maxHeight
	}
	switch {
	case h >= 2000:
		return 20000
	case h >= 1000:
		return 8000
	case h >= 700:
		return 4000
	case h >= 470:
		return 2000
	default:
		return 1000
	}
}

// Run streams a job to w until it finishes, the context ends, or the session
// is replaced.
func (t *Transcoder) Run(ctx context.Context, j Job, w io.Writer) error {
	bin := ffmpegBin()
	if bin == "" {
		return ErrNoFFmpeg
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if j.Session.Session != "" {
		t.Stop(j.Session.Session)
	}
	select {
	case t.slots <- struct{}{}:
	case <-time.After(3 * time.Second):
		return ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-t.slots }()

	if j.Session.Session != "" {
		j.Session.StartedAt = time.Now()
		j.Session.Method = j.Decision.Method
		r := &running{cancel: cancel, started: time.Now(), info: j.Session}
		t.mu.Lock()
		t.sessions[j.Session.Session] = r
		t.mu.Unlock()
		defer func() {
			t.mu.Lock()
			if t.sessions[j.Session.Session] == r {
				delete(t.sessions, j.Session.Session)
			}
			t.mu.Unlock()
		}()
	}

	args := t.Args(j)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 8 << 10}
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil // the viewer seeked, stopped, or closed the page
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		t.logger.Warn("ffmpeg stream failed", "error", err, "stderr", msg, "args", strings.Join(args, " "))
		return fmt.Errorf("ffmpeg: %v", err)
	}
	return nil
}

type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
		} else {
			l.buf.Write(p)
		}
	}
	return len(p), nil
}

// SubtitleToVTT converts a subtitle (an embedded track, typeIndex >= 0, or an
// external file, typeIndex < 0) to WebVTT, caching the result at cachePath.
func SubtitleToVTT(ctx context.Context, path string, typeIndex int, cachePath string) ([]byte, error) {
	if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
		return b, nil
	}
	bin := ffmpegBin()
	if bin == "" {
		return nil, ErrNoFFmpeg
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if typeIndex < 0 && strings.EqualFold(filepath.Ext(path), ".sub") {
		// MicroDVD .sub files carry frame numbers; assume film rate.
		args = append(args, "-sub_charenc", "UTF-8")
	}
	args = append(args, "-i", path)
	if typeIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:s:%d", typeIndex))
	}
	args = append(args, "-c:s", "webvtt", "-f", "webvtt", "pipe:1")
	cmd := exec.CommandContext(ctx, bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 4 << 10}
	if err := cmd.Run(); err != nil {
		// Files that are not UTF-8: let ffmpeg guess from a common legacy set.
		if typeIndex < 0 && strings.Contains(stderr.String(), "Invalid UTF-8") {
			return convertLegacy(ctx, bin, path, cachePath)
		}
		return nil, fmt.Errorf("convert subtitle: %s", strings.TrimSpace(stderr.String()))
	}
	b := out.Bytes()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		_ = os.WriteFile(cachePath, b, 0o644)
	}
	return b, nil
}

func convertLegacy(ctx context.Context, bin, path, cachePath string) ([]byte, error) {
	for _, enc := range []string{"CP1252", "CP1256", "CP1251", "ISO-8859-1"} {
		cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-sub_charenc", enc, "-i", path, "-c:s", "webvtt", "-f", "webvtt", "pipe:1")
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
				_ = os.WriteFile(cachePath, out, 0o644)
			}
			return out, nil
		}
	}
	return nil, errors.New("convert subtitle: unsupported text encoding")
}

// Frame writes a JPEG still of the video at position secs to dest, scaled to
// width pixels wide.
func Frame(ctx context.Context, path string, secs float64, width int, dest string) error {
	bin := ffmpegBin()
	if bin == "" {
		return ErrNoFFmpeg
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part.jpg"
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", strconv.FormatFloat(secs, 'f', 2, 64), "-i", path,
		"-map", "0:v:0", "-frames:v", "1",
		"-vf", fmt.Sprintf("thumbnail=40,scale=%d:-2", width),
		"-q:v", "3", "-y", tmp)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 2 << 10}
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract frame: %s", strings.TrimSpace(stderr.String()))
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		_ = os.Remove(tmp)
		return errors.New("extract frame: no output")
	}
	return os.Rename(tmp, dest)
}
