package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/video"
	"github.com/pirumar/vodstack/internal/webhooks"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// --- Create ---

type createVideoRequest struct {
	Title        string          `json:"title"`
	CollectionID *string         `json:"collectionId,omitempty"`
	FolderID     *string         `json:"folderId,omitempty"`
	EditSpec     *video.EditSpec `json:"editSpec,omitempty"`
}

func (s *Server) handleCreateVideo(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())

	var req createVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	id := uuid.NewString()
	v, err := s.db.CreateVideo(r.Context(), id, libraryID, req.Title, req.CollectionID, req.FolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoCreated,
		LibraryID: libraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"title": v.Title, "status": int(v.Status)},
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"videoId":   v.ID,
		"libraryId": v.LibraryID,
		"status":    int(v.Status),
	})
}

// --- Upload URL (presigned PUT) ---

func (s *Server) handleUploadURL(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	object := storage.RawObjectKey(v.ID)
	ttl := time.Duration(s.cfg.UploadURLTTL) * time.Second
	u, err := s.store.PresignedPut(r.Context(), object, ttl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "presign failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":     u.String(),
		"method":  http.MethodPut,
		"object":  object,
		"expires": int(ttl.Seconds()),
	})
}

// --- Complete (client finished uploading) ---

func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	object := storage.RawObjectKey(v.ID)
	if _, err := s.store.StatObject(r.Context(), object); err != nil {
		writeError(w, http.StatusBadRequest, "uploaded object not found in storage")
		return
	}

	if err := s.db.SetUploaded(r.Context(), v.ID, object); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if err := s.queue.EnqueueTranscode(queue.TranscodePayload{
		VideoID:      v.ID,
		LibraryID:    libraryID,
		SourceObject: object,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoUploaded,
		LibraryID: libraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"status": int(video.StatusUploaded)},
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"videoId": v.ID,
		"status":  int(video.StatusUploaded),
	})
}

// --- Fetch (migration ingest from a source URL into an existing video) ---

type fetchRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	var req fetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validSourceURL(req.URL) {
		writeError(w, http.StatusBadRequest, "valid http(s) url is required")
		return
	}

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	if err := s.enqueueFetch(r.Context(), v.ID, libraryID, req.URL); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoUploaded,
		LibraryID: libraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"status": int(video.StatusUploaded), "source": "fetch"},
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": v.ID, "status": int(video.StatusUploaded)})
}

// enqueueFetch marks a video as in-flight and enqueues a fetch+transcode job.
func (s *Server) enqueueFetch(ctx context.Context, videoID, libraryID, url string) error {
	// Move out of "created" so the UI shows it as in-progress, not awaiting upload.
	_ = s.db.SetStatus(ctx, videoID, video.StatusUploaded, 0)
	return s.queue.EnqueueTranscode(queue.TranscodePayload{
		VideoID:   videoID,
		LibraryID: libraryID,
		SourceURL: url,
	})
}

func validSourceURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// --- Get metadata ---

func (s *Server) handleGetVideo(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// --- Play (mint signed HLS URL) ---

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// The API key is the auth here, but a hard expiry still applies.
	if acc, err := s.db.GetAccess(r.Context(), libraryID, videoID); err == nil &&
		acc.ExpiresAt != nil && time.Now().After(*acc.ExpiresAt) {
		writeError(w, http.StatusForbidden, "video access expired")
		return
	}

	writeJSON(w, http.StatusOK, s.playResponse(r.Context(), v))
}

// handleDownload returns a signed URL to a video's original file, gated on the
// video's AllowDownload encoding setting. The URL carries dl=1 so the edge serves
// it as a file attachment.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, s.downloadResponse(r.Context(), v))
}

// downloadResponse builds the signed download payload (shared by the library and
// admin endpoints). Returns a 403-shaped body when downloads are disabled or the
// original is gone, surfaced via an "error" field the caller can act on.
func (s *Server) downloadResponse(ctx context.Context, v *video.Video) map[string]any {
	settings, _ := s.db.GetEncodeSettings(ctx, v.ID)
	if !settings.AllowDownload {
		return map[string]any{"error": "downloads are disabled for this video"}
	}
	if _, err := s.store.StatObject(ctx, storage.RawObjectKey(v.ID)); err != nil {
		return map[string]any{"error": "original file is not available"}
	}
	return map[string]any{
		"videoId":     v.ID,
		"downloadUrl": s.rawURL(v.ID, playTTL(v.DurationSeconds)) + "&dl=1",
	}
}

