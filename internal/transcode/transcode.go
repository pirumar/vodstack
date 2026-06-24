// Package transcode orchestrates ffprobe + ffmpeg to turn a raw source file
// into an adaptive-bitrate HLS ladder (fMP4/CMAF) plus a poster image.
//
// It shells out to the ffmpeg/ffprobe binaries (subprocess isolation: a crash
// in ffmpeg cannot take down the worker, and upgrading ffmpeg is just swapping
// the binary). All output lands under a local directory which the worker then
// mirrors to MinIO.
package transcode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pirumar/vodstack/internal/video"
)

// rung is one line of the ABR ladder. The per-codec CRF columns are the
// constant-quality targets for the bulk-lane backfills (lower = better quality).
type rung struct {
	name                       string
	w, h                       int
	vbitrate, maxrate, bufsize string
	abitrate                   string
	av1crf                     string // SVT-AV1 (libsvtav1)
	hevccrf                    string // HEVC (libx265)
	vp9crf                     string // VP9 (libvpx-vp9)
}

// ladder is ordered low -> high. Rungs above the source height are dropped so we
// never upscale; selectRungs also filters to the library's enabled resolutions.
// Bitrates for the new 240p/1440p/2160p rungs follow Bunny Stream's published
// ladder. 1440p/2160p on a CPU-only box are slow — kept opt-in via the config.
var ladder = []rung{
	{"240p", 426, 240, "500k", "535k", "750k", "64k", "40", "30", "37"},
	{"360p", 640, 360, "800k", "856k", "1200k", "96k", "38", "30", "36"},
	{"480p", 854, 480, "1400k", "1498k", "2100k", "128k", "36", "28", "34"},
	{"720p", 1280, 720, "2800k", "2996k", "4200k", "128k", "34", "26", "33"},
	{"1080p", 1920, 1080, "5000k", "5350k", "7500k", "192k", "32", "24", "31"},
	{"1440p", 2560, 1440, "8000k", "8560k", "12000k", "192k", "30", "23", "30"},
	{"2160p", 3840, 2160, "16000k", "17120k", "24000k", "192k", "28", "22", "28"},
}

// crf returns the constant-quality target for the named backfill codec.
func (r rung) crf(codec string) string {
	switch codec {
	case "hevc":
		return r.hevccrf
	case "vp9":
		return r.vp9crf
	default:
		return r.av1crf
	}
}

// Probe holds the source characteristics we need to plan the encode.
type Probe struct {
	DurationSeconds int
	Width           int
	Height          int
	HasAudio        bool
	// AudioTracks lists every audio stream in source order (a:0, a:1, ...) with
	// its language/label, used for multi-audio encoding. Always populated; for a
	// single-track source it has one entry.
	AudioTracks []AudioTrack
}

// AudioTrack is one source audio stream's metadata.
type AudioTrack struct {
	Language string // ISO code from stream tags, e.g. "eng" ("und" if unknown)
	Name     string // display label (stream title tag, else the language)
}

// WatermarkSpec is a resolved burned-in overlay: a local image path plus its
// placement. nil Options.Watermark means no overlay.
type WatermarkSpec struct {
	Path     string  // local path to the watermark image
	Position string  // topLeft|topRight|bottomLeft|bottomRight|center
	Opacity  float64 // 0..1
	Margin   int     // px inset from the frame edge
}

// Options tunes a build: which resolutions to emit, an optional watermark, and
// whether to encode every audio track as a selectable HLS audio group. A nil
// *Options (or zero value) reproduces the historical full-ladder, single-audio,
// no-watermark behavior.
type Options struct {
	Resolutions []string // enabled rung names; empty => full default ladder
	Watermark   *WatermarkSpec
	MultiAudio  bool
	Edit        *video.EditSpec
}

func (o *Options) edit() *video.EditSpec {
	if o == nil {
		return nil
	}
	return o.Edit
}

func (o *Options) resolutions() []string {
	if o == nil {
		return nil
	}
	return o.Resolutions
}

func (o *Options) watermark() *WatermarkSpec {
	if o == nil {
		return nil
	}
	return o.Watermark
}

func (o *Options) multiAudio() bool {
	return o != nil && o.MultiAudio
}

// Result summarizes a successful transcode.
type Result struct {
	AvailableResolutions string // CSV, e.g. "360p,720p"
	ThumbnailFile        string // poster filename relative to the HLS prefix
	ThumbnailsVTT        string // seek-preview WebVTT filename ("" if none)
}

// Transcoder runs ffmpeg/ffprobe using the configured binary paths.
type Transcoder struct {
	FFmpegBin  string
	FFprobeBin string
}

func New(ffmpegBin, ffprobeBin string) *Transcoder {
	return &Transcoder{FFmpegBin: ffmpegBin, FFprobeBin: ffprobeBin}
}

