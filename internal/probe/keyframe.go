package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"time"
)

// SeekPastKeyframe is how far past a keyframe a copying stream asks ffmpeg
// to seek so that it lands on that keyframe. ffmpeg's command line seeks
// about 0.13 s early on inputs that cannot seek by presentation time (MKV,
// for one) when the video has B-frames; aiming a little past the keyframe
// keeps that from dropping back to the keyframe before it.
const SeekPastKeyframe = 0.2

// Landing is where an input seek with stream copy really starts.
type Landing struct {
	// Keyframe is the presentation time of the video keyframe the stream
	// starts on.
	Keyframe float64 `json:"keyframe"`
	// Zero is the source time that becomes 0 in the stream: the earliest
	// packet after the seek, across all streams.
	Zero float64 `json:"zero"`
}

// SeekLanding reports where a seek toward at lands when video is copied.
// A copying stream can only start on a keyframe, so a player that asked for
// at shows the keyframe's picture at stream time 0 and must offset its clock
// by Zero, not by at.
func SeekLanding(ctx context.Context, path string, at float64) (Landing, error) {
	if at <= 0 {
		return Landing{}, nil
	}
	bin := ffprobePath()
	if bin == "" {
		return Landing{Keyframe: at, Zero: at}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "stream=index,codec_type:packet=stream_index,pts_time,dts_time,flags",
		"-of", "json",
		"-read_intervals", fmt.Sprintf("%.3f%%+#12", at),
		path,
	).Output()
	if err != nil {
		return Landing{Keyframe: at, Zero: at}, fmt.Errorf("probe seek: %w", err)
	}
	var doc struct {
		Streams []struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Packets []struct {
			StreamIndex int    `json:"stream_index"`
			PTS         string `json:"pts_time"`
			DTS         string `json:"dts_time"`
			Flags       string `json:"flags"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return Landing{Keyframe: at, Zero: at}, fmt.Errorf("probe seek: %w", err)
	}
	video := -1
	for _, s := range doc.Streams {
		if s.CodecType == "video" {
			video = s.Index
			break
		}
	}
	l := Landing{Keyframe: math.NaN(), Zero: math.Inf(1)}
	for _, p := range doc.Packets {
		pts, perr := strconv.ParseFloat(p.PTS, 64)
		dts, derr := strconv.ParseFloat(p.DTS, 64)
		if derr == nil && dts < l.Zero {
			l.Zero = dts
		} else if derr != nil && perr == nil && pts < l.Zero {
			l.Zero = pts
		}
		if p.StreamIndex == video && math.IsNaN(l.Keyframe) && perr == nil && len(p.Flags) > 0 && p.Flags[0] == 'K' {
			l.Keyframe = pts
		}
	}
	if math.IsNaN(l.Keyframe) || math.IsInf(l.Zero, 1) {
		return Landing{Keyframe: at, Zero: at}, nil
	}
	if l.Zero > l.Keyframe {
		l.Zero = l.Keyframe
	}
	if l.Zero < 0 {
		l.Zero = 0
	}
	return l, nil
}
