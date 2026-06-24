// Package encoding holds the library-level encoding settings model. The settings
// mirror Bunny Stream's "Encoding Tier" controls — enabled resolutions, output
// codecs, MP4 fallback, original download, Early-Play, multi-audio tracks, and a
// burned-in watermark — and drive what the transcode pipeline produces.
//
// A library stores defaults (encoding_config); each video snapshots them at
// creation (videos.encode_settings) so backfills and re-encodes use the exact
// config the upload was made with. Zero value is not meaningful: load through
// DefaultConfig + Normalize, which fills every field.
package encoding

import "strings"

// Resolution ladder names, low -> high. The transcode ladder maps each to a
// width/height/bitrate (see internal/transcode). Canonical display order.
var AllResolutions = []string{"240p", "360p", "480p", "720p", "1080p", "1440p", "2160p"}

// Output codecs. h264 is always produced (universal compatibility); the others
// are CPU-expensive opt-in backfills that run on the bulk lane and get merged
// into the master playlist so a player picks the best codec it can decode.
var AllCodecs = []string{"h264", "hevc", "av1", "vp9"}

// Watermark anchor positions (corner/center of the frame).
var watermarkPositions = map[string]bool{
	"topLeft": true, "topRight": true, "bottomLeft": true,
	"bottomRight": true, "center": true,
}

// Watermark is a burned-in image overlay applied during encoding (cannot be
// removed afterwards). Object is the MinIO key of the uploaded watermark image.
type Watermark struct {
	Enabled  bool    `json:"enabled"`
	Object   string  `json:"object"`   // MinIO key, e.g. "library/<id>/watermark.png" ("" = none)
	Position string  `json:"position"` // one of watermarkPositions
	Opacity  float64 `json:"opacity"`  // 0..1
	Margin   int     `json:"margin"`   // px inset from the frame edge
}

// Config is a library's encoding settings. Use DefaultConfig + Normalize.
type Config struct {
	Resolutions   []string  `json:"resolutions"`   // subset of AllResolutions
	Codecs        []string  `json:"codecs"`        // subset of AllCodecs; h264 always forced on
	MP4Fallback   bool      `json:"mp4Fallback"`   // also produce a progressive MP4 (<=1080p)
	AllowDownload bool      `json:"allowDownload"` // expose the original/MP4 via a download URL
	EarlyPlay     bool      `json:"earlyPlay"`     // play the original while encoding (exposes the source)
	MultiAudio    bool      `json:"multiAudio"`    // encode every source audio track as a selectable language
	Watermark     Watermark `json:"watermark"`
}

// DefaultConfig returns the historical behavior: the original 4-rung H.264 ladder
// with every advanced feature off, so an un-configured library is unchanged.
func DefaultConfig() Config {
	return Config{
		Resolutions:   []string{"360p", "480p", "720p", "1080p"},
		Codecs:        []string{"h264"},
		MP4Fallback:   false,
		AllowDownload: false,
		EarlyPlay:     false,
		MultiAudio:    false,
		Watermark:     Watermark{Position: "bottomRight", Opacity: 1, Margin: 24},
	}
}

// Normalize validates and clamps a config in place, falling back to defaults for
// any invalid field. Applied both when loading stored config and before saving.
func (c *Config) Normalize() {
	def := DefaultConfig()
	c.Resolutions = sanitizeSet(c.Resolutions, AllResolutions, def.Resolutions)
	c.Codecs = sanitizeSet(c.Codecs, AllCodecs, def.Codecs)
	// h264 is mandatory: it is the only universally-decodable ladder and the base
	// every other codec's master is merged onto.
	if !contains(c.Codecs, "h264") {
		c.Codecs = sanitizeSet(append([]string{"h264"}, c.Codecs...), AllCodecs, def.Codecs)
	}

	if !watermarkPositions[c.Watermark.Position] {
		c.Watermark.Position = def.Watermark.Position
	}
	if c.Watermark.Opacity <= 0 || c.Watermark.Opacity > 1 {
		c.Watermark.Opacity = def.Watermark.Opacity
	}
	if c.Watermark.Margin < 0 || c.Watermark.Margin > 512 {
		c.Watermark.Margin = def.Watermark.Margin
	}
	// A watermark with no image can never render; treat it as disabled.
	if strings.TrimSpace(c.Watermark.Object) == "" {
		c.Watermark.Enabled = false
	}
}

// ResolutionEnabled reports whether a ladder rung name is in the enabled set.
func (c *Config) ResolutionEnabled(name string) bool { return contains(c.Resolutions, name) }

// HasCodec reports whether a codec is enabled.
func (c *Config) HasCodec(name string) bool { return contains(c.Codecs, name) }

// ExtraCodecs returns the enabled codecs beyond the always-on H.264, in canonical
// order. These are the bulk-lane backfills to enqueue after the main ladder.
func (c *Config) ExtraCodecs() []string {
	out := make([]string, 0, len(c.Codecs))
	for _, k := range AllCodecs {
		if k != "h264" && contains(c.Codecs, k) {
			out = append(out, k)
		}
	}
	return out
}

// sanitizeSet keeps only members of allowed (in allowed's canonical order),
// dedupes, and falls back to def when nothing valid remains.
func sanitizeSet(in, allowed, def []string) []string {
	ok := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		ok[k] = true
	}
	want := make(map[string]bool, len(in))
	for _, k := range in {
		k = strings.TrimSpace(k)
		if ok[k] {
			want[k] = true
		}
	}
	if len(want) == 0 {
		return append([]string(nil), def...)
	}
	out := make([]string, 0, len(want))
	for _, k := range allowed { // canonical order
		if want[k] {
			out = append(out, k)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
