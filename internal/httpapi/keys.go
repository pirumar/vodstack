package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/storage"
)

// handleKey serves the AES-128 content key for an encrypted video. It is gated by
// the SAME signed playback token that authorizes the segments: the player's HLS
// loader re-appends the master's ?exp&token to the key URI, and we verify it
// against the video's /hls/<id>/ prefix. No valid token -> 403. The key is never
// cached (the edge does not front this route; the URI points straight at the API).
func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	videoID := chi.URLParam(r, "videoId")

	exp, err := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
	token := r.URL.Query().Get("token")
	if err != nil || token == "" {
		writeError(w, http.StatusForbidden, "missing token")
		return
	}
	// The playback token signs the video's HLS prefix.
	prefix := "/" + storage.HLSPrefix(videoID) // -> /hls/<id>/
	if !s.signer.Verify(prefix, exp, token) {
		writeError(w, http.StatusForbidden, "invalid or expired token")
		return
	}

	ck, err := s.db.GetContentKeyForVideo(r.Context(), videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no key for video")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key lookup failed")
		return
	}
	keyBytes, err := hex.DecodeString(ck.KeyHex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt key")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(keyBytes)
}
