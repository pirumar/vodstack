// Package search powers in-video search: it parses caption VTTs into timestamped
// chunks, embeds them through a pluggable provider (local CPU model, Gemini, or
// Voyage), and the DB layer fuses a vector (pgvector) and lexical (pg_trgm) scan.
//
// Embeddings are fixed at EmbedDim dimensions for every provider so the pgvector
// column and ANN index never change shape. Providers are asked to emit exactly
// EmbedDim; one that can't is rejected at embed time.
package search

import "strings"

// EmbedDim is the fixed embedding dimension stored in Postgres (vector(1024)).
// Every provider must produce vectors of this length.
const EmbedDim = 1024

// Provider identifiers (the value stored in search_config.provider).
const (
	ProviderLocal  = "local"
	ProviderGemini = "gemini"
	ProviderVoyage = "voyage"
	// ProviderCustom is any OpenAI-compatible embeddings endpoint (e.g. OpenWebUI,
	// LiteLLM, a local Ollama proxy). Configured with a full BaseURL + model + key.
	ProviderCustom = "custom"
)

// Default model per provider, all configured to emit EmbedDim.
const (
	defaultLocalModel  = "BAAI/bge-m3"          // 1024-dim native, strong multilingual (Turkish)
	defaultGeminiModel = "gemini-embedding-001" // outputDimensionality=1024
	defaultVoyageModel = "voyage-3.5"           // output_dimension=1024
)

const (
	defaultChunkSeconds = 30
	minChunkSeconds     = 10
	maxChunkSeconds     = 120
)

// Config is a library's in-video-search settings, persisted as the
// libraries.search_config JSONB (mirrors the player.Config pattern). The zero
// value is not meaningful; load through DefaultConfig + Normalize.
type Config struct {
	Enabled      bool   `json:"enabled"`      // master switch (gates indexing + query)
	Provider     string `json:"provider"`     // local | gemini | voyage | custom
	Model        string `json:"model"`        // provider-specific model id
	APIKey       string `json:"apiKey"`       // for gemini/voyage/custom; masked when surfaced
	BaseURL      string `json:"baseUrl"`      // custom provider: full OpenAI-compatible /embeddings URL
	ChunkSeconds int    `json:"chunkSeconds"` // transcript window length
	ShowInPlayer bool   `json:"showInPlayer"` // expose the in-player search box on the embed
}

// DefaultConfig returns the safe out-of-the-box setup: disabled, local CPU
// embedder (no external dependency, no key), 30s windows.
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		Provider:     ProviderLocal,
		Model:        defaultLocalModel,
		APIKey:       "",
		ChunkSeconds: defaultChunkSeconds,
		ShowInPlayer: false,
	}
}

// DefaultModel returns the default model id for a (normalized) provider.
func DefaultModel(provider string) string {
	switch provider {
	case ProviderGemini:
		return defaultGeminiModel
	case ProviderVoyage:
		return defaultVoyageModel
	default:
		return defaultLocalModel
	}
}

// Normalize validates and clamps a config in place, falling back to defaults for
// any invalid field. Applied both on load and before save.
func (c *Config) Normalize() {
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	switch c.Provider {
	case ProviderLocal, ProviderGemini, ProviderVoyage, ProviderCustom:
	default:
		c.Provider = ProviderLocal
	}
	// For custom the model is endpoint-specific (no sane default), so keep whatever
	// was given; for the known providers fall back to their default model.
	if strings.TrimSpace(c.Model) == "" && c.Provider != ProviderCustom {
		c.Model = DefaultModel(c.Provider)
	} else {
		c.Model = strings.TrimSpace(c.Model)
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if c.ChunkSeconds < minChunkSeconds {
		c.ChunkSeconds = defaultChunkSeconds
	} else if c.ChunkSeconds > maxChunkSeconds {
		c.ChunkSeconds = maxChunkSeconds
	}
}
