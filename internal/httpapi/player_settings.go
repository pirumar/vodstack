package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/player"
)

// adminGetPlayerSettings returns the admin library's player config plus the full
// catalog of toggleable controls so the settings UI can render every option.
func (s *Server) adminGetPlayerSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetPlayerConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":      cfg,
		"allControls": player.AllControls,
	})
}

// adminSetPlayerSettings validates and stores the admin library's player config.
func (s *Server) adminSetPlayerSettings(w http.ResponseWriter, r *http.Request) {
	var cfg player.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	cfg.Normalize()
	if err := s.db.SetPlayerConfig(r.Context(), s.cfg.AdminLibraryID, cfg); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
