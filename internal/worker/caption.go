package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/webhooks"
)

var captionClient = &http.Client{Timeout: 30 * time.Minute} // ASR can be slow on CPU

// handleAutoCaption extracts the audio, runs ASR via the faster-whisper sidecar,
// and stores the resulting WebVTT through the SAME captions path the admin upload
// uses — so it shows up in the player with zero extra plumbing. Bulk lane.
func (wk *Worker) handleAutoCaption(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.AutoCaptionPayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad caption payload: %v: %w", err, asynq.SkipRetry)
	}
	fin := wk.trackOp(ctx, p.VideoID, db.OpKindCaption)
	defer func() { fin(err) }()
	if wk.cfg.WhisperURL == "" {
		return fmt.Errorf("auto-caption disabled (no WHISPER_URL): %w", asynq.SkipRetry)
	}

	workDir := filepath.Join(wk.cfg.ScratchDir, p.VideoID+"-caption")
	srcPath := filepath.Join(workDir, "source")
	wavPath := filepath.Join(workDir, "audio.wav")
	_ = os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	// 1. Fetch raw source + extract audio.
	if err := wk.store.DownloadToFile(ctx, storage.RawObjectKey(p.VideoID), srcPath); err != nil {
		return fmt.Errorf("caption: download source: %w", err)
	}
	if err := wk.tc.ExtractAudioWAV(ctx, srcPath, wavPath); err != nil {
		return fmt.Errorf("caption: extract audio: %w", err)
	}

	// 2. Run ASR.
	vtt, detected, err := wk.transcribe(ctx, wavPath, p.Lang)
	if err != nil {
		return fmt.Errorf("caption: asr: %w", err)
	}
	lang := p.Lang
	if lang == "" {
		lang = detected
	}
	if lang == "" {
		lang = "und"
	}

	// 3. Store via the existing captions path.
	object := storage.HLSPrefix(p.VideoID) + "captions/" + lang + ".vtt"
	if err := wk.store.PutBytes(ctx, object, vtt, "text/vtt"); err != nil {
		return fmt.Errorf("caption: store vtt: %w", err)
	}
	if err := wk.db.AddCaption(ctx, uuid.NewString(), p.VideoID, lang, strings.ToUpper(lang)+" (auto)", object); err != nil {
		return fmt.Errorf("caption: record: %w", err)
	}

	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoCaptioned,
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data:      map[string]any{"lang": lang, "auto": true},
	})

	// Chain: if in-video search is enabled, build the index from this fresh
	// transcript. Best-effort — a caption is still useful on its own.
	if cfg, err := wk.db.GetSearchConfig(ctx, p.LibraryID); err == nil && cfg.Enabled {
		if err := wk.queue.EnqueueBuildSearchIndex(queue.BuildSearchIndexPayload{
			VideoID: p.VideoID, LibraryID: p.LibraryID, Lang: lang,
		}); err != nil {
			log.Printf("enqueue search index %s: %v", p.VideoID, err)
		}
	}

	// Chain: if an LLM router is configured, generate description/tags/chapters
	// from the transcript. Best-effort.
	if cfg, err := wk.db.GetLLMConfig(ctx, p.LibraryID); err == nil && cfg.Ready() {
		if err := wk.queue.EnqueueGenerateAIContent(queue.GenerateAIContentPayload{
			VideoID: p.VideoID, LibraryID: p.LibraryID, Lang: lang,
		}); err != nil {
			log.Printf("enqueue ai content %s: %v", p.VideoID, err)
		}
	}
	return nil
}

// transcribe POSTs the WAV to the sidecar and returns the VTT + detected language.
func (wk *Worker) transcribe(ctx context.Context, wavPath, lang string) ([]byte, string, error) {
	f, err := os.Open(wavPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	url := strings.TrimRight(wk.cfg.WhisperURL, "/") + "/transcribe"
	if lang != "" {
		url += "?lang=" + lang
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, f)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "audio/wav")

	resp, err := captionClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("whisper returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return body, resp.Header.Get("X-Detected-Language"), nil
}
