package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pirumar/vodstack/internal/auth"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/video"
	"github.com/pirumar/vodstack/internal/webhooks"
)

const sessionCookie = "fs_admin"

// adminRoutes mounts the BFF used by the web panel. These operate on the
// configured AdminLibraryID server-side, so the library API key never reaches
// the browser. Auth is a signed session cookie minted from an admin password.
func (s *Server) adminRoutes(r chi.Router) {
	r.Post("/login", s.adminLogin)
	r.Post("/logout", s.adminLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.adminAuth)
		r.Get("/me", s.adminMe)
		r.Get("/videos", s.adminListVideos)
		r.Post("/videos", s.adminCreateVideo)
		r.Post("/videos/import", s.adminImport)
		r.Put("/videos/{videoId}/source", s.adminUploadSource)
		r.Get("/videos/{videoId}", s.adminGetVideo)
		r.Patch("/videos/{videoId}", s.adminUpdateVideo)
		r.Get("/videos/{videoId}/play", s.adminPlay)
		r.Get("/videos/{videoId}/download", s.adminDownload)
		r.Delete("/videos/{videoId}", s.adminDeleteVideo)
		r.Put("/videos/{videoId}/chapters", s.adminSetChapters)
		r.Put("/videos/{videoId}/folder", s.adminMoveVideo)
		r.Post("/videos/{videoId}/captions", s.adminUploadCaption)
		r.Delete("/videos/{videoId}/captions/{lang}", s.adminDeleteCaption)
		// Custom poster: upload an image (sync) or grab a frame (worker job).
		r.Put("/videos/{videoId}/poster", s.adminUploadPoster)
		r.Post("/videos/{videoId}/poster/frame", s.adminPosterFromFrame)

		// Folders: nested organization of the admin library's videos.
		r.Get("/folders", s.adminListFolders)
		r.Post("/folders", s.adminCreateFolder)
		r.Put("/folders/{folderId}", s.adminUpdateFolder)
		r.Delete("/folders/{folderId}", s.adminDeleteFolder)

		// Player: library-wide player customization settings.
		r.Get("/player-settings", s.adminGetPlayerSettings)
		r.Put("/player-settings", s.adminSetPlayerSettings)

		// Encoding: library-wide encoding defaults (resolutions, codecs, MP4
		// fallback, download, Early-Play, multi-audio, watermark).
		r.Get("/encoding-settings", s.adminGetEncodingSettings)
		r.Put("/encoding-settings", s.adminSetEncodingSettings)
		r.Put("/encoding-settings/watermark", s.adminUploadWatermark)
		r.Delete("/encoding-settings/watermark", s.adminDeleteWatermark)

		// In-video search: library settings + query + per-video (re)index.
		r.Get("/search", s.adminSearch)
		r.Get("/search-settings", s.adminGetSearchSettings)
		r.Put("/search-settings", s.adminSetSearchSettings)
		r.Post("/videos/{videoId}/reindex", s.adminReindex)

		// AI content: LLM router settings + per-video generation (summary/tags/chapters).
		r.Get("/llm-settings", s.adminGetLLMSettings)
		r.Put("/llm-settings", s.adminSetLLMSettings)
		r.Post("/videos/{videoId}/ai-content", s.adminGenerateAIContent)

		// Trash: soft-delete (above) is recoverable; these manage the bin.
		r.Get("/trash", s.adminListTrash)
		r.Post("/videos/{videoId}/restore", s.adminRestore)
		r.Delete("/videos/{videoId}/purge", s.adminPurge)

		// Platform: API keys + webhook endpoints for the admin library.
		r.Get("/api-keys", s.adminListAPIKeys)
		r.Post("/api-keys", s.adminCreateAPIKey)
		r.Delete("/api-keys/{keyId}", s.adminRevokeAPIKey)
		r.Get("/webhooks", s.adminListWebhooks)
		r.Post("/webhooks", s.adminCreateWebhook)
		r.Delete("/webhooks/{webhookId}", s.adminDeleteWebhook)

		// Codec: opt a video into an extra-codec backfill (bulk lane). /av1 is the
		// legacy alias; /codecs/{codec} supports av1|hevc|vp9.
		r.Post("/videos/{videoId}/av1", s.adminGenerateAV1)
		r.Post("/videos/{videoId}/codecs/{codec}", s.adminGenerateCodec)
		// AI: generate captions via ASR (bulk lane).
		r.Post("/videos/{videoId}/captions/auto", s.adminAutoCaption)
		// Analytics: library-wide rollup + per-video QoE.
		r.Get("/analytics", s.adminLibraryAnalytics)
		r.Get("/videos/{videoId}/analytics", s.adminAnalytics)
		// Viewers: who watched this video + how far (per-viewer progress).
		r.Get("/videos/{videoId}/viewers", s.adminVideoViewers)
		// Security: AES-128 encryption + per-video access policy.
		r.Post("/videos/{videoId}/encrypt", s.adminEncrypt)
		r.Put("/videos/{videoId}/access", s.adminSetAccess)
		// Operations: live status (queued/running/done/failed) of advanced jobs.
		r.Get("/videos/{videoId}/operations", s.adminVideoOperations)

		// Multi-tenancy: provision libraries + mint their keys.
		r.Get("/libraries", s.adminListLibraries)
		r.Post("/libraries", s.adminCreateLibrary)
		r.Post("/libraries/{libId}/keys", s.adminCreateLibraryKey)
	})
}

