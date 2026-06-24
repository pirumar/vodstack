package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// geminiEmbedder uses Google's Generative Language API (batchEmbedContents),
// requesting outputDimensionality=EmbedDim so vectors match the column.
type geminiEmbedder struct {
	apiKey string
	model  string
}

func (e *geminiEmbedder) Provider() string { return ProviderGemini }
func (e *geminiEmbedder) Model() string    { return e.model }

func (e *geminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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

func (e *geminiEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	modelPath := e.model
	if len(modelPath) < 7 || modelPath[:7] != "models/" {
		modelPath = "models/" + modelPath
	}
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type embedReq struct {
		Model                string  `json:"model"`
		Content              content `json:"content"`
		OutputDimensionality int     `json:"outputDimensionality"`
	}
	reqBody := struct {
		Requests []embedReq `json:"requests"`
	}{}
	for _, t := range texts {
		reqBody.Requests = append(reqBody.Requests, embedReq{
			Model:                modelPath,
			Content:              content{Parts: []part{{Text: t}}},
			OutputDimensionality: EmbedDim,
		})
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s:batchEmbedContents?key=%s", modelPath, e.apiKey)
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
		return nil, fmt.Errorf("gemini embed returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	vecs := make([][]float32, len(out.Embeddings))
	for i, em := range out.Embeddings {
		vecs[i] = em.Values
	}
	return vecs, nil
}
