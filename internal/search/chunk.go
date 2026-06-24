package search

import (
	"strconv"
	"strings"
)

// Chunk is a contiguous span of transcript with a seek target. start_sec is what
// the player jumps to when this chunk matches a query.
type Chunk struct {
	Index    int
	StartSec float64
	EndSec   float64
	Text     string
}

// Cue is one parsed WebVTT cue (exported for reuse, e.g. LLM chapter generation).
type Cue struct {
	Start float64
	End   float64
	Text  string
}

// ParseAndChunk parses a WebVTT transcript and groups consecutive cues into
// windows of about chunkSeconds. Each chunk carries the start of its first cue
// (the seek target) and the joined text of its cues. Cues with no text are
// skipped. A non-positive chunkSeconds falls back to the default.
func ParseAndChunk(vtt []byte, chunkSeconds int) []Chunk {
	if chunkSeconds <= 0 {
		chunkSeconds = defaultChunkSeconds
	}
	cues := ParseCues(vtt)
	if len(cues) == 0 {
		return nil
	}

	var (
		out      []Chunk
		window   []string
		winStart = cues[0].Start
		winEnd   = cues[0].Start
		idx      int
	)
	flush := func() {
		if len(window) == 0 {
			return
		}
		out = append(out, Chunk{
			Index:    idx,
			StartSec: winStart,
			EndSec:   winEnd,
			Text:     strings.Join(window, " "),
		})
		idx++
		window = nil
	}

	for _, c := range cues {
		if len(window) == 0 {
			winStart = c.Start
		}
		window = append(window, c.Text)
		winEnd = c.End
		if winEnd-winStart >= float64(chunkSeconds) {
			flush()
		}
	}
	flush()
	return out
}

// ParseCues extracts cues from WebVTT bytes. It tolerates the optional WEBVTT
// header, blank lines, NOTE blocks, and numeric/string cue identifiers preceding
// a timing line.
func ParseCues(vtt []byte) []Cue {
	s := strings.ReplaceAll(string(vtt), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")

	var out []Cue
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "-->") {
			continue
		}
		start, end, ok := parseTiming(line)
		if !ok {
			continue
		}
		// Collect the cue payload: following non-empty lines until a blank line.
		var text []string
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" {
				break
			}
			text = append(text, next)
			i++
		}
		joined := strings.TrimSpace(strings.Join(text, " "))
		if joined == "" {
			continue
		}
		out = append(out, Cue{Start: start, End: end, Text: joined})
	}
	return out
}

// parseTiming parses a "start --> end [settings]" line into seconds.
func parseTiming(line string) (start, end float64, ok bool) {
	parts := strings.SplitN(line, "-->", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok1 := parseClock(strings.TrimSpace(parts[0]))
	// The end side may carry cue settings (e.g. "align:start"); take the first token.
	rhs := strings.Fields(strings.TrimSpace(parts[1]))
	if len(rhs) == 0 {
		return 0, 0, false
	}
	end, ok2 := parseClock(rhs[0])
	return start, end, ok1 && ok2
}

// parseClock parses "HH:MM:SS.mmm" or "MM:SS.mmm" into seconds.
func parseClock(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	segs := strings.Split(s, ":")
	if len(segs) < 2 || len(segs) > 3 {
		return 0, false
	}
	var hours, mins float64
	secField := segs[len(segs)-1]
	minField := segs[len(segs)-2]
	if len(segs) == 3 {
		h, err := strconv.ParseFloat(segs[0], 64)
		if err != nil {
			return 0, false
		}
		hours = h
	}
	m, err := strconv.ParseFloat(minField, 64)
	if err != nil {
		return 0, false
	}
	mins = m
	sec, err := strconv.ParseFloat(secField, 64)
	if err != nil {
		return 0, false
	}
	return hours*3600 + mins*60 + sec, true
}