// adminAuth gates routes on a valid session cookie.
func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.signer.ValidSession(c.Value) {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AdminPassword == "" {
		writeError(w, http.StatusServiceUnavailable, "admin panel disabled (no ADMIN_PASSWORD set)")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// Constant-time compare against the configured password.
	if !auth.Equal(auth.HashAPIKey(req.Password), auth.HashAPIKey(s.cfg.AdminPassword)) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	ttl := time.Duration(s.cfg.AdminSessionTTL) * time.Second
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.signer.NewSession(ttl),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.cfg.AdminSessionTTL,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) adminLogout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) adminMe(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"libraryId":     s.cfg.AdminLibraryID,
		"embedBaseUrl":  s.cfg.EmbedBaseURL,
	})
}

// listItem is a video enriched with signed URLs so the grid can show posters
// and play without an extra round-trip per video.
type listItem struct {
	video.Video
	HlsUrl    string `json:"hlsUrl,omitempty"`
	PosterUrl string `json:"posterUrl,omitempty"`
}

func (s *Server) adminListVideos(w http.ResponseWriter, r *http.Request) {
	// ?folderId controls scoping: absent or "all" -> every video; "root" or ""
	// -> library root only; a UUID -> that folder's direct children.
	var (
		vids []video.Video
		err  error
	)
	switch fid := r.URL.Query().Get("folderId"); fid {
	case "", "all":
		vids, err = s.db.ListVideos(r.Context(), s.cfg.AdminLibraryID)
	case "root":
		vids, err = s.db.ListVideosInFolder(r.Context(), s.cfg.AdminLibraryID, nil)
	default:
		vids, err = s.db.ListVideosInFolder(r.Context(), s.cfg.AdminLibraryID, &fid)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	items := make([]listItem, 0, len(vids))
	for i := range vids {
		v := vids[i]
		hls, poster := s.signedURLs(&v)
		items = append(items, listItem{Video: v, HlsUrl: hls, PosterUrl: poster})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) adminCreateVideo(w http.ResponseWriter, r *http.Request) {
	var req createVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := req.EditSpec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid editSpec: "+err.Error())
		return
	}
	id := uuid.NewString()
	v, err := s.db.CreateVideo(r.Context(), id, s.cfg.AdminLibraryID, req.Title, req.CollectionID, req.FolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	if req.EditSpec != nil && !req.EditSpec.IsIdentity() {
		if err := s.db.SetEditSpec(r.Context(), v.ID, req.EditSpec.Normalize()); err != nil {
			writeError(w, http.StatusInternalServerError, "save edit spec failed")
			return
		}
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoCreated,
		LibraryID: s.cfg.AdminLibraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"title": v.Title, "status": int(v.Status)},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"videoId": v.ID, "status": int(v.Status)})
}

// adminImport creates a video and ingests it from a source URL (migrate an
// existing Bunny/Vimeo/any MP4 into vodstack without a browser upload).
func (s *Server) adminImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validSourceURL(req.URL) {
		writeError(w, http.StatusBadRequest, "valid http(s) url is required")
		return
	}
	title := req.Title
	if title == "" {
		title = deriveTitle(req.URL)
	}

	id := uuid.NewString()
	v, err := s.db.CreateVideo(r.Context(), id, s.cfg.AdminLibraryID, title, nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	if err := s.enqueueFetch(r.Context(), v.ID, s.cfg.AdminLibraryID, req.URL); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoUploaded,
		LibraryID: s.cfg.AdminLibraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"status": int(video.StatusUploaded), "source": "import"},
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": v.ID, "status": int(video.StatusUploaded)})
}

