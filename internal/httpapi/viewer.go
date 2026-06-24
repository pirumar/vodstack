package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
)

// defaultHistoryLimit / maxHistoryLimit bound the per-viewer history and
// per-video viewer listings.
const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 200
)

// handleMintViewerToken issues a signed viewer token (the embed "vt" param). It
// is server-to-server (API-key protected): the platform calls it for one of its
// end-users, then renders the iframe with ?vt=<token>. The token binds the
// viewer id to this library so it cannot be replayed elsewhere.
func (s *Server) handleMintViewerToken(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())

	var req struct {
		ViewerID   string `json:"viewerId"`
		TTLSeconds int    `json:"ttlSeconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.ViewerID == "" {
		writeError(w, http.StatusBadRequest, "viewerId is required")
		return
	}

	ttl := req.TTLSeconds
	if ttl <= 0 {
		ttl = s.cfg.ViewerTokenTTL
	}
	if ttl < 60 {
		ttl = 60
	}
	if ttl > s.cfg.ViewerTokenMaxTTL {
		ttl = s.cfg.ViewerTokenMaxTTL
	}

	exp := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	tok := s.signer.SignViewer(libraryID, req.ViewerID, exp)
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "exp": exp})
}

// handleEmbedProgress returns the saved resume point for the viewer carried in
// the ?vt param. Public + CORS-open (called by the iframe player). A missing or
// invalid token, or a viewer that hasn't watched this video, yields a zero
// position so the player has a uniform code path.
func (s *Server) handleEmbedProgress(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "libraryId")
	videoID := chi.URLParam(r, "videoId")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	vid, ok := s.signer.VerifyViewer(libraryID, r.URL.Query().Get("vt"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"position": 0, "completed": false})
		return
	}

	p, err := s.db.GetViewerProgress(r.Context(), libraryID, vid, videoID)
	if err != nil { // includes ErrNotFound — no saved progress yet
		writeJSON(w, http.StatusOK, map[string]any{"position": 0, "completed": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"position":       p.Position,
		"completed":      p.Completed,
		"watchedPercent": p.WatchedPercent,
	})
}

// handleViewerHistory lists the videos a viewer has watched (server-to-server,
// API-key protected). The platform calls it to render a "continue watching" /
// history list for one of its users.
func (s *Server) handleViewerHistory(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	viewerID := chi.URLParam(r, "viewerId")
	if viewerID == "" {
		writeError(w, http.StatusBadRequest, "missing viewer id")
		return
	}

	rows, err := s.db.ListViewerHistory(r.Context(), libraryID, viewerID, historyLimit(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleVideoViewerProgress returns one viewer's progress for one video
// (server-to-server, API-key protected). viewerId is a query param.
func (s *Server) handleVideoViewerProgress(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")
	viewerID := r.URL.Query().Get("viewerId")
	if viewerID == "" {
		writeError(w, http.StatusBadRequest, "viewerId is required")
		return
	}

	p, err := s.db.GetViewerProgress(r.Context(), libraryID, viewerID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no progress for this viewer")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// adminVideoViewers lists the viewers who have watched a video, for the panel's
// per-video detail drawer (session-cookie auth).
func (s *Server) adminVideoViewers(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	rows, err := s.db.ListVideoViewers(r.Context(), s.cfg.AdminLibraryID, videoID, historyLimit(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "viewers lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// historyLimit reads ?limit, defaulting and capping it.
func historyLimit(r *http.Request) int {
	limit := defaultHistoryLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	return limit
}
