package search

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Embedder turns texts into fixed-dimension vectors. The same Embedder is used
// at index time (worker, many chunks) and query time (API, one string), built
// from the library's stored Config so both sides agree on provider + model.
type Embedder interface {
	// Embed returns one EmbedDim-length vector per input text, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Provider() string
	Model() string
}

// embedHTTPClient is shared by the HTTP-backed providers. CPU embedding of a
// whole transcript (local) or a large remote batch can take a while.
var embedHTTPClient = &http.Client{Timeout: 10 * time.Minute}

// NewEmbedder builds the embedder described by cfg. whisperURL is the local
// sidecar base (only needed for the local provider). Returns an error when the
// configuration is incomplete (e.g. a remote provider without an API key).
func NewEmbedder(cfg Config, whisperURL string) (Embedder, error) {
	cfg.Normalize()
	switch cfg.Provider {
	case ProviderLocal:
		if whisperURL == "" {
			return nil, fmt.Errorf("local embedder needs WHISPER_URL")
		}
		return &localEmbedder{baseURL: whisperURL, model: cfg.Model}, nil
	case ProviderGemini:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("gemini embedder needs an API key")
		}
		return &geminiEmbedder{apiKey: cfg.APIKey, model: cfg.Model}, nil
	case ProviderVoyage:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("voyage embedder needs an API key")
		}
		return &voyageEmbedder{apiKey: cfg.APIKey, model: cfg.Model}, nil
	case ProviderCustom:
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("custom embedder needs a base URL")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("custom embedder needs a model")
		}
		return &customEmbedder{baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model}, nil
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", cfg.Provider)
	}
}

// batchEmbed splits texts into batches of batchSize and concatenates the
// per-batch results, preserving order.
func batchEmbed(ctx context.Context, texts []string, batchSize int, fn func(context.Context, []string) ([][]float32, error)) ([][]float32, error) {
	if batchSize <= 0 {
		batchSize = len(texts)
	}
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := fn(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// checkDims verifies the provider returned the expected count of EmbedDim vectors.
func checkDims(vecs [][]float32, want int) error {
	if len(vecs) != want {
		return fmt.Errorf("embedder returned %d vectors, want %d", len(vecs), want)
	}
	for i, v := range vecs {
		if len(v) != EmbedDim {
			return fmt.Errorf("embedding %d has dim %d, want %d (model misconfigured?)", i, len(v), EmbedDim)
		}
	}
	return nil
}
