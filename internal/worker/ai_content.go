package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/llm"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/search"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/video"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// handleGenerateAIContent feeds a video's transcript to the library's configured
// LLM router and stores a description, tags, and/or chapters. Bulk lane.
func (wk *Worker) handleGenerateAIContent(ctx context.Context, t *asynq.Task) (err error) {
	var p queue.GenerateAIContentPayload
	if err = json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad ai-content payload: %v: %w", err, asynq.SkipRetry)
	}
	fin := wk.trackOp(ctx, p.VideoID, db.OpKindAIContent)
	defer func() { fin(err) }()
	kinds := normalizeKinds(p.Kinds)

	cfg, err := wk.db.GetLLMConfig(ctx, p.LibraryID)
	if err != nil {
		return fmt.Errorf("ai-content: load llm config: %w", err)
	}
	if !cfg.Ready() {
		return fmt.Errorf("llm not configured for %s: %w", p.LibraryID, asynq.SkipRetry)
	}
	client, err := llm.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("ai-content: client: %v: %w", err, asynq.SkipRetry)
	}

	// Resolve the caption track + parse it.
	caps, err := wk.db.ListCaptions(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("ai-content: list captions: %w", err)
	}
	if len(caps) == 0 {
		return fmt.Errorf("ai-content: video %s has no captions: %w", p.VideoID, asynq.SkipRetry)
	}
	object := ""
	for _, c := range caps {
		if p.Lang == "" || c.Lang == p.Lang {
			object = c.Object
			break
		}
	}
	if object == "" {
		return fmt.Errorf("ai-content: no caption for lang %q: %w", p.Lang, asynq.SkipRetry)
	}
	vtt, err := wk.store.GetBytes(ctx, object)
	if err != nil {
		return fmt.Errorf("ai-content: fetch vtt: %w", err)
	}
	cues := search.ParseCues(vtt)
	if len(cues) == 0 {
		return fmt.Errorf("ai-content: empty transcript: %w", asynq.SkipRetry)
	}
	transcript := cuesText(cues)

	v, err := wk.db.GetVideo(ctx, p.LibraryID, p.VideoID)
	if err != nil {
		return fmt.Errorf("ai-content: get video: %w", err)
	}
	duration := 0
	if v.DurationSeconds != nil {
		duration = *v.DurationSeconds
	}

	produced := make(map[string]bool)
	if kinds["summary"] {
		desc, err := client.Summary(ctx, transcript)
		if err != nil {
			return fmt.Errorf("ai-content: summary: %w", err)
		}
		if err := wk.db.SetDescription(ctx, p.LibraryID, p.VideoID, desc); err != nil {
			return fmt.Errorf("ai-content: store description: %w", err)
		}
		produced["summary"] = true
	}
	if kinds["tags"] {
		tags, err := client.Tags(ctx, transcript)
		if err != nil {
			return fmt.Errorf("ai-content: tags: %w", err)
		}
		if err := wk.db.SetTags(ctx, p.LibraryID, p.VideoID, tags); err != nil {
			return fmt.Errorf("ai-content: store tags: %w", err)
		}
		produced["tags"] = true
	}
	if kinds["chapters"] {
		chapters, err := client.Chapters(ctx, cues, duration)
		if err != nil {
			return fmt.Errorf("ai-content: chapters: %w", err)
		}
		if len(chapters) > 0 {
			raw, _ := json.Marshal(chapters)
			if err := wk.db.SetChapters(ctx, p.LibraryID, p.VideoID, raw); err != nil {
				return fmt.Errorf("ai-content: store chapters: %w", err)
			}
			vttBytes := []byte(video.ChaptersVTT(chapters, duration))
			obj := storage.HLSPrefix(p.VideoID) + "chapters.vtt"
			if err := wk.store.PutBytes(ctx, obj, vttBytes, "text/vtt"); err != nil {
				return fmt.Errorf("ai-content: store chapters.vtt: %w", err)
			}
			produced["chapters"] = true
		}
	}

	done := make([]string, 0, len(produced))
	for k := range produced {
		done = append(done, k)
	}
	wk.wh.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoEnriched,
		LibraryID: p.LibraryID,
		VideoID:   p.VideoID,
		Data:      map[string]any{"kinds": done},
	})
	return nil
}

// normalizeKinds returns the requested kinds as a set; empty input means all.
func normalizeKinds(in []string) map[string]bool {
	allowed := map[string]bool{"summary": true, "tags": true, "chapters": true}
	out := map[string]bool{}
	for _, k := range in {
		k = strings.ToLower(strings.TrimSpace(k))
		if allowed[k] {
			out[k] = true
		}
	}
	if len(out) == 0 {
		return allowed
	}
	return out
}

func cuesText(cues []search.Cue) string {
	parts := make([]string, len(cues))
	for i, c := range cues {
		parts[i] = c.Text
	}
	return strings.Join(parts, " ")
}