// ffprobe JSON (subset we read).
type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Tags      struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe inspects the source file.
func (t *Transcoder) Probe(ctx context.Context, srcPath string) (*Probe, error) {
	cmd := exec.CommandContext(ctx, t.FFprobeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		srcPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var po probeOutput
	if err := json.Unmarshal(out, &po); err != nil {
		return nil, fmt.Errorf("ffprobe parse: %w", err)
	}
	p := &Probe{}
	for _, s := range po.Streams {
		switch s.CodecType {
		case "video":
			if s.Width > p.Width {
				p.Width = s.Width
				p.Height = s.Height
			}
		case "audio":
			p.HasAudio = true
			lang := strings.TrimSpace(s.Tags.Language)
			if lang == "" {
				lang = "und"
			}
			name := strings.TrimSpace(s.Tags.Title)
			if name == "" {
				name = strings.ToUpper(lang)
			}
			p.AudioTracks = append(p.AudioTracks, AudioTrack{Language: lang, Name: name})
		}
	}
	if p.Width == 0 || p.Height == 0 {
		return nil, fmt.Errorf("ffprobe: no video stream found")
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(po.Format.Duration), 64); err == nil {
		p.DurationSeconds = int(f)
	}
	return p, nil
}

// selectRungs returns the ladder rungs to encode: those that fit the source
// height (never upscaling) AND are in the enabled set. enabled nil/empty means
// the default ladder (360p..1080p). Always returns at least one rung so a tiny
// source or an over-restrictive config still yields playable output.
func selectRungs(sourceHeight int, enabled []string) []rung {
	want := map[string]bool{}
	for _, name := range enabled {
		want[name] = true
	}
	if len(want) == 0 {
		for _, name := range defaultResolutions {
			want[name] = true
		}
	}

	var sel []rung
	for _, r := range ladder {
		if r.h <= sourceHeight && want[r.name] {
			sel = append(sel, r)
		}
	}
	if len(sel) > 0 {
		return sel
	}
	// Nothing enabled fits the source. Fall back to the largest rung that does
	// fit (ignoring the enabled set) so we never publish an empty ladder.
	for i := len(ladder) - 1; i >= 0; i-- {
		if ladder[i].h <= sourceHeight {
			return []rung{ladder[i]}
		}
	}
	return []rung{ladder[0]} // source smaller than our smallest rung
}

// defaultResolutions mirrors encoding.DefaultConfig().Resolutions; duplicated
// here to keep the transcode package free of an import cycle with config.
var defaultResolutions = []string{"360p", "480p", "720p", "1080p"}

// BuildHLS produces the ABR ladder + poster under outDir. Layout:
//
//	outDir/master.m3u8
//	outDir/0/playlist.m3u8  outDir/0/init.mp4  outDir/0/seg_00000.m4s ...
//	outDir/1/...
//	outDir/poster.jpg
//
// opts (may be nil) selects the enabled resolutions, an optional burned-in
// watermark, and multi-audio encoding; nil reproduces the historical full-ladder,
// single-audio, no-watermark behavior.
// onProgress (may be nil) is called with the encode percentage (0-100) as
// ffmpeg advances through the source.
// keyInfoPath (may be "") points at an ffmpeg HLS key-info file; when set the
// ladder is AES-128 encrypted and seek-preview thumbnails are skipped (the
// encrypted rendition cannot be decoded without the key server).
func (t *Transcoder) BuildHLS(ctx context.Context, srcPath, outDir string, p *Probe, opts *Options, onProgress func(pct int), keyInfoPath string) (*Result, error) {
	rungs := selectRungs(p.Height, opts.resolutions())

	// ffmpeg's HLS muxer writes into %v subdirs that must already exist. In
	// multi-audio mode the audio renditions occupy subdirs after the video ones.
	am := planAudio(p, opts)
	if err := makeVariantDirs(outDir, len(rungs)+am.extraDirs()); err != nil {
		return nil, err
	}

	args := t.buildArgs(srcPath, outDir, rungs, p, opts, h264Plan(), keyInfoPath)
	cmd := exec.CommandContext(ctx, t.FFmpegBin, args...)
	cmd.Stderr = os.Stderr // ffmpeg logs to stderr; surface it in worker logs

	// ffmpeg writes machine-readable progress (key=value) to stdout via
	// "-progress pipe:1". We parse out_time_us to compute a percentage.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	t.scanProgress(stdout, p.DurationSeconds, onProgress)
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg transcode: %w", err)
	}

	posterAt := 3.0
	if e := opts.edit(); e != nil && len(e.Segments) == 1 {
		s := e.Segments[0]
		posterAt = s.Start + 3
		if posterAt >= s.End {
			posterAt = s.Start + (s.End-s.Start)/2
		}
	}
	if err := t.posterFrame(ctx, srcPath, filepath.Join(outDir, "poster.jpg"), posterAt, editVideoChain(opts.edit())); err != nil {
		return nil, fmt.Errorf("poster: %w", err)
	}

	// Seek-preview sprite + WebVTT. Generated from the lowest rendition (fast to
	// decode) rather than the source. Best-effort: a failure here doesn't fail
	// the whole transcode. Skipped when encrypting (the rendition needs the key
	// server to decode); an encrypt re-encode keeps the existing thumbnails.
	var vtt string
	if keyInfoPath == "" {
		if v, err := t.thumbnails(ctx, outDir, p); err == nil {
			vtt = v
		}
	}

	names := make([]string, len(rungs))
	for i, r := range rungs {
		names[i] = r.name
	}
	return &Result{
		AvailableResolutions: strings.Join(names, ","),
		ThumbnailFile:        "poster.jpg",
		ThumbnailsVTT:        vtt,
	}, nil
}

