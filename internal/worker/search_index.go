package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/search"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// handleBuildSearchIndex parses a caption VTT into timestamped chunks, embeds
// them through the library's configured provider, and stores the in-video search
// index (replacing any prior index for that track). Bulk lane.
func (wk *Worker) handleBuildSearchIndex(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.BuildSearchIndexPayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad search-index payload: %v: %w", err, asynq.SkipRetry)
	}
	fin := wk.trackOp(ctx, p.VideoID, db.OpKindSearchIndex)
	defer func() { fin(err) }()

	cfg, err := wk.db.GetSearchConfig(ctx, p.LibraryID)
	if err != nil {
		return fmt.Errorf("search index: load config: %w", err)
	}
	if !cfg.Enabled {
		return fmt.Errorf("search disabled for library %s: %w", p.LibraryID, asynq.SkipRetry)
	}

	// Resolve the caption track to index.
	caps, err := wk.db.ListCaptions(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("search index: list captions: %w", err)
	}
	if len(caps) == 0 {
		return fmt.Errorf("search index: video %s has no captions: %w", p.VideoID, asynq.SkipRetry)
	}
	lang, object := p.Lang, ""
	for _, c := range caps {
		if p.Lang == "" || c.Lang == p.Lang {
			lang, object = c.Lang, c.Object
			break
		}
	}
	if object == "" {
		return fmt.Errorf("search index: no caption for lang %q on %s: %w", p.Lang, p.VideoID, asynq.SkipRetry)
	}

	// Parse + chunk the transcript.
	vtt, err := wk.store.GetBytes(ctx, object)
	if err != nil {
		return fmt.Errorf("search index: fetch vtt: %w", err)
	}
	chunks := search.ParseAndChunk(vtt, cfg.ChunkSeconds)
	if len(chunks) == 0 {
		// Nothing to index (empty/odd transcript); clear any stale index and stop.
		_ = wk.db.ReplaceSearchChunks(ctx, p.LibraryID, p.VideoID, lang, nil, nil, cfg.Provider, cfg.Model)
		return nil
	}

	// Embed via the configured provider.
	embedder, err := search.NewEmbedder(cfg, wk.cfg.WhisperURL)
	if err != nil {
		return fmt.Errorf("search index: embedder: %v: %w", err, asynq.SkipRetry)
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("search index: embed: %w", err)
	}

	if err := wk.db.ReplaceSearchChunks(ctx, p.LibraryID, p.VideoID, lang, chunks, vectors, embedder.Provider(), embedder.Model()); err != nil {
		return fmt.Errorf("search index: store: %w", err)
	}

	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoIndexed,
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data:      map[string]any{"lang": lang, "chunks": len(chunks), "provider": embedder.Provider()},
	})
	return nil
}
