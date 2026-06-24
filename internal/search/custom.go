package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// customEmbedder targets any OpenAI-compatible embeddings endpoint (OpenWebUI,
// LiteLLM, Ollama proxy, …): POST {"model","input":[...]} with a Bearer key, read
// back {"data":[{"embedding":[...],"index":N}]}. The configured model must emit
// EmbedDim-length vectors (checked at embed time).
type customEmbedder struct {
	baseURL string
	apiKey  string
	model   string
}

func (e *customEmbedder) Provider() string { return ProviderCustom }
func (e *customEmbedder) Model() string    { return e.model }

func (e *customEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := batchEmbed(ctx, texts, 64, e.embedBatch)
	if err != nil {
		return nil, err
	}
	if err := checkDims(vecs, len(texts)); err != nil {
		return nil, err
	}
	return vecs, nil
}

func (e *customEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := embedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("custom embed returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode custom response: %w", err)
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		// Honor the index field when present; otherwise fall back to response order.
		pos := d.Index
		if pos < 0 || pos >= len(vecs) {
			pos = i
		}
		vecs[pos] = d.Embedding
	}
	return vecs, nil
}