// thumbnails builds a sprite sheet + WebVTT mapping time ranges to sprite tiles,
// for hover/scrub previews. Decodes the already-produced 360p rendition (rung 0)
// so it stays cheap even for multi-hour sources. Returns the VTT filename.
func (t *Transcoder) thumbnails(ctx context.Context, outDir string, p *Probe) (string, error) {
	if p.DurationSeconds <= 0 {
		return "", nil
	}
	const (
		maxThumbs = 100
		cols      = 10
		thumbW    = 160
	)
	interval := p.DurationSeconds / maxThumbs
	if interval < 2 {
		interval = 2
	}
	count := (p.DurationSeconds + interval - 1) / interval
	if count < 1 {
		count = 1
	}
	rows := (count + cols - 1) / cols

	thumbH := thumbW * p.Height / p.Width
	if thumbH < 2 {
		thumbH = 90
	}
	if thumbH%2 != 0 {
		thumbH++
	}

	// Decode the 360p rendition (rung 0) — far cheaper than the source.
	src := filepath.Join(outDir, "0", "playlist.m3u8")
	sprite := "sprite.jpg"
	vf := fmt.Sprintf("fps=1/%d,scale=%d:%d,tile=%dx%d", interval, thumbW, thumbH, cols, rows)
	cmd := exec.CommandContext(ctx, t.FFmpegBin,
		"-y", "-allowed_extensions", "ALL", "-i", src,
		"-frames:v", "1", "-vf", vf, "-an",
		filepath.Join(outDir, sprite),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sprite: %w", err)
	}

	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i := 0; i < count; i++ {
		start := i * interval
		end := start + interval
		if end > p.DurationSeconds {
			end = p.DurationSeconds
		}
		x := (i % cols) * thumbW
		y := (i / cols) * thumbH
		fmt.Fprintf(&b, "%s --> %s\n%s#xywh=%d,%d,%d,%d\n\n",
			vttTime(start), vttTime(end), sprite, x, y, thumbW, thumbH)
	}
	const vttName = "thumbnails.vtt"
	if err := os.WriteFile(filepath.Join(outDir, vttName), []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return vttName, nil
}

func vttTime(sec int) string {
	return fmt.Sprintf("%02d:%02d:%02d.000", sec/3600, (sec%3600)/60, sec%60)
}

// scanProgress reads ffmpeg's -progress output and reports a percentage based
// on the processed input time vs the total duration. Blocks until the pipe
// closes (ffmpeg exits).
func (t *Transcoder) scanProgress(r io.Reader, durationSeconds int, onProgress func(pct int)) {
	if onProgress == nil || durationSeconds <= 0 {
		// Drain so ffmpeg's stdout buffer never blocks.
		_, _ = io.Copy(io.Discard, r)
		return
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		v, ok := strings.CutPrefix(line, "out_time_us=")
		if !ok {
			continue
		}
		us, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		pct := int(float64(us) / 1e6 / float64(durationSeconds) * 100)
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		onProgress(pct)
	}
}

// makeVariantDirs pre-creates outDir/0 .. outDir/count-1 (ffmpeg's HLS muxer
// requires the %v segment directories to exist).
func makeVariantDirs(outDir string, count int) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := os.MkdirAll(filepath.Join(outDir, strconv.Itoa(i)), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// audioMapping is the resolved audio layout for one encode. In group mode every
// source track becomes a shared HLS audio rendition (#EXT-X-MEDIA TYPE=AUDIO);
// otherwise the first track is muxed once into each video variant (legacy).
type audioMapping struct {
	hasAudio  bool
	groupMode bool
	tracks    []AudioTrack
}

// extraDirs is how many %v subdirs the audio renditions need beyond the video
// rungs (one per track in group mode, zero otherwise).
func (am audioMapping) extraDirs() int {
	if am.groupMode {
		return len(am.tracks)
	}
	return 0
}

// planAudio decides the audio layout. Group mode requires multi-audio to be
// enabled AND more than one source track; otherwise we keep the simple muxed
// single-track behavior.
func planAudio(p *Probe, opts *Options) audioMapping {
	if !p.HasAudio {
		return audioMapping{}
	}
	if opts.multiAudio() && len(p.AudioTracks) > 1 {
		return audioMapping{hasAudio: true, groupMode: true, tracks: p.AudioTracks}
	}
	return audioMapping{hasAudio: true}
}

// codecPlan captures the per-codec ffmpeg differences: the video encoder/quality
// args per rung, the shared video args, and the master playlist filename.
type codecPlan struct {
	masterName string
	perVariant func(i int, r rung) []string
	shared     []string
}

func h264Plan() codecPlan {
	return codecPlan{
		masterName: "master.m3u8",
		perVariant: func(i int, r rung) []string {
			return []string{
				fmt.Sprintf("-c:v:%d", i), "libx264",
				fmt.Sprintf("-b:v:%d", i), r.vbitrate,
				fmt.Sprintf("-maxrate:v:%d", i), r.maxrate,
				fmt.Sprintf("-bufsize:v:%d", i), r.bufsize,
			}
		},
		shared: []string{
			"-preset", "veryfast",
			"-profile:v", "high",
			"-pix_fmt", "yuv420p",
			"-g", "48", "-keyint_min", "48", "-sc_threshold", "0",
			"-flags", "+cgop",
		},
	}
}

// backfillPlan returns the encode plan for a bulk-lane codec backfill. All use
// single-pass constant-quality (CRF) — two-pass is not worth the CPU for VOD.
func backfillPlan(codec string) codecPlan {
	switch codec {
	case "hevc":
		return codecPlan{
			masterName: "master_hevc.m3u8",
			perVariant: func(i int, r rung) []string {
				return []string{
					fmt.Sprintf("-c:v:%d", i), "libx265",
					fmt.Sprintf("-crf:v:%d", i), r.crf("hevc"),
				}
			},
			// hvc1 tag (not hev1) so Safari/HLS recognizes the fMP4 HEVC variant.
			shared: []string{
				"-preset", "medium",
				"-tag:v", "hvc1",
				"-pix_fmt", "yuv420p",
				"-g", "48", "-keyint_min", "48",
			},
		}
	case "vp9":
		return codecPlan{
			masterName: "master_vp9.m3u8",
			perVariant: func(i int, r rung) []string {
				return []string{
					fmt.Sprintf("-c:v:%d", i), "libvpx-vp9",
					fmt.Sprintf("-crf:v:%d", i), r.crf("vp9"),
					fmt.Sprintf("-b:v:%d", i), "0", // CRF (constant-quality) mode
				}
			},
			shared: []string{
				"-row-mt", "1", // multi-threaded VP9 row encoding
				"-pix_fmt", "yuv420p",
				"-g", "48", "-keyint_min", "48",
			},
		}
	default: // av1
		return codecPlan{
			masterName: "master_av1.m3u8",
			perVariant: func(i int, r rung) []string {
				return []string{
					fmt.Sprintf("-c:v:%d", i), "libsvtav1",
					fmt.Sprintf("-crf:v:%d", i), r.crf("av1"),
				}
			},
			// preset 8 keeps SVT-AV1 tractable on a CPU-only box.
			shared: []string{
				"-preset", "8",
				"-pix_fmt", "yuv420p",
				"-g", "48", "-keyint_min", "48",
			},
		}
	}
}

// videoFilterComplex builds the filter graph: optionally overlay a watermark on
// the source, then split into one scaled branch per rung. Returns the graph and
// the number of extra -i inputs it implies (1 for a watermark image, else 0).
func videoFilterComplex(rungs []rung, wm *WatermarkSpec, edit *video.EditSpec) (string, int) {
	n := len(rungs)
	var fc strings.Builder
	base := "[0:v]"
	extraInputs := 0
	if chain := editVideoChain(edit); chain != "" {
		// Spatial edit (crop/rotate/flip) is applied first, rewriting the source
		// into [edited] before the watermark/split so it is the geometric base.
		fmt.Fprintf(&fc, "[0:v]%s[edited];", chain)
		base = "[edited]"
	}
	if wm != nil {
		// Watermark is the second input [1:v]: apply opacity, then overlay it at
		// the configured corner before the split so it is burned into every rung.
		extraInputs = 1
		fmt.Fprintf(&fc, "[1:v]format=rgba,colorchannelmixer=aa=%s[wm];%s[wm]overlay=%s:format=auto[wmv];",
			trimFloat(wm.Opacity), base, overlayExpr(wm.Position, wm.Margin))
		base = "[wmv]"
	}
	fmt.Fprintf(&fc, "%ssplit=%d", base, n)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc, "[v%d]", i)
	}
	fc.WriteString(";")
	for i, r := range rungs {
		// force_original_aspect_ratio + pad keeps geometry sane for odd sources.
		fmt.Fprintf(&fc, "[v%d]scale=w=%d:h=%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2[v%dout]",
			i, r.w, r.h, r.w, r.h, i)
		if i < n-1 {
			fc.WriteString(";")
		}
	}
	return fc.String(), extraInputs
}

