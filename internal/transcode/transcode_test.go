package transcode

import (
	"strings"
	"testing"

	"github.com/pirumar/vodstack/internal/video"
)

func TestEditVideoChain(t *testing.T) {
	spec := &video.EditSpec{
		Crop:   video.Crop{X: 0.5, Y: 0, W: 0.5, H: 1},
		Rotate: 90, Flip: "h",
	}
	chain := editVideoChain(spec)
	for _, want := range []string{"crop=", "transpose=1", "hflip"} {
		if !strings.Contains(chain, want) {
			t.Fatalf("chain %q missing %q", chain, want)
		}
	}
	if editVideoChain(nil) != "" {
		t.Fatal("nil spec must yield empty chain")
	}
	if editVideoChain(&video.EditSpec{Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}) != "" {
		t.Fatal("identity spec must yield empty chain")
	}
}

func TestTrimInputArgs(t *testing.T) {
	spec := &video.EditSpec{Segments: []video.Segment{{Start: 12, End: 42}}, Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}
	ss, dur := trimInputArgs(spec)
	if ss != "12" || dur != "30" { // -ss 12 -t (42-12)=30
		t.Fatalf("got -ss %q -t %q, want 12 / 30", ss, dur)
	}
	if ss, _ := trimInputArgs(nil); ss != "" {
		t.Fatal("nil spec must yield no trim")
	}
	multi := &video.EditSpec{Segments: []video.Segment{{Start: 0, End: 10}, {Start: 20, End: 30}}, Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}
	if ss, _ := trimInputArgs(multi); ss != "" {
		t.Fatal("multi-segment must NOT use input trim")
	}
	if ss, _ := trimInputArgs(&video.EditSpec{Crop: video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none"}); ss != "" {
		t.Fatal("zero-segment spec must yield no trim")
	}
}

func TestEditedSourceArgs_MultiSegment(t *testing.T) {
	spec := &video.EditSpec{
		Segments: []video.Segment{{Start: 0, End: 10}, {Start: 20, End: 35}},
		Crop:     video.Crop{X: 0, Y: 0, W: 1, H: 1}, Flip: "none",
	}
	joined := strings.Join(editedSourceArgs("in.mp4", "out.mp4", spec), " ")
	for _, want := range []string{
		"trim=start=0:end=10", "trim=start=20:end=35",
		"atrim=start=0:end=10", "atrim=start=20:end=35",
		"concat=n=2:v=1:a=1", "out.mp4",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
	// video-only variant omits audio filters and uses a:0 concat
	vo := strings.Join(editedSourceArgsVideoOnly("in.mp4", "out.mp4", spec), " ")
	if strings.Contains(vo, "atrim") {
		t.Fatal("video-only must not contain atrim")
	}
	if !strings.Contains(vo, "concat=n=2:v=1:a=0") {
		t.Fatalf("video-only missing concat=n=2:v=1:a=0: %q", vo)
	}
}

func rungNames(rs []rung) string {
	var names []string
	for _, r := range rs {
		names = append(names, r.name)
	}
	return strings.Join(names, ",")
}

func TestSelectRungs(t *testing.T) {
	cases := []struct {
		sourceHeight int
		enabled      []string
		want         string
	}{
		// nil enabled => default ladder (360..1080), never upscaling.
		{2160, nil, "360p,480p,720p,1080p"},
		{1080, nil, "360p,480p,720p,1080p"},
		{720, nil, "360p,480p,720p"},
		{360, nil, "360p"},
		// 240p source: default set excludes 240p, but the no-upscale fallback emits
		// the largest fitting rung (240p) rather than upscaling to 360p.
		{240, nil, "240p"},
		{200, nil, "240p"}, // smaller than our smallest rung: emit the lowest (240p)
		// Explicit enabled set including the new rungs.
		{2160, []string{"240p", "720p", "1440p", "2160p"}, "240p,720p,1440p,2160p"},
		{1080, []string{"240p", "720p", "1440p", "2160p"}, "240p,720p"}, // 1440/2160 dropped (no upscale)
		// Over-restrictive config (only 1440p, but source is 720p): fall back to the
		// largest fitting rung so the ladder is never empty.
		{720, []string{"1440p"}, "720p"},
	}
	for _, c := range cases {
		got := rungNames(selectRungs(c.sourceHeight, c.enabled))
		if got != c.want {
			t.Errorf("selectRungs(%d, %v) = %q, want %q", c.sourceHeight, c.enabled, got, c.want)
		}
	}
}

func probe(h int, tracks ...AudioTrack) *Probe {
	p := &Probe{Width: h * 16 / 9, Height: h, DurationSeconds: 100}
	if len(tracks) > 0 {
		p.HasAudio = true
		p.AudioTracks = tracks
	}
	return p
}

func TestBuildArgsH264Shape(t *testing.T) {
	tc := New("ffmpeg", "ffprobe")
	p := probe(720, AudioTrack{Language: "eng", Name: "English"})
	rungs := selectRungs(p.Height, nil) // 3 rungs
	joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, nil, h264Plan(), ""), " ")

	for _, want := range []string{
		"-progress pipe:1",
		"-c:v:0 libx264",
		"-hls_segment_type fmp4",
		"-master_pl_name master.m3u8",
		"-var_stream_map v:0,a:0 v:1,a:1 v:2,a:2",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("h264 args missing %q\nfull: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-hls_key_info_file") {
		t.Error("unencrypted args should not contain -hls_key_info_file")
	}

	enc := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, nil, h264Plan(), "/tmp/k.keyinfo"), " ")
	if !strings.Contains(enc, "-hls_key_info_file /tmp/k.keyinfo") || !strings.Contains(enc, "-hls_segment_type mpegts") {
		t.Errorf("encrypted args missing key info / mpegts: %s", enc)
	}
}

