package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/encoding"
)

// watermarkObjectKey is the MinIO key for a library's watermark image. PNG keeps
// alpha so the overlay can be transparent.
func watermarkObjectKey(libraryID string) string {
	return "library/" + libraryID + "/watermark.png"
}

// adminGetEncodingSettings returns the admin library's encoding defaults plus the
// catalogs of selectable resolutions/codecs so the settings UI can render every
// option.
func (s *Server) adminGetEncodingSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetEncodingConfig(r.Context(), s.cfg.AdminLibraryID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":         cfg,
		"allResolutions": encoding.AllResolutions,
		"allCodecs":      encoding.AllCodecs,
	})
}

// adminSetEncodingSettings validates and stores the admin library's encoding
// defaults. New uploads inherit these; in-flight/finished videos keep the
// snapshot taken at their creation.
func (s *Server) adminSetEncodingSettings(w http.ResponseWriter, r *http.Request) {
	var cfg encoding.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	cfg.Normalize()
	if err := s.db.SetEncodingConfig(r.Context(), s.cfg.AdminLibraryID, cfg); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// adminUploadWatermark stores the library's watermark image (raw PNG/JPEG body)
// and turns the watermark on in the encoding config. New uploads burn it in.
func (s *Server) adminUploadWatermark(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // watermarks are small; cap at 10MB
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty watermark body")
		return
	}
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		ct = "image/png"
	}
	object := watermarkObjectKey(s.cfg.AdminLibraryID)
	if err := s.store.PutBytes(r.Context(), object, body, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	cfg, err := s.db.GetEncodingConfig(r.Context(), s.cfg.AdminLibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	cfg.Watermark.Object = object
	cfg.Watermark.Enabled = true
	cfg.Normalize()
	if err := s.db.SetEncodingConfig(r.Context(), s.cfg.AdminLibraryID, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// adminDeleteWatermark removes the watermark image and disables the overlay.
func (s *Server) adminDeleteWatermark(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.GetEncodingConfig(r.Context(), s.cfg.AdminLibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if cfg.Watermark.Object != "" {
		_ = s.store.RemoveObject(r.Context(), cfg.Watermark.Object)
	}
	cfg.Watermark.Object = ""
	cfg.Watermark.Enabled = false
	cfg.Normalize()
	if err := s.db.SetEncodingConfig(r.Context(), s.cfg.AdminLibraryID, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
