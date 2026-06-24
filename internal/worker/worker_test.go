package worker

import (
	"testing"

	"github.com/pirumar/vodstack/internal/video"
)

func TestEditedDimensions(t *testing.T) {
	// no edit
	if w, h := editedDimensions(1920, 1080, nil); w != 1920 || h != 1080 {
		t.Fatalf("nil: got %dx%d", w, h)
	}
	// crop to half width/height
	if w, h := editedDimensions(1920, 1080, &video.EditSpec{Crop: video.Crop{X: 0, Y: 0, W: 0.5, H: 0.5}, Flip: "none"}); w != 960 || h != 540 {
		t.Fatalf("crop: got %dx%d want 960x540", w, h)
	}
	// rotate 90 swaps dims
	if w, h := editedDimensions(1920, 1080, &video.EditSpec{Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Rotate: 90, Flip: "none"}); w != 1080 || h != 1920 {
		t.Fatalf("rotate90: got %dx%d want 1080x1920", w, h)
	}
}
