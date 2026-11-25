// Package stream serves video to the player: it decides between direct play,
// remuxing and transcoding, runs ffmpeg for the latter two, converts
// subtitles to WebVTT, and grabs still frames for thumbnails.
//
// Original files are only ever read.
package stream

import (
	"strings"
)

// Methods of playback, cheapest first.
const (
	MethodDirect    = "direct"    // the browser reads the original file
	MethodRemux     = "remux"     // streams copied into fragmented MP4
	MethodTranscode = "transcode" // video re-encoded (and audio if needed)
)

// Track is the part of a probed stream the decision needs.
type Track struct {
	Kind      string // video, audio, subtitle
	TypeIndex int
	Codec     string
	Profile   string
	Channels  int
	Height    int
	PixFmt    string
	IsDefault bool
}

// Source describes the file being played.
type Source struct {
	Container string
	Bitrate   int64
	Tracks    []Track
}

// Video returns the first video track.
func (s Source) Video() *Track {
	for i := range s.Tracks {
		if s.Tracks[i].Kind == "video" && !isPicture(s.Tracks[i].Codec) {
			return &s.Tracks[i]
		}
	}
	return nil
}

// Audio returns the audio track with the given type index, or nil.
func (s Source) Audio(typeIndex int) *Track {
	for i := range s.Tracks {
		if s.Tracks[i].Kind == "audio" && s.Tracks[i].TypeIndex == typeIndex {
			return &s.Tracks[i]
		}
	}
	return nil
}

// DefaultAudio returns the type index of the track a browser would play when
// given the file: the one flagged default, else the first.
func (s Source) DefaultAudio() int {
	first := -1
	for _, t := range s.Tracks {
		if t.Kind != "audio" {
			continue
		}
		if first < 0 {
			first = t.TypeIndex
		}
		if t.IsDefault {
			return t.TypeIndex
		}
	}
	if first < 0 {
		return 0
	}
	return first
}

// Subtitle returns the subtitle track with the given type index, or nil.
func (s Source) Subtitle(typeIndex int) *Track {
	for i := range s.Tracks {
		if s.Tracks[i].Kind == "subtitle" && s.Tracks[i].TypeIndex == typeIndex {
			return &s.Tracks[i]
		}
	}
	return nil
}

func isPicture(c string) bool {
	switch c {
	case "mjpeg", "png", "bmp", "gif", "webp":
		return true
	}
	return false
}

// Client is what the browser says it can play, from MediaSource /
// canPlayType checks.
type Client struct {
	Containers  []string // mp4, webm, mkv
	VideoCodecs []string // h264, hevc, vp9, av1
	AudioCodecs []string // aac, mp3, opus, vorbis, flac, ac3, eac3
	// Passthrough lists audio codecs the device can pass to a receiver
	// untouched (ac3, eac3, dts, truehd); they count as supported.
	Passthrough []string
}

// DefaultClient is a conservative modern browser.
var DefaultClient = Client{
	Containers:  []string{"mp4", "webm"},
	VideoCodecs: []string{"h264", "vp8", "vp9"},
	AudioCodecs: []string{"aac", "mp3", "opus", "vorbis", "flac"},
}