func TestBuildArgsNoAudio(t *testing.T) {
	tc := New("ffmpeg", "ffprobe")
	p := probe(360)
	rungs := selectRungs(p.Height, nil)
	joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, nil, h264Plan(), ""), " ")
	if !strings.Contains(joined, "-var_stream_map v:0") {
		t.Errorf("expected video-only stream map: %s", joined)
	}
	if strings.Contains(joined, "a:0") {
		t.Errorf("no-audio source should not map audio: %s", joined)
	}
}

func TestBuildArgsWatermark(t *testing.T) {
	tc := New("ffmpeg", "ffprobe")
	p := probe(720, AudioTrack{Language: "eng", Name: "English"})
	rungs := selectRungs(p.Height, nil)
	opts := &Options{Watermark: &WatermarkSpec{Path: "/wm.png", Position: "bottomRight", Opacity: 0.5, Margin: 24}}
	joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, opts, h264Plan(), ""), " ")

	if !strings.Contains(joined, "-i /wm.png") {
		t.Errorf("watermark build missing second input: %s", joined)
	}
	if !strings.Contains(joined, "colorchannelmixer=aa=0.5") {
		t.Errorf("watermark opacity not applied: %s", joined)
	}
	if !strings.Contains(joined, "overlay=W-w-24:H-h-24") {
		t.Errorf("watermark overlay position wrong: %s", joined)
	}
}

func TestBuildArgsMultiAudio(t *testing.T) {
	tc := New("ffmpeg", "ffprobe")
	p := probe(720, AudioTrack{Language: "eng", Name: "English"}, AudioTrack{Language: "tur", Name: "Türkçe"})
	rungs := selectRungs(p.Height, nil) // 3 video rungs
	opts := &Options{MultiAudio: true}
	joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, opts, h264Plan(), ""), " ")

	for _, want := range []string{
		"-map 0:a:0",
		"-map 0:a:1",
		"v:0,agroup:aud",
		"a:0,agroup:aud,language:eng,name:audio_0,default:yes",
		"a:1,agroup:aud,language:tur,name:audio_1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("multi-audio args missing %q\nfull: %s", want, joined)
		}
	}
}

func TestBuildArgsSingleTrackIgnoresMultiAudio(t *testing.T) {
	tc := New("ffmpeg", "ffprobe")
	p := probe(360, AudioTrack{Language: "eng", Name: "English"}) // only one track
	rungs := selectRungs(p.Height, nil)
	opts := &Options{MultiAudio: true}
	joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, opts, h264Plan(), ""), " ")
	if strings.Contains(joined, "agroup:aud") {
		t.Errorf("single-track source should not use an audio group: %s", joined)
	}
}

func TestBackfillPlans(t *testing.T) {
	cases := map[string][]string{
		"av1":  {"libsvtav1", "master_av1.m3u8"},
		"hevc": {"libx265", "hvc1", "master_hevc.m3u8"},
		"vp9":  {"libvpx-vp9", "master_vp9.m3u8"},
	}
	tc := New("ffmpeg", "ffprobe")
	p := probe(720, AudioTrack{Language: "eng", Name: "English"})
	rungs := selectRungs(p.Height, nil)
	for codec, wants := range cases {
		joined := strings.Join(tc.buildArgs("/in/source", "/out", rungs, p, nil, backfillPlan(codec), ""), " ")
		for _, w := range wants {
			if !strings.Contains(joined, w) {
				t.Errorf("backfill %s args missing %q\nfull: %s", codec, w, joined)
			}
		}
	}
}

func TestCodecsTag(t *testing.T) {
	cases := []struct {
		codec, res, want string
	}{
		{"av1", "360p,720p,1080p", "av01.0.08M.08"},
		{"av1", "1440p", "av01.0.09M.08"},
		{"av1", "2160p", "av01.0.12M.08"},
		{"hevc", "1080p", "hvc1.1.6.L120.B0"},
		{"hevc", "2160p", "hvc1.1.6.L153.B0"},
		{"vp9", "1080p", "vp09.00.41.08"},
	}
	for _, c := range cases {
		if got := CodecsTag(c.codec, c.res); got != c.want {
			t.Errorf("CodecsTag(%q,%q) = %q, want %q", c.codec, c.res, got, c.want)
		}
	}
}

func TestMP4FallbackRungCaps1080(t *testing.T) {
	if r := mp4FallbackRung(2160, []string{"1440p", "2160p"}); r.name != "1080p" {
		t.Errorf("mp4 fallback should cap at 1080p, got %s", r.name)
	}
	if r := mp4FallbackRung(720, nil); r.name != "720p" {
		t.Errorf("mp4 fallback should pick tallest <=1080p that fits, got %s", r.name)
	}
}
