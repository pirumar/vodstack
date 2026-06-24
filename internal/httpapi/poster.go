package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
)

// customPosterName builds a fresh, unique poster filename. A new name on every
// change busts any edge/CDN cache that keyed on the old object path, so the new
// poster shows up immediately instead of serving a stale cached image.
func customPosterName() string {
	return "poster-custom-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".jpg"
}

// adminUploadPoster stores an admin-supplied image (raw JPEG/PNG body) as the
// video's custom poster and repoints thumbnail_file at it. Synchronous: the
// bytes are already in the request, so no worker round-trip is needed.
func (s *Server) adminUploadPoster(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // posters are small; cap at 10MB
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty poster body")
		return
	}
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		writeError(w, http.StatusBadRequest, "poster must be an image")
		return
	}

	name := customPosterName()
	object := storage.HLSPrefix(videoID) + name
	if err := s.store.PutBytes(r.Context(), object, body, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	if err := s.db.SetThumbnail(r.Context(), s.cfg.AdminLibraryID, videoID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	removeOldCustomPoster(r, s, videoID, v.ThumbnailFile)

	writeJSON(w, http.StatusOK, map[string]any{"videoId": videoID, "posterUrl": s.auxURL(v, name)})
}

// adminPosterFromFrame queues a worker job to grab a frame from the raw source at
// ?t=<seconds> and publish it as the custom poster. Deferred to the worker
// because the API image ships without ffmpeg. The UI polls the operations
// endpoint (kind "poster") for completion.
func (s *Server) adminPosterFromFrame(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !v.Status.IsReady() {
		writeError(w, http.StatusConflict, "video must finish encoding first")
		return
	}

	at, _ := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
	if at < 0 {
		at = 0
	}

	name := customPosterName()
	_ = s.db.SetOperationStatus(r.Context(), videoID, db.OpKindPoster, db.OpQueued, "")
	if err := s.queue.EnqueuePosterFrame(queue.PosterFramePayload{
		VideoID:   videoID,
		LibraryID: s.cfg.AdminLibraryID,
		AtSeconds: at,
		Object:    name,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "atSeconds": at})
}

// removeOldCustomPoster best-effort deletes a previously-set custom poster object
// so repeated changes don't accumulate. The auto-generated "poster.jpg" is left
// alone (it's the fallback the transcode produced).
func removeOldCustomPoster(r *http.Request, s *Server, videoID string, prev *string) {
	if prev == nil || !strings.HasPrefix(*prev, "poster-custom-") {
		return
	}
	_ = s.store.RemoveObject(r.Context(), storage.HLSPrefix(videoID)+*prev)
}