// deriveTitle picks a human title from a URL's last path segment.
func deriveTitle(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "Imported video"
	}
	name := path.Base(u.Path)
	name = strings.TrimSuffix(name, path.Ext(name))
	if name == "" || name == "." || name == "/" {
		return "Imported video"
	}
	return name
}

// adminUploadSource streams the request body straight into MinIO as the raw
// source, then marks the video uploaded and enqueues transcoding. The browser
// PUTs the file bytes here (proxied), so there is no MinIO CORS/presign dance.
func (s *Server) adminUploadSource(w http.ResponseWriter, r *http.Request) {
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

	object := storage.RawObjectKey(v.ID)
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	if err := s.store.PutStream(r.Context(), object, r.Body, r.ContentLength, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	if err := s.db.SetUploaded(r.Context(), v.ID, object); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if err := s.queue.EnqueueTranscode(queue.TranscodePayload{
		VideoID: v.ID, LibraryID: s.cfg.AdminLibraryID, SourceObject: object,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoUploaded,
		LibraryID: s.cfg.AdminLibraryID,
		VideoID:   v.ID,
		Data:      map[string]any{"status": int(video.StatusUploaded)},
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": v.ID, "status": int(video.StatusUploaded)})
}

// adminGetVideo returns the video plus its caption tracks (for the manage UI).
func (s *Server) adminGetVideo(w http.ResponseWriter, r *http.Request) {
	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, chi.URLParam(r, "videoId"))
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	caps, _ := s.db.ListCaptions(r.Context(), v.ID)
	if caps == nil {
		caps = []video.Caption{}
	}
	writeJSON(w, http.StatusOK, struct {
		*video.Video
		Captions []video.Caption `json:"captions"`
	}{v, caps})
}

// adminUpdateVideo patches editable metadata (title, description, tags). Only
// fields present in the request body are changed, so the UI can save one field
// at a time. Returns the updated video.
func (s *Server) adminUpdateVideo(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	if _, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	var body struct {
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		Tags        *[]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		if err := s.db.SetTitle(r.Context(), s.cfg.AdminLibraryID, videoID, title); err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
	}
	if body.Description != nil {
		if err := s.db.SetDescription(r.Context(), s.cfg.AdminLibraryID, videoID, strings.TrimSpace(*body.Description)); err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
	}
	if body.Tags != nil {
		tags := make([]string, 0, len(*body.Tags))
		for _, t := range *body.Tags {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		if err := s.db.SetTags(r.Context(), s.cfg.AdminLibraryID, videoID, tags); err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
	}

	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// adminSetChapters stores YouTube-style chapters (sorted, with a synthesized
// chapters.vtt uploaded to MinIO so any player can render section markers).
func (s *Server) adminSetChapters(w http.ResponseWriter, r *http.Request) {
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

	var chapters []video.Chapter
	if err := json.NewDecoder(r.Body).Decode(&chapters); err != nil {
		writeError(w, http.StatusBadRequest, "invalid chapters json")
		return
	}
	// Sort by start; drop empties.
	cleaned := chapters[:0]
	for _, c := range chapters {
		if strings.TrimSpace(c.Title) != "" && c.Start >= 0 {
			cleaned = append(cleaned, c)
		}
	}
	sort.Slice(cleaned, func(i, j int) bool { return cleaned[i].Start < cleaned[j].Start })

	chaptersObj := storage.HLSPrefix(videoID) + "chapters.vtt"
	if len(cleaned) == 0 {
		_ = s.db.SetChapters(r.Context(), s.cfg.AdminLibraryID, videoID, nil)
		_ = s.store.RemoveObject(r.Context(), chaptersObj)
		writeJSON(w, http.StatusOK, map[string]any{"chapters": []video.Chapter{}})
		return
	}

	raw, _ := json.Marshal(cleaned)
	if err := s.db.SetChapters(r.Context(), s.cfg.AdminLibraryID, videoID, raw); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	dur := 0
	if v.DurationSeconds != nil {
		dur = *v.DurationSeconds
	}
	vtt := video.ChaptersVTT(cleaned, dur)
	if err := s.store.PutBytes(r.Context(), chaptersObj, []byte(vtt), "text/vtt"); err != nil {
		writeError(w, http.StatusInternalServerError, "vtt upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chapters": cleaned})
}

// adminUploadCaption stores a subtitle track (?lang=tr&label=Türkçe). Accepts a
// WebVTT or SRT body; SRT is converted to VTT.
func (s *Server) adminUploadCaption(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	label := strings.TrimSpace(r.URL.Query().Get("label"))
	if lang == "" {
		writeError(w, http.StatusBadRequest, "lang is required")
		return
	}
	if label == "" {
		label = strings.ToUpper(lang)
	}
	if _, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20)) // captions are small; cap at 5MB
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty caption body")
		return
	}
	vtt := convertToVTT(string(body))

	object := storage.HLSPrefix(videoID) + "captions/" + lang + ".vtt"
	if err := s.store.PutBytes(r.Context(), object, []byte(vtt), "text/vtt"); err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	if err := s.db.AddCaption(r.Context(), uuid.NewString(), videoID, lang, label, object); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"lang": lang, "label": label})
}

