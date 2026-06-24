// Package player holds the library-level player customization model. The
// settings mirror Bunny Stream's player options (UI language, font, primary
// color, captions appearance, which controls show, playback speeds, custom CSS,
// heatmap/resume/compact toggles) and are rendered into the embeddable iframe.
package player

import (
	"regexp"
	"sort"
	"strings"
)

// Captions configures the on-screen subtitle cue appearance.
type Captions struct {
	Color      string `json:"color"`      // text color, hex
	Background string `json:"background"` // cue background, hex
	FontSize   int    `json:"fontSize"`   // px
}

// Config is a library's player customization. Zero value is not meaningful; use
// DefaultConfig and overlay stored fields via Normalize.
type Config struct {
	Language        string    `json:"language"`        // UI language: "tr","en",...
	FontFamily      string    `json:"fontFamily"`      // e.g. "Roboto"
	PrimaryColor    string    `json:"primaryColor"`    // controls/brand color, hex
	Captions        Captions  `json:"captions"`        // subtitle appearance
	Controls        []string  `json:"controls"`        // enabled control keys (see AllControls)
	PlaybackSpeeds  []float64 `json:"playbackSpeeds"`  // speed menu options
	DefaultSpeed    float64   `json:"defaultSpeed"`    // initial playback rate
	CustomCSS       string    `json:"customCSS"`       // injected into the player <style>
	ShowHeatmap     bool      `json:"showHeatmap"`     // watchtime heatmap above progress
	ResumePlayback  bool      `json:"resumePlayback"`  // remember/resume position
	CompactControls bool      `json:"compactControls"` // smaller control bar
}

// AllControls is the set of toggleable player controls (mirrors Bunny's list).
// Order is the canonical display order.
var AllControls = []string{
	"playPause", "seekBackward", "seekForward", "mute", "volume",
	"currentTime", "duration", "progress", "captions", "settings",
	"pip", "airplay", "chromecast", "fullscreen", "bigPlayButton",
}

// allowedLanguages restricts the UI language to the set we ship translations
// for. Defaults to English when an unknown value is stored.
var allowedLanguages = map[string]bool{
	"en": true, "tr": true, "de": true, "fr": true, "es": true,
}

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// DefaultConfig returns Bunny-like sensible defaults: English UI, amber brand,
// all controls on, the standard speed ladder, resume on.
func DefaultConfig() Config {
	return Config{
		Language:     "en",
		FontFamily:   "Roboto",
		PrimaryColor: "#fab027",
		Captions: Captions{
			Color:      "#ffffff",
			Background: "#000000",
			FontSize:   24,
		},
		Controls:        append([]string(nil), AllControls...),
		PlaybackSpeeds:  []float64{0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2},
		DefaultSpeed:    1,
		CustomCSS:       "",
		ShowHeatmap:     false,
		ResumePlayback:  true,
		CompactControls: false,
	}
}

// Normalize validates and clamps a config in place, falling back to defaults for
// any invalid field. It is applied both when loading stored config and before
// saving, so the embed template can trust every value.
func (c *Config) Normalize() {
	def := DefaultConfig()

	c.Language = strings.ToLower(strings.TrimSpace(c.Language))
	if !allowedLanguages[c.Language] {
		c.Language = def.Language
	}
	if strings.TrimSpace(c.FontFamily) == "" {
		c.FontFamily = def.FontFamily
	}
	if !hexColor.MatchString(c.PrimaryColor) {
		c.PrimaryColor = def.PrimaryColor
	}
	if !hexColor.MatchString(c.Captions.Color) {
		c.Captions.Color = def.Captions.Color
	}
	if !hexColor.MatchString(c.Captions.Background) {
		c.Captions.Background = def.Captions.Background
	}
	if c.Captions.FontSize < 8 || c.Captions.FontSize > 96 {
		c.Captions.FontSize = def.Captions.FontSize
	}

	c.Controls = sanitizeControls(c.Controls, def.Controls)
	c.PlaybackSpeeds = sanitizeSpeeds(c.PlaybackSpeeds, def.PlaybackSpeeds)

	if !speedAllowed(c.DefaultSpeed) {
		c.DefaultSpeed = def.DefaultSpeed
	}
	// Ensure the default speed is offered in the menu.
	if !containsSpeed(c.PlaybackSpeeds, c.DefaultSpeed) {
		c.PlaybackSpeeds = append(c.PlaybackSpeeds, c.DefaultSpeed)
		sort.Float64s(c.PlaybackSpeeds)
	}
	c.CustomCSS = sanitizeCSS(c.CustomCSS)
}

// sanitizeControls keeps only known control keys (in canonical order); an empty
// result falls back to the default set.
func sanitizeControls(in, def []string) []string {
	known := make(map[string]bool, len(AllControls))
	for _, k := range AllControls {
		known[k] = true
	}
	want := make(map[string]bool, len(in))
	for _, k := range in {
		if known[k] {
			want[k] = true
		}
	}
	if len(want) == 0 {
		return append([]string(nil), def...)
	}
	out := make([]string, 0, len(want))
	for _, k := range AllControls { // canonical order
		if want[k] {
			out = append(out, k)
		}
	}
	return out
}

func speedAllowed(s float64) bool { return s >= 0.1 && s <= 4 }

func containsSpeed(list []float64, s float64) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// sanitizeSpeeds clamps to (0.1..4], dedupes, sorts; empty -> defaults.
func sanitizeSpeeds(in, def []float64) []float64 {
	seen := make(map[float64]bool)
	out := make([]float64, 0, len(in))
	for _, s := range in {
		if speedAllowed(s) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return append([]float64(nil), def...)
	}
	sort.Float64s(out)
	return out
}

// sanitizeCSS strips characters that would let custom CSS break out of the
// <style> context into markup/script. We only ever render this inside a CSS
// block, so removing "<" (and by extension "</style>") is sufficient.
func sanitizeCSS(css string) string {
	return strings.ReplaceAll(css, "<", "")
}
