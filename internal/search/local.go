package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// localEmbedder calls the whisper sidecar's /embed endpoint, which runs a
// sentence-transformers model on CPU. No external dependency, no API key.
type localEmbedder struct {
	baseURL string
	model   string
}

func (e *localEmbedder) Provider() string { return ProviderLocal }
func (e *localEmbedder) Model() string    { return e.model }

func (e *localEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	// The sidecar holds one model in memory; send everything in a single request
	// (it batches internally).
	vecs, err := batchEmbed(ctx, texts, 256, e.embedBatch)
	if err != nil {
		return nil, err
	}
	if err := checkDims(vecs, len(texts)); err != nil {
		return nil, err
	}
	return vecs, nil
}

func (e *localEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"texts": texts})
	url := strings.TrimRight(e.baseURL, "/") + "/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := embedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whisper /embed returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	return out.Embeddings, nil
}
