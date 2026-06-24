package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pirumar/vodstack/internal/video"
)

// These tests exercise the REAL ffmpeg pipeline (not just arg shape). They are
// skipped automatically when ffmpeg/ffprobe are not on PATH, so the normal unit
// run is unaffected; CI/Docker with ffmpeg runs them.

func ffmpegOrSkip(t *testing.T) *Transcoder {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping integration test")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping integration test")
	}
	return New("ffmpeg", "ffprobe")
}

// makeSource generates a 2-second 1280x720 clip with TWO audio tracks (eng, tur).
func makeSource(t *testing.T, tc *Transcoder, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "src.mp4")
	cmd := exec.Command(tc.FFmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=2",
		"-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-metadata:s:a:0", "language=eng",
		"-metadata:s:a:1", "language=tur",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		src,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make source: %v\n%s", err, out)
	}
	return src
}

func makeWatermark(t *testing.T, tc *Transcoder, dir string) string {
	t.Helper()
	wm := filepath.Join(dir, "wm.png")
	cmd := exec.Command(tc.FFmpegBin,
		"-y", "-f", "lavfi", "-i", "color=c=red:size=120x60", "-frames:v", "1", wm,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make watermark: %v\n%s", err, out)
	}
	return wm
}

// makeSourceDur generates a single-audio clip of the given duration (seconds),
// reusing this file's testsrc2 + sine + libx264/yuv420p/aac/-shortest pattern.
func makeSourceDur(t *testing.T, tc *Transcoder, dir string, dur int) string {
	t.Helper()
	src := filepath.Join(dir, "src.mp4")
	cmd := exec.Command(tc.FFmpegBin,
		"-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=1280x720:rate=30:duration=%d", dur),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", dur),
		"-map", "0:v", "-map", "1:a",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		src,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make source: %v\n%s", err, out)
	}
	return src
}

func TestBuildEditedSource_RealFFmpeg(t *testing.T) {
	tc := ffmpegOrSkip(t)
	dir := t.TempDir()
	src := makeSourceDur(t, tc, dir, 6)
	out := filepath.Join(dir, "edited.mp4")
	spec := &video.EditSpec{Segments: []video.Segment{{Start: 0, End: 2}, {Start: 4, End: 6}}, Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}
	if _, err := tc.BuildEditedSource(context.Background(), src, out, spec); err != nil {
		t.Fatalf("BuildEditedSource: %v", err)
	}
	p, err := tc.Probe(context.Background(), out)
	if err != nil {
		t.Fatalf("probe edited: %v", err)
	}
	if p.DurationSeconds < 3 || p.DurationSeconds > 5 {
		t.Fatalf("edited duration %ds, expected ~4s", p.DurationSeconds)
	}
}

func TestIntegrationProbeMultiAudio(t *testing.T) {
	tc := ffmpegOrSkip(t)
	dir := t.TempDir()
	src := makeSource(t, tc, dir)
	p, err := tc.Probe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AudioTracks) != 2 {
		t.Fatalf("expected 2 audio tracks, got %d (%+v)", len(p.AudioTracks), p.AudioTracks)
	}
	if p.AudioTracks[0].Language != "eng" || p.AudioTracks[1].Language != "tur" {
		t.Errorf("audio track languages = %+v", p.AudioTracks)
	}
}

func TestIntegrationBuildHLSMultiAudioWatermark(t *testing.T) {
	tc := ffmpegOrSkip(t)
	dir := t.TempDir()
	src := makeSource(t, tc, dir)
	wm := makeWatermark(t, tc, dir)
	p, err := tc.Probe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "hls")
	opts := &Options{
		Resolutions: []string{"360p"}, // single rung keeps the encode fast
		MultiAudio:  true,
		Watermark:   &WatermarkSpec{Path: wm, Position: "bottomRight", Opacity: 0.6, Margin: 12},
	}
	if _, err := tc.BuildHLS(context.Background(), src, out, p, opts, nil, ""); err != nil {
		t.Fatalf("BuildHLS: %v", err)
	}
	master, err := os.ReadFile(filepath.Join(out, "master.m3u8"))
	if err != nil {
		t.Fatalf("read master: %v", err)
	}
	ms := string(master)
	// Multi-audio => two #EXT-X-MEDIA TYPE=AUDIO renditions with the languages.
	if strings.Count(ms, "#EXT-X-MEDIA:TYPE=AUDIO") != 2 {
		t.Errorf("expected 2 audio media entries in master:\n%s", ms)
	}
	if !strings.Contains(ms, `LANGUAGE="eng"`) || !strings.Contains(ms, `LANGUAGE="tur"`) {
		t.Errorf("master missing audio languages:\n%s", ms)
	}
	// Every audio rendition URI in the master must resolve to a real playlist.
	uris := 0
	for _, line := range strings.Split(ms, "\n") {
		if !strings.HasPrefix(line, "#EXT-X-MEDIA:TYPE=AUDIO") {
			continue
		}
		uris++
		i := strings.Index(line, `URI="`)
		if i < 0 {
			t.Fatalf("audio media line missing URI: %s", line)
		}
		rest := line[i+5:]
		uri := rest[:strings.IndexByte(rest, '"')]
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(uri))); err != nil {
			t.Errorf("audio rendition URI %q does not resolve: %v", uri, err)
		}
	}
	if uris != 2 {
		t.Errorf("expected 2 audio rendition URIs, got %d", uris)
	}
}

func TestIntegrationBuildCodecs(t *testing.T) {
	tc := ffmpegOrSkip(t)
	dir := t.TempDir()
	src := makeSource(t, tc, dir)
	p, err := tc.Probe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	opts := &Options{Resolutions: []string{"360p"}}
	for _, codec := range []string{"hevc", "vp9", "av1"} {
		codec := codec
		t.Run(codec, func(t *testing.T) {
			out := filepath.Join(dir, codec)
			if _, err := tc.BuildCodec(context.Background(), codec, src, out, p, opts, nil); err != nil {
				t.Fatalf("BuildCodec(%s): %v", codec, err)
			}
			if _, err := os.Stat(filepath.Join(out, "master_"+codec+".m3u8")); err != nil {
				t.Fatalf("missing %s master: %v", codec, err)
			}
		})
	}
}

func TestIntegrationBuildMP4Fallback(t *testing.T) {
	tc := ffmpegOrSkip(t)
	dir := t.TempDir()
	src := makeSource(t, tc, dir)
	p, err := tc.Probe(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "fallback.mp4")
	opts := &Options{Resolutions: []string{"360p"}}
	if _, err := tc.BuildMP4Fallback(context.Background(), src, outPath, p, opts); err != nil {
		t.Fatalf("BuildMP4Fallback: %v", err)
	}
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("fallback.mp4 missing/empty: %v", err)
	}
}