// overlayExpr is the ffmpeg overlay x:y expression for a corner/center position
// with a margin inset (W/H = main video, w/h = overlay).
func overlayExpr(position string, margin int) string {
	m := strconv.Itoa(margin)
	switch position {
	case "topLeft":
		return m + ":" + m
	case "topRight":
		return "W-w-" + m + ":" + m
	case "bottomLeft":
		return m + ":H-h-" + m
	case "center":
		return "(W-w)/2:(H-h)/2"
	default: // bottomRight
		return "W-w-" + m + ":H-h-" + m
	}
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// editVideoChain builds the comma-separated spatial filter chain (crop -> rotate
// -> flip) applied before the watermark/split. Returns "" when the spec implies
// no spatial change. Crop is normalized 0..1; output dims floor to even (yuv420p).
func editVideoChain(e *video.EditSpec) string {
	if e == nil {
		return ""
	}
	var parts []string
	if c := e.Crop; !(c.X == 0 && c.Y == 0 && c.W == 1 && c.H == 1) {
		parts = append(parts, fmt.Sprintf(
			"crop=floor(iw*%s/2)*2:floor(ih*%s/2)*2:iw*%s:ih*%s",
			trimFloat(c.W), trimFloat(c.H), trimFloat(c.X), trimFloat(c.Y)))
	}
	switch e.Rotate {
	case 90:
		parts = append(parts, "transpose=1")
	case 180:
		parts = append(parts, "transpose=2", "transpose=2")
	case 270:
		parts = append(parts, "transpose=2")
	}
	switch e.Flip {
	case "h":
		parts = append(parts, "hflip")
	case "v":
		parts = append(parts, "vflip")
	}
	return strings.Join(parts, ",")
}

// trimInputArgs returns the (-ss, -t) values for a single-segment trim, or
// ("","") when no single-range trim applies. Multi-segment edits return ("","")
// because they are materialized by BuildEditedSource before transcode.
func trimInputArgs(e *video.EditSpec) (ss string, dur string) {
	if e == nil || len(e.Segments) != 1 {
		return "", ""
	}
	s := e.Segments[0]
	return trimFloat(s.Start), trimFloat(s.End - s.Start)
}

// editedSourceArgs builds the ffmpeg invocation that concatenates the kept
// segments (video + audio) into one near-lossless mp4 mezzanine. Spatial ops are
// NOT applied here; the normal ladder applies crop/rotate via editVideoChain on
// the mezzanine. Assumes len(e.Segments) >= 2 and the source has audio.
func editedSourceArgs(srcPath, outPath string, e *video.EditSpec) []string {
	n := len(e.Segments)
	var fc strings.Builder
	for i, s := range e.Segments {
		fmt.Fprintf(&fc, "[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[v%d];",
			trimFloat(s.Start), trimFloat(s.End), i)
		fmt.Fprintf(&fc, "[0:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS[a%d];",
			trimFloat(s.Start), trimFloat(s.End), i)
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc, "[v%d][a%d]", i, i)
	}
	fmt.Fprintf(&fc, "concat=n=%d:v=1:a=1[v][a]", n)
	return []string{
		"-y", "-nostats", "-i", srcPath,
		"-filter_complex", fc.String(),
		"-map", "[v]", "-map", "[a]",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "16", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart", outPath,
	}
}

