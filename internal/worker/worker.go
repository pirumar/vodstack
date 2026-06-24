// Package worker consumes transcode jobs from asynq: download the raw source,
// probe it, build the HLS ladder, mirror the output to MinIO, and advance the
// video's status. ffmpeg output is built on local scratch first; only after a
// successful upload is the video marked finished, so a partial/failed encode is
// never referenced by /play (which gates on status).
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/config"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/encoding"
	"github.com/pirumar/vodstack/internal/metrics"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/transcode"
	"github.com/pirumar/vodstack/internal/video"
	"github.com/pirumar/vodstack/internal/webhooks"
)

type Worker struct {
	cfg   *config.Config
	db    *db.DB
	store *storage.Store
	tc    *transcode.Transcoder
	wh    *webhooks.Dispatcher
	queue *queue.Client
}

func New(cfg *config.Config, database *db.DB, store *storage.Store, tc *transcode.Transcoder, wh *webhooks.Dispatcher, q *queue.Client) *Worker {
	return &Worker{cfg: cfg, db: database, store: store, tc: tc, wh: wh, queue: q}
}

// trackOp marks an advanced operation as running and returns a finalizer that
// records done/failed based on the handler's returned error. Call it right after
// the payload parses and defer the finalizer with the handler's named return:
//
//	fin := wk.trackOp(ctx, p.VideoID, db.OpKindAV1)
//	defer func() { fin(err) }()
//
// The terminal write uses a detached context so a 'failed' status is still
// persisted even when the job failed because ctx was canceled/timed out.
func (wk *Worker) trackOp(ctx context.Context, videoID, kind string) func(err error) {
	_ = wk.db.SetOperationStatus(ctx, videoID, kind, db.OpRunning, "")
	return func(err error) {
		status, msg := db.OpDone, ""
		if err != nil {
			status, msg = db.OpFailed, err.Error()
		}
		_ = wk.db.SetOperationStatus(context.Background(), videoID, kind, status, msg)
	}
}

// Mux wires the task types this worker handles.
func (wk *Worker) Mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeTranscode, wk.handleTranscode)
	mux.HandleFunc(queue.TypeCodecBackfill, wk.handleCodecBackfill)
	mux.HandleFunc(queue.TypeAutoCaption, wk.handleAutoCaption)
	mux.HandleFunc(queue.TypeEncrypt, wk.handleEncrypt)
	mux.HandleFunc(queue.TypeBuildSearchIndex, wk.handleBuildSearchIndex)
	mux.HandleFunc(queue.TypeGenerateAIContent, wk.handleGenerateAIContent)
	mux.HandleFunc(queue.TypePosterFrame, wk.handlePosterFrame)
	mux.HandleFunc(queue.TypeWebhookDeliver, wk.handleWebhookDeliver)
	return mux
}

func (wk *Worker) handleTranscode(ctx context.Context, t *asynq.Task) error {
	var p queue.TranscodePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		// Unrecoverable: malformed payload. SkipRetry so asynq doesn't loop.
		return fmt.Errorf("bad payload: %v: %w", err, asynq.SkipRetry)
	}

	start := time.Now()
	if err := wk.process(ctx, p); err != nil {
		// Canceled (e.g. the video was deleted mid-encode): the ffmpeg child was
		// killed via the canceled context. Don't mark failed, fire a failure
		// webhook, or let asynq retry — the video is gone on purpose.
		if ctx.Err() != nil {
			log.Printf("transcode %s canceled: %v", p.VideoID, ctx.Err())
			return nil
		}
		log.Printf("transcode %s failed: %v", p.VideoID, err)
		_ = wk.db.SetFailed(ctx, p.VideoID, err.Error())
		wk.wh.Dispatch(ctx, webhooks.Event{
			Type:      webhooks.EventVideoFailed,
			LibraryID: p.LibraryID,
			VideoID:   p.VideoID,
			Data:      map[string]any{"status": int(video.StatusFailed), "error": err.Error()},
		})
		metrics.ObserveTranscode(false, time.Since(start))
		return err // asynq retries up to MaxRetry
	}
	metrics.ObserveTranscode(true, time.Since(start))
	return nil
}

