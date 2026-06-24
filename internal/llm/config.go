// Package llm generates content from a video transcript (description/summary,
// tags, chapters) via an OpenAI-compatible /chat/completions endpoint configured
// per library — any router works (OpenRouter, OpenWebUI, LiteLLM, vLLM, …). It
// mirrors the internal/search provider pattern: a JSONB config on the library,
// a thin HTTP client, and prompt builders that parse the model's output.
package llm

import "strings"

const (
	defaultTemperature = 0.3
	defaultMaxTokens   = 1024
	maxTemperature     = 2.0
)

// Config is a library's LLM router settings, persisted as libraries.llm_config
// JSONB. The zero value is not meaningful; load through DefaultConfig + Normalize.
type Config struct {
	Enabled     bool    `json:"enabled"`     // master switch
	BaseURL     string  `json:"baseUrl"`     // full OpenAI-compatible /chat/completions URL
	Model       string  `json:"model"`       // model id
	APIKey      string  `json:"apiKey"`      // bearer key; masked when surfaced
	Temperature float64 `json:"temperature"` // sampling temperature
	MaxTokens   int     `json:"maxTokens"`   // response cap
}

// DefaultConfig returns the safe disabled default.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		BaseURL:     "",
		Model:       "",
		APIKey:      "",
		Temperature: defaultTemperature,
		MaxTokens:   defaultMaxTokens,
	}
}

// Normalize validates and clamps a config in place. Applied on load and before save.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Model = strings.TrimSpace(c.Model)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.Temperature < 0 || c.Temperature > maxTemperature {
		c.Temperature = defaultTemperature
	}
	if c.MaxTokens < 64 || c.MaxTokens > 32000 {
		c.MaxTokens = defaultMaxTokens
	}
}

// Ready reports whether the config can actually run a request.
func (c Config) Ready() bool {
	return c.Enabled && c.BaseURL != "" && c.Model != ""
}