// editedSourceArgsVideoOnly is editedSourceArgs for sources with no audio track.
func editedSourceArgsVideoOnly(srcPath, outPath string, e *video.EditSpec) []string {
	n := len(e.Segments)
	var fc strings.Builder
	for i, s := range e.Segments {
		fmt.Fprintf(&fc, "[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[v%d];",
			trimFloat(s.Start), trimFloat(s.End), i)
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc, "[v%d]", i)
	}
	fmt.Fprintf(&fc, "concat=n=%d:v=1:a=0[v]", n)
	return []string{
		"-y", "-nostats", "-i", srcPath, "-filter_complex", fc.String(),
		"-map", "[v]", "-c:v", "libx264", "-preset", "veryfast", "-crf", "16",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart", outPath,
	}
}

// BuildEditedSource materializes a multi-segment edit into outPath and returns it.
// The caller then transcodes outPath through the normal ladder.
func (t *Transcoder) BuildEditedSource(ctx context.Context, srcPath, outPath string, e *video.EditSpec) (string, error) {
	cmd := exec.CommandContext(ctx, t.FFmpegBin, editedSourceArgs(srcPath, outPath, e)...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build edited source: %w", err)
	}
	return outPath, nil
}

// BuildEditedSourceVideoOnly is BuildEditedSource for sources with no audio.
func (t *Transcoder) BuildEditedSourceVideoOnly(ctx context.Context, srcPath, outPath string, e *video.EditSpec) (string, error) {
	cmd := exec.CommandContext(ctx, t.FFmpegBin, editedSourceArgsVideoOnly(srcPath, outPath, e)...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build edited source (video only): %w", err)
	}
	return outPath, nil
}