func (wk *Worker) process(ctx context.Context, p queue.TranscodePayload) error {
	workDir := filepath.Join(wk.cfg.ScratchDir, p.VideoID)
	srcPath := filepath.Join(workDir, "source")
	outDir := filepath.Join(workDir, "hls")
	// Always start clean (idempotent across retries) and clean up at the end.
	_ = os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	// If the video was deleted before we picked the job up, skip cleanly (no
	// retry) rather than transcoding something headed straight for the trash.
	// (An active job is interrupted via context cancellation; this covers the
	// queued-but-not-yet-started case.)
	if _, err := wk.db.GetVideo(ctx, p.LibraryID, p.VideoID); errors.Is(err, db.ErrNotFound) {
		log.Printf("transcode %s: video no longer exists, skipping", p.VideoID)
		return nil
	}

	if err := wk.db.SetStatus(ctx, p.VideoID, video.StatusProcessing, 0); err != nil {
		return err
	}

	// 1. Obtain the raw source. Either it's already in MinIO (uploaded), or we
	//    fetch it from a URL (migration ingest) and retain it in MinIO.
	if p.SourceURL != "" {
		if err := downloadURL(ctx, p.SourceURL, srcPath); err != nil {
			return fmt.Errorf("fetch source url: %w", err)
		}
		object := storage.RawObjectKey(p.VideoID)
		if err := wk.store.UploadFile(ctx, object, srcPath, "video/mp4"); err != nil {
			return fmt.Errorf("store fetched raw: %w", err)
		}
		if err := wk.db.SetUploaded(ctx, p.VideoID, object); err != nil {
			return err
		}
		// SetUploaded reset status to 1; put it back to processing.
		_ = wk.db.SetStatus(ctx, p.VideoID, video.StatusProcessing, 0)
	} else {
		if err := wk.store.DownloadToFile(ctx, p.SourceObject, srcPath); err != nil {
			return fmt.Errorf("download source: %w", err)
		}
	}

	// 2. Probe + load this video's encoding settings (snapshotted at creation).
	probe, err := wk.tc.Probe(ctx, srcPath)
	if err != nil {
		return err
	}
	settings, err := wk.db.GetEncodeSettings(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("load encode settings: %w", err)
	}
	wmPath, err := wk.prepareWatermark(ctx, settings, workDir)
	if err != nil {
		return fmt.Errorf("prepare watermark: %w", err)
	}
	opts := encodeOptions(settings, wmPath)

	// Pre-upload browser edit (trim/crop/rotate). nil ⇒ no edit.
	edit, err := wk.db.GetEditSpec(ctx, p.VideoID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("load edit spec: %w", err)
	}
	transcodeSrc := srcPath
	if edit.IsTemporal() { // >1 segment ⇒ materialize an edited mezzanine first
		edited := filepath.Join(workDir, "edited.mp4")
		var berr error
		if probe.HasAudio {
			_, berr = wk.tc.BuildEditedSource(ctx, srcPath, edited, edit)
		} else {
			_, berr = wk.tc.BuildEditedSourceVideoOnly(ctx, srcPath, edited, edit)
		}
		if berr != nil {
			return fmt.Errorf("apply multi-segment edit: %w", berr)
		}
		transcodeSrc = edited
		// Re-probe so duration/dims and the rung ladder reflect the cut result.
		if probe, err = wk.tc.Probe(ctx, transcodeSrc); err != nil {
			return fmt.Errorf("re-probe edited: %w", err)
		}
		opts.Edit = spatialOnly(edit) // cuts already applied; only crop/rotate remain
	} else {
		opts.Edit = edit // single-range trim (-ss/-t) + spatial, applied in one pass
		if edit.SegmentCount() == 1 { // single-range trim shortens the output
			s := edit.Segments[0]
			probe.DurationSeconds = int(s.End - s.Start)
		}
	}

	// Reported dims must reflect crop/rotate; do NOT mutate probe.Width/Height,
	// which the rung ladder in BuildHLS keys off the source frame.
	reportW, reportH := probe.Width, probe.Height
	if edit != nil {
		reportW, reportH = editedDimensions(probe.Width, probe.Height, edit)
	}

	// 3. Transcode (status transcoding while ffmpeg runs). Progress is written
	//    to the DB, throttled so we don't hammer it on every ffmpeg tick.
	if err := wk.db.SetStatus(ctx, p.VideoID, video.StatusTranscoding, 0); err != nil {
		return err
	}
	lastPct := -1
	var lastAt time.Time
	onProgress := func(pct int) {
		if pct <= lastPct {
			return
		}
		if pct < 100 && time.Since(lastAt) < 1500*time.Millisecond {
			return
		}
		lastPct, lastAt = pct, time.Now()
		_ = wk.db.SetStatus(ctx, p.VideoID, video.StatusTranscoding, pct)
	}
	result, err := wk.tc.BuildHLS(ctx, transcodeSrc, outDir, probe, opts, onProgress, "")
	if err != nil {
		return err
	}

	// 3b. Optional progressive MP4 fallback (<=1080p) for old devices / downloads.
	if settings.MP4Fallback {
		if _, err := wk.tc.BuildMP4Fallback(ctx, transcodeSrc, filepath.Join(outDir, "fallback.mp4"), probe, opts); err != nil {
			// Non-fatal: HLS is the primary deliverable. Log and continue.
			log.Printf("transcode %s: mp4 fallback failed: %v", p.VideoID, err)
		}
	}

	// 4. Upload the whole HLS tree to MinIO under hls/{id}/.
	if err := wk.store.UploadDir(ctx, outDir, storage.HLSPrefix(p.VideoID)); err != nil {
		return fmt.Errorf("upload hls: %w", err)
	}

	// 5. Mark finished with probed/encoded metadata.
	size := dirSize(outDir)
	if err := wk.db.SetFinished(ctx, p.VideoID, db.FinishInfo{
		DurationSeconds:      probe.DurationSeconds,
		Width:                reportW,
		Height:               reportH,
		SizeBytes:            size,
		AvailableResolutions: result.AvailableResolutions,
		ThumbnailFile:        result.ThumbnailFile,
		ThumbnailsVTT:        result.ThumbnailsVTT,
	}); err != nil {
		return err
	}

	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoEncoded,
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data: map[string]any{
			"status":               int(video.StatusFinished),
			"durationSeconds":      probe.DurationSeconds,
			"availableResolutions": result.AvailableResolutions,
		},
	})

	// If this video opts into extra codecs (AV1/HEVC/VP9), kick off the backfill on
	// the bulk lane. The H.264 ladder is already live; the combined master swaps in
	// when each codec is ready.
	if len(settings.ExtraCodecs()) > 0 {
		for _, codec := range settings.ExtraCodecs() {
			_ = wk.db.SetOperationStatus(ctx, p.VideoID, codecOpKind(codec), db.OpQueued, "")
		}
		if err := wk.queue.EnqueueCodecBackfill(queue.CodecBackfillPayload{VideoID: p.VideoID, LibraryID: p.LibraryID}); err != nil {
			log.Printf("enqueue codec backfill %s: %v", p.VideoID, err)
		}
	}
	return nil
}

