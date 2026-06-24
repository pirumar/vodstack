package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/transcode"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// codecOpKind maps a backfill codec to its video_operations kind.
func codecOpKind(codec string) string {
	switch codec {
	case "hevc":
		return db.OpKindHEVC
	case "vp9":
		return db.OpKindVP9
	default:
		return db.OpKindAV1
	}
}

// handleCodecBackfill encodes every extra codec (AV1/HEVC/VP9) listed in the
// video's encode_settings that is not already published, then rebuilds the
// combined master.m3u8 that lists the H.264 ladder plus each extra codec. Players
// that can decode a richer codec pick it; everyone else keeps getting H.264. One
// job per video keeps the master rebuild sequential (race-free). Bulk lane only.
func (wk *Worker) handleCodecBackfill(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.CodecBackfillPayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad codec backfill payload: %v: %w", err, asynq.SkipRetry)
	}

	settings, err := wk.db.GetEncodeSettings(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("codec backfill: load settings: %w", err)
	}
	codecs := settings.ExtraCodecs()
	if len(codecs) == 0 {
		return nil // nothing to do
	}

	workDir := filepath.Join(wk.cfg.ScratchDir, p.VideoID+"-codec")
	srcPath := filepath.Join(workDir, "source")
	_ = os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	// Fetch the retained raw source once for all codecs.
	if err := wk.store.DownloadToFile(ctx, storage.RawObjectKey(p.VideoID), srcPath); err != nil {
		return fmt.Errorf("codec backfill: download source: %w", err)
	}
	probe, err := wk.tc.Probe(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("codec backfill: probe: %w", err)
	}
	// Reapply the watermark for visual parity with the H.264 ladder.
	wmPath, err := wk.prepareWatermark(ctx, settings, workDir)
	if err != nil {
		return fmt.Errorf("codec backfill: watermark: %w", err)
	}
	opts := encodeOptions(settings, wmPath)

	// Apply the same pre-upload edit the H.264 ladder used, so extra-codec
	// renditions share identical geometry & timing.
	edit, err := wk.db.GetEditSpec(ctx, p.VideoID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("load edit spec: %w", err)
	}
	codecSrc := srcPath
	if edit.IsTemporal() {
		edited := filepath.Join(workDir, "edited.mp4")
		var berr error
		if probe.HasAudio {
			_, berr = wk.tc.BuildEditedSource(ctx, srcPath, edited, edit)
		} else {
			_, berr = wk.tc.BuildEditedSourceVideoOnly(ctx, srcPath, edited, edit)
		}
		if berr != nil {
			return fmt.Errorf("apply multi-segment edit (backfill): %w", berr)
		}
		codecSrc = edited
		if probe, err = wk.tc.Probe(ctx, codecSrc); err != nil {
			return fmt.Errorf("re-probe edited (backfill): %w", err)
		}
		opts.Edit = spatialOnly(edit)
	} else {
		opts.Edit = edit
	}

	hlsPrefix := storage.HLSPrefix(p.VideoID)

	// Capture the pristine H.264 master once (before the first swap) so every
	// rebuild starts from the pure H.264 variants rather than an already-merged one.
	if _, statErr := wk.store.GetBytes(ctx, hlsPrefix+"master_h264.m3u8"); statErr != nil {
		if cur, e := wk.store.GetBytes(ctx, hlsPrefix+"master.m3u8"); e == nil {
			_ = wk.store.PutBytes(ctx, hlsPrefix+"master_h264.m3u8", cur, "application/vnd.apple.mpegurl")
		}
	}

	// Encode each requested codec that is not already published.
	for _, codec := range codecs {
		codecMaster := hlsPrefix + codec + "/master_" + codec + ".m3u8"
		if _, e := wk.store.GetBytes(ctx, codecMaster); e == nil {
			continue // already present
		}
		if err := wk.encodeOneCodec(ctx, p, codec, codecSrc, workDir, probe, opts); err != nil {
			return err
		}
	}

	// Rebuild the combined master from the pristine H.264 master + every present
	// codec ladder, then publish it atomically.
	resCSV := strings.Join(settings.Resolutions, ",")
	if err := wk.rebuildCombinedMaster(ctx, p.VideoID, codecs, resCSV, probe.HasAudio); err != nil {
		return fmt.Errorf("codec backfill: rebuild master: %w", err)
	}

	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoAV1Ready, // generic "extra codec(s) ready" signal
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data:      map[string]any{"codecs": codecs},
	})
	return nil
}

// encodeOneCodec encodes one codec ladder, uploads it under hls/<id>/<codec>/, and
// records its operation status.
func (wk *Worker) encodeOneCodec(ctx context.Context, p queue.CodecBackfillPayload, codec, srcPath, workDir string, probe *transcode.Probe, opts *transcode.Options) (err error) {
	fin := wk.trackOp(ctx, p.VideoID, codecOpKind(codec))
	defer func() { fin(err) }()

	codecDir := filepath.Join(workDir, codec)
	res, err := wk.tc.BuildCodec(ctx, codec, srcPath, codecDir, probe, opts, nil)
	if err != nil {
		return fmt.Errorf("codec backfill: encode %s: %w", codec, err)
	}
	if err := wk.store.UploadDir(ctx, codecDir, storage.HLSPrefix(p.VideoID)+codec+"/"); err != nil {
		return fmt.Errorf("codec backfill: upload %s: %w", codec, err)
	}
	// av1_resolutions has a dedicated column for back-compat; other codecs are
	// reflected only in the combined master (the source of truth for playback).
	if codec == "av1" {
		_ = wk.db.SetAV1Resolutions(ctx, p.VideoID, res)
	}
	return nil
}

// rebuildCombinedMaster folds every present codec ladder into the pristine H.264
// master and publishes the result as master.m3u8. resCSV sizes the injected
// CODECS level to the tallest enabled resolution.
func (wk *Worker) rebuildCombinedMaster(ctx context.Context, videoID string, codecs []string, resCSV string, hasAudio bool) error {
	hlsPrefix := storage.HLSPrefix(videoID)
	combined, err := wk.store.GetBytes(ctx, hlsPrefix+"master_h264.m3u8")
	if err != nil {
		// No pristine copy (e.g. older video): fall back to the current master.
		combined, err = wk.store.GetBytes(ctx, hlsPrefix+"master.m3u8")
		if err != nil {
			return fmt.Errorf("read base master: %w", err)
		}
	}
	for _, codec := range codecs {
		codecMaster, e := wk.store.GetBytes(ctx, hlsPrefix+codec+"/master_"+codec+".m3u8")
		if e != nil {
			continue // not published (encode skipped/failed) — leave it out
		}
		tag := transcode.CodecsTag(codec, resCSV)
		if hasAudio {
			tag += ",mp4a.40.2"
		}
		combined = transcode.MergeMasters(combined, codecMaster, codec+"/", tag)
	}
	return wk.store.PutBytes(ctx, hlsPrefix+"master.m3u8", combined, "application/vnd.apple.mpegurl")
}
