package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
)

// heatmapBuckets is the resolution of the watchtime curve (one value per ~1% of
// the video). The player smooths it for display.
const heatmapBuckets = 100

// handleHeatmap serves the public watchtime heatmap for a video, derived from
// playback_events. It is gated by the library's ShowHeatmap setting and returns
// {"heatmap": null} until enough data is collected.
func (s *Server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "libraryId")
	videoID := chi.URLParam(r, "videoId")

	w.Header().Set("Access-Control-Allow-Origin", "*")

	cfg, err := s.db.GetPlayerConfig(r.Context(), libraryID)
	if err != nil || !cfg.ShowHeatmap {
		writeJSON(w, http.StatusOK, map[string]any{"heatmap": nil})
		return
	}

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil || v.DurationSeconds == nil {
		writeJSON(w, http.StatusOK, map[string]any{"heatmap": nil})
		return
	}

	hm, err := s.db.GetHeatmap(r.Context(), libraryID, videoID, heatmapBuckets, *v.DurationSeconds)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"heatmap": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"heatmap": hm})
}