// encodeOptions builds the transcode options from a video's encode settings.
// watermarkPath is the local path of the prepared watermark image ("" = none).
func encodeOptions(settings encoding.Config, watermarkPath string) *transcode.Options {
	opts := &transcode.Options{
		Resolutions: settings.Resolutions,
		MultiAudio:  settings.MultiAudio,
	}
	if settings.Watermark.Enabled && watermarkPath != "" {
		opts.Watermark = &transcode.WatermarkSpec{
			Path:     watermarkPath,
			Position: settings.Watermark.Position,
			Opacity:  settings.Watermark.Opacity,
			Margin:   settings.Watermark.Margin,
		}
	}
	return opts
}

// spatialOnly returns a copy of the edit with segments cleared, keeping only
// crop/rotate/flip — used after BuildEditedSource has already applied the cuts.
func spatialOnly(e *video.EditSpec) *video.EditSpec {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Segments = nil
	return &cp
}

// editedDimensions returns the output frame size after crop then rotate are
// applied to a w×h source (flip does not change dimensions). Even-rounded.
func editedDimensions(w, h int, e *video.EditSpec) (int, int) {
	if e == nil {
		return w, h
	}
	if c := e.Crop; !(c.X == 0 && c.Y == 0 && c.W == 1 && c.H == 1) {
		w = int(float64(w) * c.W)
		h = int(float64(h) * c.H)
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	w -= w % 2
	h -= h % 2
	if e.Rotate == 90 || e.Rotate == 270 {
		w, h = h, w
	}
	return w, h
}

// prepareWatermark downloads the library's watermark image into workDir when the
// config enables it, returning the local path ("" when no watermark applies).
func (wk *Worker) prepareWatermark(ctx context.Context, settings encoding.Config, workDir string) (string, error) {
	if !settings.Watermark.Enabled || settings.Watermark.Object == "" {
		return "", nil
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(workDir, "watermark.png")
	if err := wk.store.DownloadToFile(ctx, settings.Watermark.Object, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// PurgeExpiredTrash permanently removes videos that have been in the trash
// longer than the retention window (objects + DB row). Run periodically.
func (wk *Worker) PurgeExpiredTrash(ctx context.Context) {
	retention := time.Duration(wk.cfg.TrashRetentionDays) * 24 * time.Hour
	entries, err := wk.db.ListExpiredTrash(ctx, retention)
	if err != nil {
		log.Printf("trash purge: list: %v", err)
		return
	}
	for _, e := range entries {
		_ = wk.store.RemovePrefix(ctx, storage.HLSPrefix(e.ID))
		_ = wk.store.RemovePrefix(ctx, "raw/"+e.ID+"/")
		if err := wk.db.PurgeVideo(ctx, e.LibraryID, e.ID); err != nil {
			log.Printf("trash purge %s: %v", e.ID, err)
			continue
		}
		log.Printf("trash purged %s (retention expired)", e.ID)
	}
}

// PrunePlaybackEvents deletes analytics events older than the retention window.
func (wk *Worker) PrunePlaybackEvents(ctx context.Context) {
	retention := time.Duration(wk.cfg.PlaybackRetentionDays) * 24 * time.Hour
	if err := wk.db.PrunePlaybackEvents(ctx, retention); err != nil {
		log.Printf("playback prune: %v", err)
	}
}

// PruneViewerProgress deletes per-viewer progress untouched within the retention
// window. Skipped when retention is 0 (keep forever).
func (wk *Worker) PruneViewerProgress(ctx context.Context) {
	if wk.cfg.ViewerProgressRetentionDays <= 0 {
		return
	}
	retention := time.Duration(wk.cfg.ViewerProgressRetentionDays) * 24 * time.Hour
	if err := wk.db.PruneViewerProgress(ctx, retention); err != nil {
		log.Printf("viewer progress prune: %v", err)
	}
}

// downloadURL streams a remote file to a local path. No explicit timeout: large
// migration sources can take a while, and the asynq task context bounds it.
func downloadURL(ctx context.Context, url, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("source url returned %d", resp.StatusCode)
	}
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
