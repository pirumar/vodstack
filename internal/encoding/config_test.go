package encoding

import (
	"strings"
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	var c Config // zero value
	c.Normalize()
	if strings.Join(c.Resolutions, ",") != "360p,480p,720p,1080p" {
		t.Errorf("default resolutions = %v", c.Resolutions)
	}
	if strings.Join(c.Codecs, ",") != "h264" {
		t.Errorf("default codecs = %v", c.Codecs)
	}
}

func TestNormalizeForcesH264AndCanonicalOrder(t *testing.T) {
	c := Config{Codecs: []string{"vp9", "av1"}} // h264 missing, out of order
	c.Normalize()
	if strings.Join(c.Codecs, ",") != "h264,av1,vp9" {
		t.Errorf("codecs = %v, want canonical order with h264 forced", c.Codecs)
	}
}

func TestNormalizeDropsUnknownAndDedupes(t *testing.T) {
	c := Config{Resolutions: []string{"720p", "720p", "9000p", "240p"}}
	c.Normalize()
	if strings.Join(c.Resolutions, ",") != "240p,720p" {
		t.Errorf("resolutions = %v, want deduped/sorted/known-only", c.Resolutions)
	}
}

func TestExtraCodecs(t *testing.T) {
	c := Config{Codecs: []string{"h264", "hevc", "av1"}}
	c.Normalize()
	if strings.Join(c.ExtraCodecs(), ",") != "hevc,av1" {
		t.Errorf("extra codecs = %v", c.ExtraCodecs())
	}
}

func TestWatermarkNormalize(t *testing.T) {
	// Enabled but no image => disabled.
	c := Config{Watermark: Watermark{Enabled: true, Opacity: 5, Position: "weird", Margin: -3}}
	c.Normalize()
	if c.Watermark.Enabled {
		t.Error("watermark with no object should be disabled")
	}
	if c.Watermark.Position != "bottomRight" || c.Watermark.Opacity != 1 || c.Watermark.Margin != 24 {
		t.Errorf("watermark fields not clamped to defaults: %+v", c.Watermark)
	}

	// Enabled with an image keeps a valid position/opacity.
	c2 := Config{Watermark: Watermark{Enabled: true, Object: "library/x/watermark.png", Position: "topLeft", Opacity: 0.5, Margin: 10}}
	c2.Normalize()
	if !c2.Watermark.Enabled || c2.Watermark.Position != "topLeft" || c2.Watermark.Opacity != 0.5 {
		t.Errorf("valid watermark was altered: %+v", c2.Watermark)
	}
}
