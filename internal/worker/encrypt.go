package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// handleEncrypt re-encodes a video's ladder with AES-128 and stores the key. The
// playlist's #EXT-X-KEY URI points at the API key endpoint; the segments are
// ciphertext at rest and the key is only served to holders of a valid playback
// token. Bulk lane. The poster stays (regenerated from source); seek thumbnails
// are skipped for encrypted ladders (kept from the original transcode in MinIO).
func (wk *Worker) handleEncrypt(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.EncryptPayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad encrypt payload: %v: %w", err, asynq.SkipRetry)
	}
	fin := wk.trackOp(ctx, p.VideoID, db.OpKindEncrypt)
	defer func() { fin(err) }()
	if wk.cfg.KeyBaseURL == "" {
		return fmt.Errorf("encryption disabled (no KEY_BASE_URL): %w", asynq.SkipRetry)
	}

	workDir := filepath.Join(wk.cfg.ScratchDir, p.VideoID+"-enc")
	srcPath := filepath.Join(workDir, "source")
	outDir := filepath.Join(workDir, "hls")
	keyPath := filepath.Join(workDir, "enc.key")
	keyInfoPath := filepath.Join(workDir, "enc.keyinfo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	// 1. Generate a 16-byte key + IV.
	key := make([]byte, 16)
	iv := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if _, err := rand.Read(iv); err != nil {
		return err
	}
	keyID := uuid.NewString()
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return err
	}
	// keyinfo: <key URI written into the playlist> / <local key file> / <IV hex>
	keyURI := strings.TrimRight(wk.cfg.KeyBaseURL, "/") + "/keys/" + p.LibraryID + "/" + p.VideoID
	keyInfo := keyURI + "\n" + keyPath + "\n" + hex.EncodeToString(iv) + "\n"
	if err := os.WriteFile(keyInfoPath, []byte(keyInfo), 0o600); err != nil {
		return err
	}

	// 2. Fetch raw source + re-encode encrypted.
	if err := wk.store.DownloadToFile(ctx, storage.RawObjectKey(p.VideoID), srcPath); err != nil {
		return fmt.Errorf("encrypt: download source: %w", err)
	}
	probe, err := wk.tc.Probe(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("encrypt: probe: %w", err)
	}
	// Re-encode with the same resolutions/watermark/multi-audio the video was
	// originally built with so the encrypted ladder matches.
	settings, err := wk.db.GetEncodeSettings(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("encrypt: load settings: %w", err)
	}
	wmPath, err := wk.prepareWatermark(ctx, settings, workDir)
	if err != nil {
		return fmt.Errorf("encrypt: watermark: %w", err)
	}
	opts := encodeOptions(settings, wmPath)
	if _, err := wk.tc.BuildHLS(ctx, srcPath, outDir, probe, opts, nil, keyInfoPath); err != nil {
		return fmt.Errorf("encrypt: encode: %w", err)
	}

	// 3. Publish the encrypted ladder (replaces master/variants/segments).
	if err := wk.store.UploadDir(ctx, outDir, storage.HLSPrefix(p.VideoID)); err != nil {
		return fmt.Errorf("encrypt: upload: %w", err)
	}

	// 4. Persist the key (also flips videos.encryption_mode='aes128').
	if err := wk.db.SaveContentKey(ctx, keyID, p.LibraryID, p.VideoID,
		hex.EncodeToString(key), hex.EncodeToString(iv)); err != nil {
		return fmt.Errorf("encrypt: save key: %w", err)
	}

	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoEncrypted,
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data:      map[string]any{"encryptionMode": "aes128"},
	})
	return nil
}
