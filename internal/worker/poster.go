package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
)

// handlePosterFrame grabs a single frame from the raw source at the requested
// timestamp and publishes it as the video's custom poster. The API can't do this
// itself (its image ships without ffmpeg); the worker has ffmpeg + the source.
// On success it points videos.thumbnail_file at the new object, so the next
// signed /play URL serves it.
func (wk *Worker) handlePosterFrame(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.PosterFramePayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad poster payload: %v: %w", err, asynq.SkipRetry)
	}
	fin := wk.trackOp(ctx, p.VideoID, db.OpKindPoster)
	defer func() { fin(err) }()

	workDir := filepath.Join(wk.cfg.ScratchDir, p.VideoID+"-poster")
	srcPath := filepath.Join(workDir, "source")
	outPath := filepath.Join(workDir, "poster.jpg")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	// The raw source is retained after transcode (codec backfills reuse it), so
	// it's available for the frame grab.
	if err := wk.store.DownloadToFile(ctx, storage.RawObjectKey(p.VideoID), srcPath); err != nil {
		return fmt.Errorf("poster: download source: %w", err)
	}
	if err := wk.tc.PosterAt(ctx, srcPath, outPath, p.AtSeconds); err != nil {
		return fmt.Errorf("poster: extract frame: %w", err)
	}

	object := storage.HLSPrefix(p.VideoID) + p.Object
	if err := wk.store.UploadFile(ctx, object, outPath, "image/jpeg"); err != nil {
		return fmt.Errorf("poster: upload: %w", err)
	}
	if err := wk.db.SetThumbnail(ctx, p.LibraryID, p.VideoID, p.Object); err != nil {
		return fmt.Errorf("poster: set thumbnail: %w", err)
	}
	return nil
}