// varStreamMap builds ffmpeg's -var_stream_map value for n video rungs and the
// resolved audio mapping.
func varStreamMap(n int, am audioMapping) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		switch {
		case am.groupMode:
			fmt.Fprintf(&b, "v:%d,agroup:aud", i)
		case am.hasAudio:
			fmt.Fprintf(&b, "v:%d,a:%d", i, i)
		default:
			fmt.Fprintf(&b, "v:%d", i)
		}
	}
	if am.groupMode {
		for k, tr := range am.tracks {
			def := ""
			if k == 0 {
				def = ",default:yes"
			}
			// ffmpeg uses name: as the output subdir (not the m3u8 NAME attribute,
			// which it auto-generates). Use a unique audio_<k> so the per-track
			// playlist dirs never collide; LANGUAGE carries the display signal.
			fmt.Fprintf(&b, " a:%d,agroup:aud,language:%s,name:audio_%d%s",
				k, sanitizeToken(tr.Language), k, def)
		}
	}
	return b.String()
}

// sanitizeToken strips characters that would break var_stream_map parsing
// (entries are space-separated, fields comma-separated).
func sanitizeToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" {
		s = "und"
	}
	return s
}

// buildArgs assembles the full ffmpeg invocation for one codec ladder, shared by
// the H.264 main transcode and the bulk-lane backfills. keyInfoPath (H.264 only)
// switches to AES-128 MPEG-TS segments.
func (t *Transcoder) buildArgs(srcPath, outDir string, rungs []rung, p *Probe, opts *Options, plan codecPlan, keyInfoPath string) []string {
	n := len(rungs)
	am := planAudio(p, opts)
	wm := opts.watermark()
	fc, extraInputs := videoFilterComplex(rungs, wm, opts.edit())

	args := []string{"-y", "-nostats", "-progress", "pipe:1"}
	if ss, dur := trimInputArgs(opts.edit()); ss != "" {
		args = append(args, "-ss", ss, "-t", dur)
	}
	args = append(args, "-i", srcPath)
	if extraInputs == 1 {
		args = append(args, "-i", wm.Path)
	}
	args = append(args, "-filter_complex", fc)

	// Map video outputs first, then audio.
	for i := 0; i < n; i++ {
		args = append(args, "-map", fmt.Sprintf("[v%dout]", i))
	}
	if am.groupMode {
		for k := range am.tracks {
			args = append(args, "-map", fmt.Sprintf("0:a:%d", k))
		}
	} else if am.hasAudio {
		for i := 0; i < n; i++ {
			args = append(args, "-map", "a:0")
		}
	}

	// Per-variant video codec settings.
	for i, r := range rungs {
		args = append(args, plan.perVariant(i, r)...)
	}
	// Audio codec (applies to every audio output).
	if am.hasAudio {
		args = append(args, "-c:a", "aac", "-ar", "48000", "-ac", "2", "-b:a", "128k")
	}
	// Shared encode settings (closed GOP / fixed keyframe interval keep segment
	// boundaries clean and renditions switchable).
	args = append(args, plan.shared...)

	args = append(args,
		"-f", "hls",
		"-hls_time", "4",
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-master_pl_name", plan.masterName,
		"-var_stream_map", varStreamMap(n, am),
	)
	// Segment container: fMP4/CMAF normally, but ffmpeg cannot AES-128-encrypt
	// fMP4 ("Encrypted fmp4 not yet supported"), so encrypted ladders fall back
	// to MPEG-TS segments (which hls.js plays natively with clear-key AES-128).
	if keyInfoPath != "" {
		args = append(args,
			"-hls_segment_type", "mpegts",
			"-hls_segment_filename", filepath.Join(outDir, "%v", "seg_%05d.ts"),
			"-hls_key_info_file", keyInfoPath,
		)
	} else {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.Join(outDir, "%v", "seg_%05d.m4s"),
		)
	}
	args = append(args, filepath.Join(outDir, "%v", "playlist.m3u8"))
	return args
}

