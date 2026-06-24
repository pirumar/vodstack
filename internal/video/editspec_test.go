package video

import (
	"math"
	"testing"
)

func TestEditSpec_IdentityAndTemporal(t *testing.T) {
	var nilSpec *EditSpec
	if !nilSpec.IsIdentity() {
		t.Fatal("nil spec must be identity")
	}
	empty := &EditSpec{Segments: nil, Crop: Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}
	if !empty.IsIdentity() {
		t.Fatal("empty segments + full crop + no rotate/flip must be identity")
	}
	trimmed := &EditSpec{Segments: []Segment{{Start: 5, End: 100}}, Crop: Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}
	if trimmed.IsIdentity() {
		t.Fatal("a single trimmed segment must NOT be identity")
	}
	if trimmed.IsTemporal() {
		t.Fatal("single segment is not temporal")
	}
	multi := &EditSpec{Segments: []Segment{{0, 10}, {20, 30}}, Crop: fullCrop(), Flip: "none"}
	if !multi.IsTemporal() || multi.SegmentCount() != 2 {
		t.Fatal("multi-segment must be temporal with count 2")
	}
}

func TestEditSpec_Validate(t *testing.T) {
	bad := []*EditSpec{
		{Segments: []Segment{{End: 10, Start: 10}}, Crop: fullCrop(), Flip: "none"},
		{Segments: []Segment{{0, 10}, {5, 15}}, Crop: fullCrop(), Flip: "none"},
		{Segments: []Segment{{0, 10}}, Crop: Crop{X: 0.6, Y: 0, W: 0.5, H: 1}, Flip: "none"},
		{Segments: []Segment{{0, 10}}, Crop: fullCrop(), Rotate: 45, Flip: "none"},
		{Segments: []Segment{{0, 10}}, Crop: fullCrop(), Flip: "diagonal"},
		{Segments: []Segment{{0, 10}}, Crop: Crop{X: 0, Y: 0, W: math.NaN(), H: 1}, Flip: "none"},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
	good := &EditSpec{Segments: []Segment{{0, 10}, {20, 30}}, Crop: Crop{0, 0, 0.5, 0.5}, Rotate: 90, Flip: "h"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good spec failed: %v", err)
	}
}

func fullCrop() Crop { return Crop{X: 0, Y: 0, W: 1, H: 1} }
