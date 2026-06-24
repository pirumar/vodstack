package video

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// EditSpec is the browser-produced edit decision list applied at transcode time.
// A nil *EditSpec, or an identity spec (one full-range segment, full-frame crop,
// no rotate/flip), means "no edit" and leaves the pipeline unchanged.
type EditSpec struct {
	Version  int       `json:"version"`
	Segments []Segment `json:"segments"`
	Crop     Crop      `json:"crop"`
	Rotate   int       `json:"rotate"` // 0|90|180|270
	Flip     string    `json:"flip"`   // none|h|v
}

// Segment is a kept time range in source seconds (start inclusive, end exclusive).
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Crop is a normalized rectangle in 0..1 of the source frame.
type Crop struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// SegmentCount returns how many kept ranges the spec has.
func (e *EditSpec) SegmentCount() int {
	if e == nil {
		return 0
	}
	return len(e.Segments)
}

// IsTemporal reports whether time-based editing (multi-segment cut) is required.
// A single segment is handled by input seeking, not by the concat pre-pass.
func (e *EditSpec) IsTemporal() bool { return e.SegmentCount() > 1 }

// hasCrop reports a non-identity crop rectangle.
func (e *EditSpec) hasCrop() bool {
	c := e.Crop
	return !(c.X == 0 && c.Y == 0 && c.W == 1 && c.H == 1)
}

// IsIdentity reports whether the spec changes nothing about the source.
// Identity means no segments (empty == keep the whole video) plus a full-frame
// crop and no rotate/flip. Any segment present is an explicit keep-range, i.e. a
// real temporal edit (a single segment is a trim), so it is never identity.
func (e *EditSpec) IsIdentity() bool {
	if e == nil {
		return true
	}
	if e.hasCrop() || e.Rotate != 0 || (e.Flip != "" && e.Flip != "none") {
		return false
	}
	return len(e.Segments) == 0
}

// Validate checks structural invariants. nil and identity specs are valid.
func (e *EditSpec) Validate() error {
	if e == nil {
		return nil
	}
	switch e.Rotate {
	case 0, 90, 180, 270:
	default:
		return fmt.Errorf("rotate must be one of 0,90,180,270 (got %d)", e.Rotate)
	}
	switch e.Flip {
	case "", "none", "h", "v":
	default:
		return fmt.Errorf("flip must be none|h|v (got %q)", e.Flip)
	}
	c := e.Crop
	if math.IsNaN(c.X) || math.IsNaN(c.Y) || math.IsNaN(c.W) || math.IsNaN(c.H) ||
		math.IsInf(c.X, 0) || math.IsInf(c.Y, 0) || math.IsInf(c.W, 0) || math.IsInf(c.H, 0) {
		return fmt.Errorf("crop values must be finite")
	}
	if c.W <= 0 || c.H <= 0 || c.X < 0 || c.Y < 0 || c.X+c.W > 1.0001 || c.Y+c.H > 1.0001 {
		return fmt.Errorf("crop rectangle out of 0..1 bounds")
	}
	segs := append([]Segment(nil), e.Segments...)
	sort.Slice(segs, func(i, j int) bool { return segs[i].Start < segs[j].Start })
	var prevEnd float64 = -1
	for _, s := range segs {
		if s.Start < 0 || s.End <= s.Start {
			return fmt.Errorf("segment must have 0<=start<end (got %v)", s)
		}
		if s.Start < prevEnd {
			return fmt.Errorf("segments must not overlap")
		}
		prevEnd = s.End
	}
	return nil
}

// Normalize sorts segments and defaults empty fields so downstream code can rely
// on a canonical shape. Returns the same pointer for convenience.
func (e *EditSpec) Normalize() *EditSpec {
	if e == nil {
		return nil
	}
	if e.Flip == "" {
		e.Flip = "none"
	}
	sort.Slice(e.Segments, func(i, j int) bool { return e.Segments[i].Start < e.Segments[j].Start })
	return e
}

// ParseEditSpec decodes raw jsonb into an *EditSpec; empty/null => nil.
func ParseEditSpec(raw []byte) (*EditSpec, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil, nil
	}
	var e EditSpec
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return e.Normalize(), nil
}