// BuildCodec produces a parallel ladder for a non-H.264 codec ("av1","hevc",
// "vp9") under outDir, matching the H.264 rung selection. The master is named
// master_<codec>.m3u8. These codecs are CPU-expensive, so this only ever runs on
// the bulk lane as an opt-in backfill. The watermark (if any) is reapplied for
// visual parity; multi-audio is intentionally not used here (the primary track is
// muxed per variant — the H.264 ladder carries the full audio-group). Returns the
// encoded resolutions (CSV).
func (t *Transcoder) BuildCodec(ctx context.Context, codec, srcPath, outDir string, p *Probe, opts *Options, onProgress func(pct int)) (string, error) {
	rungs := selectRungs(p.Height, opts.resolutions())
	if err := makeVariantDirs(outDir, len(rungs)); err != nil {
		return "", err
	}

	// Reapply the watermark and resolutions, but force single-audio for backfills.
	bopts := &Options{Resolutions: opts.resolutions(), Watermark: opts.watermark(), Edit: opts.edit()}
	args := t.buildArgs(srcPath, outDir, rungs, p, bopts, backfillPlan(codec), "")
	cmd := exec.CommandContext(ctx, t.FFmpegBin, args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg %s start: %w", codec, err)
	}
	t.scanProgress(stdout, p.DurationSeconds, onProgress)
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("ffmpeg %s transcode: %w", codec, err)
	}

	names := make([]string, len(rungs))
	for i, r := range rungs {
		names[i] = r.name
	}
	return strings.Join(names, ","), nil
}

// resolutionHeight maps a ladder rung name to its pixel height (0 if unknown).
func resolutionHeight(name string) int {
	for _, r := range ladder {
		if r.name == name {
			return r.h
		}
	}
	return 0
}

// maxHeightCSV returns the tallest rung height in a CSV like "360p,720p,1080p".
func maxHeightCSV(csv string) int {
	max := 0
	for _, name := range strings.Split(csv, ",") {
		if h := resolutionHeight(strings.TrimSpace(name)); h > max {
			max = h
		}
	}
	return max
}

// CodecsTag returns the HLS CODECS attribute injected onto a backfill codec's
// variants. ffmpeg's HLS muxer omits CODECS for these fMP4 variants, so without
// this a player cannot tell they are AV1/HEVC/VP9 and would not prefer them. The
// level is sized to the tallest enabled resolution (resCSV). mp4a.40.2 is
// appended by the caller when the source has audio.
func CodecsTag(codec, resCSV string) string {
	h := maxHeightCSV(resCSV)
	switch codec {
	case "hevc": // hvc1.<profile>.<compat>.L<level*30>.<constraints>
		lvl := "L120" // 4.0 (<=1080p)
		switch {
		case h > 1440:
			lvl = "L153" // 5.1 (4K)
		case h > 1080:
			lvl = "L123" // 4.1 (1440p)
		}
		return "hvc1.1.6." + lvl + ".B0"
	case "vp9": // vp09.<profile>.<level>.<bitdepth>
		lvl := "41" // 4.1 (~1080p)
		switch {
		case h > 1440:
			lvl = "51"
		case h > 1080:
			lvl = "50"
		}
		return "vp09.00." + lvl + ".08"
	default: // av1: av01.0.<seq_level_idx>M.<bitdepth>
		lvl := "08M" // 4.0
		switch {
		case h > 1440:
			lvl = "12M" // 5.0
		case h > 1080:
			lvl = "09M" // 4.1
		}
		return "av01.0." + lvl + ".08"
	}
}

// MergeMasters builds a combined master playlist by copying every variant from
// baseMaster as-is, then appending each variant from extraMaster with its URI
// prefixed by uriPrefix (e.g. "av1/") and a CODECS attribute (extraCodecs)
// injected where missing. It chains: pass an already-combined master as the base
// to fold in another codec. A player loads the combined master and picks the
// variants whose codec it can decode, falling back to H.264 (avc1).
func MergeMasters(baseMaster, extraMaster []byte, uriPrefix, extraCodecs string) []byte {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	// Copy the base verbatim (minus its header) so #EXT-X-MEDIA audio-group
	// renditions — present when multi-audio is on — survive the merge. The extra
	// codec ladders are single-audio (muxed), so they have no media groups; copy
	// only their stream-inf variants, prefixed + CODECS-injected.
	copyBaseBody(&b, baseMaster)
	writeVariants(&b, extraMaster, uriPrefix, extraCodecs)
	return []byte(b.String())
}

