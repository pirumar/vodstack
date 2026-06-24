package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/llm"
	"github.com/pirumar/vodstack/internal/queue"
)

// llmSettingsView is the wire shape for LLM settings — the API key is never echoed
// back; hasApiKey signals whether one is stored.
type llmSettingsView struct {
	Enabled     bool    `json:"enabled"`
	BaseURL     string  `json:"baseUrl"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens"`
	HasApiKey   bool    `json:"hasApiKey"`
}

func viewLLMConfig(cfg llm.Config) llmSettingsView {
	return llmSettingsView{
		Enabled:     cfg.Enabled,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		HasApiKey:   cfg.APIKey != "",
	}
}

func (s *Server) adminGetLLMSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetLLMConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": viewLLMConfig(cfg)})
}

func (s *Server) adminSetLLMSettings(w http.ResponseWriter, r *http.Request) {
	existing, err := s.db.GetLLMConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	var incoming llm.Config
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// A blank API key means "keep the stored one" (the UI never receives it back).
	if strings.TrimSpace(incoming.APIKey) == "" {
		incoming.APIKey = existing.APIKey
	}
	incoming.Normalize()

	if err := s.db.SetLLMConfig(r.Context(), s.cfg.AdminLibraryID, incoming); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, viewLLMConfig(incoming))
}

// adminGenerateAIContent enqueues LLM content generation for a video. Optional
// ?kinds=summary,tags,chapters (empty = all); ?lang= selects a caption track.
func (s *Server) adminGenerateAIContent(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	if _, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	var kinds []string
	if k := strings.TrimSpace(r.URL.Query().Get("kinds")); k != "" {
		kinds = strings.Split(k, ",")
	}
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	_ = s.db.SetOperationStatus(r.Context(), videoID, db.OpKindAIContent, db.OpQueued, "")
	if err := s.queue.EnqueueGenerateAIContent(queue.GenerateAIContentPayload{
		VideoID: videoID, LibraryID: s.cfg.AdminLibraryID, Lang: lang, Kinds: kinds,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "status": "generating"})
}