// playResponse builds the signed play payload for a video (shared by the
// library and admin endpoints), including seek-preview thumbnails, caption
// tracks and chapters.
func (s *Server) playResponse(ctx context.Context, v *video.Video) map[string]any {
	resp := map[string]any{
		"libraryId": v.LibraryID,
		"videoId":   v.ID,
		"status":    int(v.Status),
		"isReady":   v.Status.IsReady(),
	}
	if v.DurationSeconds != nil {
		resp["length"] = *v.DurationSeconds
	}

	settings, _ := s.db.GetEncodeSettings(ctx, v.ID)

	if !v.Status.IsReady() {
		// Early-Play: serve the retained original progressively while encoding
		// finishes, so the video is near-instantly playable. The player switches to
		// the HLS master once isReady. Enabling this publicly exposes the original.
		if settings.EarlyPlay {
			if _, err := s.store.StatObject(ctx, storage.RawObjectKey(v.ID)); err == nil {
				resp["earlyPlay"] = true
				resp["earlyPlayUrl"] = s.rawURL(v.ID, playTTL(v.DurationSeconds))
			}
		}
		return resp
	}

	resp["hlsUrl"] = s.auxURL(v, "master.m3u8")
	if settings.MP4Fallback {
		resp["mp4Url"] = s.auxURL(v, "fallback.mp4")
	}
	if settings.AllowDownload {
		resp["downloadUrl"] = s.rawURL(v.ID, playTTL(v.DurationSeconds)) + "&dl=1"
	}
	if v.ThumbnailFile != nil {
		resp["posterUrl"] = s.auxURL(v, *v.ThumbnailFile)
	}
	if v.ThumbnailsVTT != nil {
		resp["thumbnailsUrl"] = s.auxURL(v, *v.ThumbnailsVTT)
	}
	if len(v.Chapters) > 0 {
		resp["chaptersUrl"] = s.auxURL(v, "chapters.vtt")
		resp["chapters"] = v.Chapters
	}
	if caps, err := s.db.ListCaptions(ctx, v.ID); err == nil && len(caps) > 0 {
		prefix := storage.HLSPrefix(v.ID)
		arr := make([]map[string]string, 0, len(caps))
		for _, c := range caps {
			arr = append(arr, map[string]string{
				"lang":  c.Lang,
				"label": c.Label,
				"url":   s.auxURL(v, strings.TrimPrefix(c.Object, prefix)),
			})
		}
		resp["captions"] = arr
	}
	return resp
}

// auxURL signs a URL for any file under a video's HLS prefix (master playlist,
// poster, thumbnails.vtt, captions/*.vtt, chapters.vtt).
func (s *Server) auxURL(v *video.Video, filename string) string {
	prefix := "/" + storage.HLSPrefix(v.ID) // -> /hls/<id>/
	return s.signer.SignedURL(s.cfg.PublicBaseURL, prefix, filename, playTTL(v.DurationSeconds))
}

// rawURL signs a URL for a video's retained original under the /raw/ prefix (used
// for Early-Play and downloads). The edge serves /raw/ with the same HMAC check
// as /hls/.
func (s *Server) rawURL(videoID string, ttl time.Duration) string {
	prefix := "/raw/" + videoID + "/"
	return s.signer.SignedURL(s.cfg.PublicBaseURL, prefix, "source", ttl)
}

// signedURLs returns the signed master playlist and poster URLs for a ready
// video (empty strings otherwise). Used by the library list grid.
func (s *Server) signedURLs(v *video.Video) (hls, poster string) {
	if !v.Status.IsReady() {
		return "", ""
	}
	hls = s.auxURL(v, "master.m3u8")
	if v.ThumbnailFile != nil {
		poster = s.auxURL(v, *v.ThumbnailFile)
	}
	return hls, poster
}

// playTTL gives the token a lifetime comfortably longer than the video so a
// viewer can finish (and seek) without the token expiring mid-playback. Capped
// at 6h; the frontend refreshes on the rare 403.
func playTTL(duration *int) time.Duration {
	base := 2 * time.Hour
	if duration != nil {
		d := time.Duration(*duration)*time.Second + 30*time.Minute
		if d > base {
			base = d
		}
	}
	const max = 6 * time.Hour
	if base > max {
		base = max
	}
	return base
}

// --- Delete ---

func (s *Server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	libraryID := libraryFromCtx(r.Context())
	videoID := chi.URLParam(r, "videoId")

	// Soft-delete (recoverable). The worker purges objects after retention.
	if err := s.db.SoftDelete(r.Context(), libraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	// Stop any in-flight work (e.g. a running transcode) for this video.
	s.queue.CancelVideoTasks(videoID)
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoDeleted,
		LibraryID: libraryID,
		VideoID:   videoID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