// copyBaseBody writes every line of a master playlist except its #EXTM3U /
// #EXT-X-VERSION header (those are emitted once by the caller), preserving
// #EXT-X-MEDIA, #EXT-X-STREAM-INF, URIs and any other tags as-is.
func copyBaseBody(b *strings.Builder, master []byte) {
	for _, raw := range strings.Split(string(master), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTM3U") || strings.HasPrefix(line, "#EXT-X-VERSION") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
}

// writeVariants copies every EXT-X-STREAM-INF + URI pair from a master playlist,
// optionally prefixing the URI line and injecting a CODECS attribute when the
// stream-inf line lacks one.
func writeVariants(b *strings.Builder, master []byte, uriPrefix, injectCodecs string) {
	lines := strings.Split(string(master), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		// The URI is the next non-blank, non-comment line.
		var uri string
		for j := i + 1; j < len(lines); j++ {
			cand := strings.TrimSpace(lines[j])
			if cand == "" || strings.HasPrefix(cand, "#") {
				continue
			}
			uri = cand
			i = j
			break
		}
		if uri == "" {
			continue
		}
		if injectCodecs != "" && !strings.Contains(line, "CODECS=") {
			line += fmt.Sprintf(`,CODECS="%s"`, injectCodecs)
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(uriPrefix + uri)
		b.WriteString("\n")
	}
}

// ExtractAudioWAV decodes the source's audio to 16 kHz mono PCM WAV — the format
// Whisper expects. Returns the output path.
func (t *Transcoder) ExtractAudioWAV(ctx context.Context, srcPath, outPath string) error {
	cmd := exec.CommandContext(ctx, t.FFmpegBin,
		"-y", "-i", srcPath,
		"-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "pcm_s16le", "-f", "wav",
		outPath,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildMP4Fallback produces a single progressive MP4 (H.264/AAC, +faststart) for
// old devices that cannot play HLS and for downloads. It encodes at the tallest
// enabled rung that is <=1080p (MP4 fallback is capped at 1080p), reapplying the
// watermark for parity with the HLS ladder. Returns the output resolution name.
func (t *Transcoder) BuildMP4Fallback(ctx context.Context, srcPath, outPath string, p *Probe, opts *Options) (string, error) {
	r := mp4FallbackRung(p.Height, opts.resolutions())
	scale := fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		r.w, r.h, r.w, r.h)

	args := []string{"-y"}
	if ss, dur := trimInputArgs(opts.edit()); ss != "" {
		args = append(args, "-ss", ss, "-t", dur)
	}
	args = append(args, "-i", srcPath)
	chain := editVideoChain(opts.edit()) // "" when no spatial edit
	if wm := opts.watermark(); wm != nil {
		args = append(args, "-i", wm.Path)
		src := "[0:v]"
		var pre string
		if chain != "" {
			pre = fmt.Sprintf("[0:v]%s[edited];", chain)
			src = "[edited]"
		}
		fc := fmt.Sprintf("%s[1:v]format=rgba,colorchannelmixer=aa=%s[wm];%s[wm]overlay=%s:format=auto[wmv];[wmv]%s[vout]",
			pre, trimFloat(wm.Opacity), src, overlayExpr(wm.Position, wm.Margin), scale)
		args = append(args, "-filter_complex", fc, "-map", "[vout]")
	} else {
		vf := scale
		if chain != "" {
			vf = chain + "," + scale
		}
		args = append(args, "-vf", vf, "-map", "0:v:0")
	}
	if p.HasAudio {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-pix_fmt", "yuv420p",
		"-b:v", r.vbitrate, "-maxrate", r.maxrate, "-bufsize", r.bufsize,
	)
	if p.HasAudio {
		args = append(args, "-c:a", "aac", "-ar", "48000", "-ac", "2", "-b:a", "128k")
	}
	args = append(args, "-movflags", "+faststart", outPath)

	cmd := exec.CommandContext(ctx, t.FFmpegBin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mp4 fallback: %w", err)
	}
	return r.name, nil
}

// mp4FallbackRung picks the tallest enabled rung <=1080p that fits the source,
// capping at 1080p when only higher rungs are enabled.
func mp4FallbackRung(sourceHeight int, enabled []string) rung {
	rungs := selectRungs(sourceHeight, enabled)
	best := rungs[0]
	for _, r := range rungs {
		if r.h <= 1080 && r.h >= best.h {
			best = r
		}
	}
	if best.h > 1080 {
		for _, r := range ladder {
			if r.name == "1080p" {
				return r
			}
		}
	}
	return best
}

// poster grabs a single frame a few seconds in for the thumbnail.
// posterFrame extracts a single high-quality JPEG at atSeconds (fast input seek),
// optionally applying a video filter chain (crop/rotate/flip) so the poster
// matches the edited output. vfChain "" means no filter.
func (t *Transcoder) posterFrame(ctx context.Context, srcPath, outPath string, atSeconds float64, vfChain string) error {
	if atSeconds < 0 {
		atSeconds = 0
	}
	args := []string{"-y", "-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64), "-i", srcPath, "-frames:v", "1", "-q:v", "2"}
	if vfChain != "" {
		args = append(args, "-vf", vfChain)
	}
	args = append(args, outPath)
	cmd := exec.CommandContext(ctx, t.FFmpegBin, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PosterAt extracts a single high-quality JPEG frame at atSeconds (clamped to
// >= 0). Used by the custom-poster "pick a frame" feature. Fast input seek
// (-ss before -i) keeps it cheap even on long sources.
func (t *Transcoder) PosterAt(ctx context.Context, srcPath, outPath string, atSeconds float64) error {
	if atSeconds < 0 {
		atSeconds = 0
	}
	cmd := exec.CommandContext(ctx, t.FFmpegBin,
		"-y",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", srcPath,
		"-frames:v", "1",
		"-q:v", "2",
		outPath,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
