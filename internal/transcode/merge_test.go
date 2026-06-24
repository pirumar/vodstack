package transcode

import (
	"strings"
	"testing"
)

func TestMergeMasters(t *testing.T) {
	h264 := "#EXTM3U\n#EXT-X-VERSION:7\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,CODECS=\"avc1.640028,mp4a.40.2\"\n" +
		"0/playlist.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720,CODECS=\"avc1.640028,mp4a.40.2\"\n" +
		"1/playlist.m3u8\n"
	// AV1 master WITHOUT CODECS (mirrors ffmpeg 5.x output) — must be injected.
	av1 := "#EXTM3U\n#EXT-X-VERSION:7\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=600000,RESOLUTION=640x360\n" +
		"0/playlist.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720\n" +
		"1/playlist.m3u8\n"

	av1Codecs := CodecsTag("av1", "360p,720p")
	out := string(MergeMasters([]byte(h264), []byte(av1), "av1/", av1Codecs+",mp4a.40.2"))

	// Header present once.
	if strings.Count(out, "#EXTM3U") != 1 {
		t.Fatalf("expected exactly one #EXTM3U header:\n%s", out)
	}
	// Both codec families present (AV1 CODECS injected).
	if !strings.Contains(out, "avc1.640028") || !strings.Contains(out, av1Codecs) {
		t.Fatalf("combined master missing a codec family:\n%s", out)
	}
	// AV1 variants got a CODECS attribute injected.
	if strings.Count(out, "av01") < 2 {
		t.Fatalf("expected 2 injected av01 CODECS:\n%s", out)
	}
	// H.264 URIs unprefixed; AV1 URIs prefixed with av1/.
	if !strings.Contains(out, "\n0/playlist.m3u8\n") {
		t.Fatalf("expected unprefixed H.264 URI:\n%s", out)
	}
	if !strings.Contains(out, "\nav1/0/playlist.m3u8\n") || !strings.Contains(out, "\nav1/1/playlist.m3u8\n") {
		t.Fatalf("expected av1/-prefixed AV1 URIs:\n%s", out)
	}
	// Four variants total (2 H.264 + 2 AV1).
	if got := strings.Count(out, "#EXT-X-STREAM-INF"); got != 4 {
		t.Fatalf("expected 4 stream-inf entries, got %d:\n%s", got, out)
	}
}

// A multi-audio H.264 base has #EXT-X-MEDIA audio renditions; merging an extra
// codec must preserve them (the audio group is shared, not re-encoded per codec).
func TestMergeMastersPreservesAudioGroup(t *testing.T) {
	h264 := "#EXTM3U\n#EXT-X-VERSION:7\n" +
		`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="group_aud",NAME="audio_1",DEFAULT=YES,LANGUAGE="eng",URI="audio_0/playlist.m3u8"` + "\n" +
		`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="group_aud",NAME="audio_2",DEFAULT=NO,LANGUAGE="tur",URI="audio_1/playlist.m3u8"` + "\n" +
		`#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,CODECS="avc1.640028,mp4a.40.2",AUDIO="group_aud"` + "\n" +
		"0/playlist.m3u8\n"
	hevc := "#EXTM3U\n#EXT-X-VERSION:7\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=700000,RESOLUTION=640x360\n" +
		"0/playlist.m3u8\n"

	out := string(MergeMasters([]byte(h264), []byte(hevc), "hevc/", "hvc1.1.6.L120.B0,mp4a.40.2"))

	if strings.Count(out, "#EXT-X-MEDIA:TYPE=AUDIO") != 2 {
		t.Fatalf("audio group renditions dropped on merge:\n%s", out)
	}
	if !strings.Contains(out, `AUDIO="group_aud"`) {
		t.Fatalf("base stream-inf lost its AUDIO group:\n%s", out)
	}
	if !strings.Contains(out, "\nhevc/0/playlist.m3u8\n") {
		t.Fatalf("hevc variant not appended with prefix:\n%s", out)
	}
}