func (s *Server) adminDeleteCaption(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	lang := chi.URLParam(r, "lang")
	object, err := s.db.DeleteCaption(r.Context(), videoID, lang)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "caption not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	_ = s.store.RemoveObject(r.Context(), object)
	w.WriteHeader(http.StatusNoContent)
}

// convertToVTT returns WebVTT unchanged, or converts SRT (comma decimal in
// timestamps, no header) to VTT.
func convertToVTT(content string) string {
	content = strings.TrimPrefix(content, "\ufeff") // strip BOM
	if strings.HasPrefix(strings.TrimSpace(content), "WEBVTT") {
		return content
	}
	// SRT -> VTT: header + comma->dot in timecodes.
	converted := srtTimecode.ReplaceAllString(content, "$1.$2")
	return "WEBVTT\n\n" + converted
}

var srtTimecode = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

func (s *Server) adminPlay(w http.ResponseWriter, r *http.Request) {
	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, chi.URLParam(r, "videoId"))
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, s.playResponse(r.Context(), v))
}

// adminDownload returns a signed download URL for a video's original file (gated
// on its AllowDownload setting).
func (s *Server) adminDownload(w http.ResponseWriter, r *http.Request) {
	v, err := s.db.GetVideo(r.Context(), s.cfg.AdminLibraryID, chi.URLParam(r, "videoId"))
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

// adminDeleteVideo soft-deletes (moves to trash). Files are kept so it can be
// restored; the worker purges them after the retention window.
func (s *Server) adminDeleteVideo(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	if err := s.db.SoftDelete(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	// Stop any in-flight work (e.g. a running transcode) so we don't keep burning
	// CPU on a video the user just trashed.
	s.queue.CancelVideoTasks(videoID)
	s.hooks.Dispatch(r.Context(), webhooks.Event{
		Type:      webhooks.EventVideoDeleted,
		LibraryID: s.cfg.AdminLibraryID,
		VideoID:   videoID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// adminListTrash returns trashed videos plus the retention window so the UI can
// show "X days left".
func (s *Server) adminListTrash(w http.ResponseWriter, r *http.Request) {
	vids, err := s.db.ListTrashed(r.Context(), s.cfg.AdminLibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	items := make([]map[string]any, 0, len(vids))
	for i := range vids {
		v := vids[i]
		item := map[string]any{
			"videoId":              v.ID,
			"title":                v.Title,
			"status":               int(v.Status),
			"availableResolutions": v.AvailableResolutions,
		}
		if v.DurationSeconds != nil {
			item["length"] = *v.DurationSeconds
		}
		if v.SizeBytes != nil {
			item["storageSize"] = *v.SizeBytes
		}
		if v.DeletedAt != nil {
			item["deletedAt"] = v.DeletedAt.UTC().Format(time.RFC3339)
			purgeAt := v.DeletedAt.Add(time.Duration(s.cfg.TrashRetentionDays) * 24 * time.Hour)
			item["purgeAt"] = purgeAt.UTC().Format(time.RFC3339)
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"retentionDays": s.cfg.TrashRetentionDays,
		"videos":        items,
	})
}

func (s *Server) adminRestore(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	if err := s.db.Restore(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not in trash")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"videoId": videoID, "status": "restored"})
}

// adminPurge permanently deletes a video now (objects + row), bypassing the
// retention window.
func (s *Server) adminPurge(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	// Defensive: cancel any still-running job before we delete its objects/row.
	s.queue.CancelVideoTasks(videoID)
	_ = s.store.RemovePrefix(r.Context(), storage.HLSPrefix(videoID))
	_ = s.store.RemovePrefix(r.Context(), "raw/"+videoID+"/")
	if err := s.db.PurgeVideo(r.Context(), s.cfg.AdminLibraryID, videoID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "purge failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
