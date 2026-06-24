package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/search"
)

// embedQuery embeds a single query string with the library's configured provider.
// It returns nil on any failure so the caller falls back to lexical-only search
// rather than erroring the request.
func (s *Server) embedQuery(ctx context.Context, cfg search.Config, q string) []float32 {
	emb, err := search.NewEmbedder(cfg, s.cfg.WhisperURL)
	if err != nil {
		return nil
	}
	vecs, err := emb.Embed(ctx, []string{q})
	if err != nil || len(vecs) != 1 {
		return nil
	}
	return vecs[0]
}

// runSearch performs the hybrid search for one library and writes the JSON
// result envelope. Shared by the admin and public-API endpoints.
func (s *Server) runSearch(w http.ResponseWriter, r *http.Request, libraryID, q, videoID string) {
	cfg, err := s.db.GetSearchConfig(r.Context(), libraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config lookup failed")
		return
	}
	if !cfg.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "results": []db.SearchHit{}})
		return
	}
	vec := s.embedQuery(r.Context(), cfg, q)
	hits, err := s.db.SearchChunks(r.Context(), libraryID, vec, q, videoID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "results": hits})
}

// --- Admin BFF ---

// adminSearch searches the admin library's transcripts. ?q is the query; optional
// ?videoId restricts to one video.
func (s *Server) adminSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "results": []db.SearchHit{}})
		return
	}
	s.runSearch(w, r, s.cfg.AdminLibraryID, q, strings.TrimSpace(r.URL.Query().Get("videoId")))
}

// searchSettingsView is the wire shape for search settings — the API key is never
// echoed back; hasApiKey signals whether one is stored.
type searchSettingsView struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	BaseURL      string `json:"baseUrl"`
	ChunkSeconds int    `json:"chunkSeconds"`
	ShowInPlayer bool   `json:"showInPlayer"`
	HasApiKey    bool   `json:"hasApiKey"`
}

func viewSearchConfig(cfg search.Config) searchSettingsView {
	return searchSettingsView{
		Enabled:      cfg.Enabled,
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		BaseURL:      cfg.BaseURL,
		ChunkSeconds: cfg.ChunkSeconds,
		ShowInPlayer: cfg.ShowInPlayer,
		HasApiKey:    cfg.APIKey != "",
	}
}

// providerOption advertises a selectable embedding provider + its default model.
type providerOption struct {
	ID           string `json:"id"`
	DefaultModel string `json:"defaultModel"`
	NeedsAPIKey  bool   `json:"needsApiKey"`
	NeedsBaseURL bool   `json:"needsBaseUrl"`
}

func (s *Server) adminGetSearchSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetSearchConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config": viewSearchConfig(cfg),
		"providers": []providerOption{
			{ID: search.ProviderLocal, DefaultModel: search.DefaultModel(search.ProviderLocal), NeedsAPIKey: false},
			{ID: search.ProviderGemini, DefaultModel: search.DefaultModel(search.ProviderGemini), NeedsAPIKey: true},
			{ID: search.ProviderVoyage, DefaultModel: search.DefaultModel(search.ProviderVoyage), NeedsAPIKey: true},
			{ID: search.ProviderCustom, DefaultModel: "", NeedsAPIKey: true, NeedsBaseURL: true},
		},
	})
}

func (s *Server) adminSetSearchSettings(w http.ResponseWriter, r *http.Request) {
	existing, err := s.db.GetSearchConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	var incoming search.Config
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// A blank API key means "keep the stored one" (the UI never receives it back).
	if strings.TrimSpace(incoming.APIKey) == "" {
		incoming.APIKey = existing.APIKey
	}
	// If the provider changed, default the model unless one was explicitly given.
	incoming.Normalize()

	if err := s.db.SetSearchConfig(r.Context(), s.cfg.AdminLibraryID, incoming); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, viewSearchConfig(incoming))
}

// adminReindex (re)builds the search index for a video on the bulk lane. Optional
// ?lang= selects a caption track.
func (s *Server) adminReindex(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if _, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	_ = s.db.SetOperationStatus(r.Context(), videoID, db.OpKindSearchIndex, db.OpQueued, "")
	if err := s.queue.EnqueueBuildSearchIndex(queue.BuildSearchIndexPayload{
		VideoID: videoID, LibraryID: s.cfg.AdminLibraryID, Lang: lang,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "status": "indexing"})
}

// --- Public library API ---

// handleLibrarySearch is the tenant-scoped search endpoint (API-key auth).
func (s *Server) handleLibrarySearch(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "results": []db.SearchHit{}})
		return
	}
	s.runSearch(w, r, libraryID, q, strings.TrimSpace(r.URL.Query().Get("videoId")))
}

// --- Public embed (single video, gated by ShowInPlayer) ---

// handleEmbedSearch powers the in-player search box. Public + CORS-open, scoped to
// one video, and only active when the library enabled ShowInPlayer.
func (s *Server) handleEmbedSearch(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "libraryId")
	videoID := chi.URLParam(r, "videoId")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cfg, err := s.db.GetSearchConfig(r.Context(), libraryID)
	if err != nil || !cfg.Enabled || !cfg.ShowInPlayer {
		writeJSON(w, http.StatusOK, map[string]any{"results": []db.SearchHit{}})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []db.SearchHit{}})
		return
	}
	vec := s.embedQuery(r.Context(), cfg, q)
	hits, err := s.db.SearchChunks(r.Context(), libraryID, vec, q, videoID, 10)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []db.SearchHit{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": hits})
}