func has(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// Request is what the viewer asked for.
type Request struct {
	Audio      int  // audio type index
	BurnSub    int  // subtitle type index to burn in, -1 for none
	MaxHeight  int  // 0 = original
	MaxBitrate int  // kbit/s, 0 = automatic
	Force      string // "", remux, transcode
	AudioDelay int  // milliseconds
}

// Decision is how a file will be played and why.
type Decision struct {
	Method    string   `json:"method"`
	VideoCopy bool     `json:"video_copy"`
	AudioCopy bool     `json:"audio_copy"`
	Reasons   []string `json:"reasons"`
}

// browserVideo reports whether the client decodes a video track. 10-bit
// H.264 (Hi10P) is the classic anime case browsers cannot decode.
func browserVideo(c Client, v *Track) bool {
	if v == nil {
		return true
	}
	codec := v.Codec
	if codec == "h264" && (strings.Contains(v.PixFmt, "10") || strings.EqualFold(v.Profile, "High 10")) {
		return false
	}
	return has(c.VideoCodecs, codec)
}

func browserAudio(c Client, a *Track) bool {
	if a == nil {
		return true
	}
	if has(c.Passthrough, a.Codec) {
		return true
	}
	if !has(c.AudioCodecs, a.Codec) {
		return false
	}
	// Browsers downmix multichannel AAC/Opus fine; FLAC beyond stereo is
	// patchy, so it is converted.
	if a.Codec == "flac" && a.Channels > 2 {
		return false
	}
	return true
}

// containerOK reports whether the browser can open the container directly.
func containerOK(c Client, container string) bool {
	switch container {
	case "mp4", "m4v", "mov":
		return has(c.Containers, "mp4")
	case "webm":
		return has(c.Containers, "webm")
	case "mkv":
		return has(c.Containers, "mkv")
	}
	return false
}

// mp4Video reports whether a video codec can be copied into fragmented MP4.
func mp4Video(codec string) bool {
	switch codec {
	case "h264", "hevc", "av1", "vp9", "mpeg4":
		return true
	}
	return false
}

// Decide picks the cheapest method that plays the request on the client.
func Decide(src Source, req Request, c Client) Decision {
	d := Decision{Reasons: []string{}}
	v := src.Video()
	a := src.Audio(req.Audio)

	videoOK := browserVideo(c, v)
	audioOK := browserAudio(c, a)
	scale := v != nil && req.MaxHeight > 0 && v.Height > req.MaxHeight
	bitrateCap := req.MaxBitrate > 0 && src.Bitrate > int64(req.MaxBitrate)*1000
	burn := req.BurnSub >= 0

	if !videoOK {
		d.Reasons = append(d.Reasons, "video codec not supported by this browser")
	}
	if !audioOK {
		d.Reasons = append(d.Reasons, "audio codec not supported by this browser")
	}
	if scale || bitrateCap {
		d.Reasons = append(d.Reasons, "quality limit below the original")
	}
	if burn {
		d.Reasons = append(d.Reasons, "image subtitles are burned into the picture")
	}

	needVideoEncode := !videoOK || scale || bitrateCap || burn || req.Force == MethodTranscode ||
		(v != nil && !mp4Video(v.Codec))
	if needVideoEncode {
		d.Method = MethodTranscode
		d.VideoCopy = false
		d.AudioCopy = audioOK && a != nil && mp4Audio(a.Codec) && req.AudioDelay == 0
		return d
	}

	direct := req.Force == "" && containerOK(c, src.Container) && audioOK &&
		req.Audio == src.DefaultAudio() && req.AudioDelay == 0
	if !containerOK(c, src.Container) {
		d.Reasons = append(d.Reasons, "container "+src.Container+" is not playable in the browser")
	}
	if req.Audio != src.DefaultAudio() {
		d.Reasons = append(d.Reasons, "a non-default audio track was chosen")
	}
	if req.AudioDelay != 0 {
		d.Reasons = append(d.Reasons, "audio delay is applied on the server")
	}
	if direct {
		d.Method = MethodDirect
		d.VideoCopy, d.AudioCopy = true, true
		d.Reasons = []string{}
		return d
	}
	d.Method = MethodRemux
	d.VideoCopy = true
	d.AudioCopy = audioOK && a != nil && mp4Audio(a.Codec)
	return d
}

// mp4Audio reports whether an audio codec can be copied into MP4.
func mp4Audio(codec string) bool {
	switch codec {
	case "aac", "mp3", "opus", "flac", "ac3", "eac3", "alac":
		return true
	}
	return false
}

// ImageSubtitle reports whether a subtitle codec is a bitmap format that has
// to be burned into the video rather than shown as text.
func ImageSubtitle(codec string) bool {
	switch codec {
	case "hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvdsub", "dvb_subtitle", "xsub", "vobsub":
		return true
	}
	return false
}
