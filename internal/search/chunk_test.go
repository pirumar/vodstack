package search

import "testing"

func TestParseClock(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"00:00:05.000", 5, true},
		{"00:01:30.500", 90.5, true},
		{"01:00:00.000", 3600, true},
		{"02:05.000", 125, true}, // MM:SS form
		{"garbage", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := parseClock(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseClock(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseAndChunkGroupsByWindow(t *testing.T) {
	vtt := []byte(`WEBVTT

00:00:00.000 --> 00:00:10.000
Birinci cümle.

00:00:10.000 --> 00:00:20.000
İkinci cümle.

00:00:20.000 --> 00:00:35.000
Üçüncü cümle uzun.

00:00:35.000 --> 00:00:45.000
Dördüncü cümle.
`)
	// 30s windows: cues 1-3 (0..35 closes when >=30 at end 35) form chunk 0,
	// cue 4 forms chunk 1.
	chunks := ParseAndChunk(vtt, 30)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].StartSec != 0 {
		t.Errorf("chunk0 start = %v want 0", chunks[0].StartSec)
	}
	if chunks[0].Text != "Birinci cümle. İkinci cümle. Üçüncü cümle uzun." {
		t.Errorf("chunk0 text = %q", chunks[0].Text)
	}
	if chunks[1].StartSec != 35 {
		t.Errorf("chunk1 start = %v want 35", chunks[1].StartSec)
	}
	if chunks[0].Index != 0 || chunks[1].Index != 1 {
		t.Errorf("indexes = %d,%d", chunks[0].Index, chunks[1].Index)
	}
}

func TestParseAndChunkSkipsEmptyAndHandlesCRLF(t *testing.T) {
	vtt := []byte("WEBVTT\r\n\r\n00:00:01.000 --> 00:00:02.000\r\nHello\r\n\r\n00:00:02.000 --> 00:00:03.000\r\n\r\n")
	chunks := ParseAndChunk(vtt, 30)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Text != "Hello" {
		t.Errorf("text = %q want Hello", chunks[0].Text)
	}
}

func TestParseAndChunkEmpty(t *testing.T) {
	if got := ParseAndChunk([]byte("WEBVTT\n\n"), 30); got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestConfigNormalize(t *testing.T) {
	c := Config{Provider: "GEMINI", Model: "", ChunkSeconds: 0}
	c.Normalize()
	if c.Provider != ProviderGemini {
		t.Errorf("provider = %q", c.Provider)
	}
	if c.Model != defaultGeminiModel {
		t.Errorf("model = %q want %q", c.Model, defaultGeminiModel)
	}
	if c.ChunkSeconds != defaultChunkSeconds {
		t.Errorf("chunkSeconds = %d", c.ChunkSeconds)
	}

	bad := Config{Provider: "nonsense", ChunkSeconds: 9999}
	bad.Normalize()
	if bad.Provider != ProviderLocal {
		t.Errorf("invalid provider should fall back to local, got %q", bad.Provider)
	}
	if bad.ChunkSeconds != maxChunkSeconds {
		t.Errorf("chunkSeconds clamp = %d want %d", bad.ChunkSeconds, maxChunkSeconds)
	}
}
