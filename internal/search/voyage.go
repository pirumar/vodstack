package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// voyageEmbedder uses the Voyage AI embeddings API, requesting
// output_dimension=EmbedDim so vectors match the column.
type voyageEmbedder struct {
	apiKey string
	model  string
}

func (e *voyageEmbedder) Provider() string { return ProviderVoyage }
func (e *voyageEmbedder) Model() string    { return e.model }

func (e *voyageEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := batchEmbed(ctx, texts, 128, e.embedBatch)
	if err != nil {
		return nil, err
	}
	if err := checkDims(vecs, len(texts)); err != nil {
		return nil, err
	}
	return vecs, nil
}

func (e *voyageEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"input":            texts,
		"model":            e.model,
		"output_dimension": EmbedDim,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := embedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voyage embed returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode voyage response: %w", err)
	}
	vecs := make([][]float32, len(out.Data))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("voyage returned out-of-range index %d", d.Index)
		}
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}
