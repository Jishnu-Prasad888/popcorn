package ml

import (
	"image/jpeg"
	"os"
	"testing"
)

// TestSCRFDDetectRealModel runs the real detector on a real photo. It needs
// POPCORN_TEST_SCRFD (model path) and POPCORN_TEST_FACE_IMAGE (a jpeg with faces).
func TestSCRFDDetectRealModel(t *testing.T) {
	mp, ip := os.Getenv("POPCORN_TEST_SCRFD"), os.Getenv("POPCORN_TEST_FACE_IMAGE")
	if mp == "" || ip == "" {
		t.Skip("POPCORN_TEST_SCRFD / POPCORN_TEST_FACE_IMAGE not set")
	}
	det, err := NewSCRFDDetector(mp)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(ip)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	boxes, err := det.Detect(img)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range boxes {
		t.Logf("face %+v", b)
	}
	if len(boxes) == 0 {
		t.Fatal("no faces detected")
	}
}
