package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient is shared; LLM generation over a transcript can take a while.
var httpClient = &http.Client{Timeout: 4 * time.Minute}

// Client calls an OpenAI-compatible chat-completions endpoint.
type Client struct {
	cfg Config
}

// NewClient builds a client from a (normalized) config. Returns an error if the
// config is not ready (disabled / missing URL or model).
func NewClient(cfg Config) (*Client, error) {
	cfg.Normalize()
	if !cfg.Ready() {
		return nil, fmt.Errorf("llm not configured (enabled+baseUrl+model required)")
	}
	return &Client{cfg: cfg}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat sends a system + user message and returns the assistant's text content.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	reqBody := map[string]any{
		"model": c.cfg.Model,
		"messages": []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"temperature": c.cfg.Temperature,
		"max_tokens":  c.cfg.MaxTokens,
		"stream":      false,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm returned %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode llm response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}
