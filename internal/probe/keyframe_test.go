package probe

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

// A copying stream asked to start between keyframes starts on the keyframe
// before; SeekLanding must report that keyframe, not the requested time.
func TestSeekLanding(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil || !Available() {
		t.Skip("ffmpeg and ffprobe are needed")
	}
	for _, ext := range []string{"mkv", "mp4"} {
		path := filepath.Join(t.TempDir(), "clip."+ext)
		// A keyframe every 2 s (48 frames at 24 fps), with B-frames.
		out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=12:size=320x180:rate=24",
			"-c:v", "libx264", "-g", "48", "-bf", "2", "-pix_fmt", "yuv420p", path).CombinedOutput()
		if err != nil {
			t.Skipf("could not encode a test clip: %v %s", err, out)
		}
		for _, tc := range []struct{ at, keyframe float64 }{{5, 4}, {7.9, 6}, {4, 4}, {0, 0}} {
			l, err := SeekLanding(context.Background(), path, tc.at)
			if err != nil {
				t.Fatalf("%s at %v: %v", ext, tc.at, err)
			}
			if math.Abs(l.Keyframe-tc.keyframe) > 0.1 {
				t.Errorf("%s at %v: keyframe %v, want %v", ext, tc.at, l.Keyframe, tc.keyframe)
			}
			if l.Zero > l.Keyframe || l.Keyframe-l.Zero > 0.25 {
				t.Errorf("%s at %v: zero %v not just before keyframe %v", ext, tc.at, l.Zero, l.Keyframe)
			}
		}
	}
}
