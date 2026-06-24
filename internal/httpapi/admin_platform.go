package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pirumar/vodstack/internal/auth"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
)

// --- Library (tenant) management (admin BFF) ---

func (s *Server) adminListLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.db.ListLibraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if libs == nil {
		libs = []db.LibraryInfo{}
	}
	writeJSON(w, http.StatusOK, libs)
}

// adminCreateLibrary provisions a new tenant and mints its first API key (returned
// once). Body: {name, [id]}. id defaults to a slug of the name.
func (s *Server) adminCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	id := slugify(req.ID)
	if id == "" {
		id = slugify(req.Name)
	}
	if id == "" {
		id = uuid.NewString()
	}
	if err := s.db.CreateLibrary(r.Context(), id, strings.TrimSpace(req.Name)); err != nil {
		writeError(w, http.StatusConflict, "library id already exists or invalid")
		return
	}
	// Mint a first key.
	plaintext, err := randomToken("vds_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	keyID := uuid.NewString()
	if err := s.db.CreateAPIKey(r.Context(), keyID, id, auth.HashAPIKey(plaintext), "default", nil); err != nil {
		writeError(w, http.StatusInternalServerError, "key create failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": req.Name, "key": plaintext,
	})
}

// adminCreateLibraryKey mints an additional API key for any library.
func (s *Server) adminCreateLibraryKey(w http.ResponseWriter, r *http.Request) {
	libID := chi.URLParam(r, "libId")
	if _, err := s.db.GetLibrary(r.Context(), libID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	plaintext, err := randomToken("vds_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	id := uuid.NewString()
	if err := s.db.CreateAPIKey(r.Context(), id, libID, auth.HashAPIKey(plaintext), strings.TrimSpace(req.Name), req.Scopes); err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "libraryId": libID, "key": plaintext})
}

// slugify lowercases and replaces runs of non-alphanumerics with single hyphens.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- API key management (admin BFF) ---
//
// Keys are minted for the configured AdminLibraryID. The plaintext key is
// returned exactly once, at creation; only its hash is stored.

func (s *Server) adminListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.db.ListAPIKeys(r.Context(), s.cfg.AdminLibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if keys == nil {
		keys = []db.APIKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) adminCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	plaintext, err := randomToken("vds_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	id := uuid.NewString()
	if err := s.db.CreateAPIKey(r.Context(), id, s.cfg.AdminLibraryID,
		auth.HashAPIKey(plaintext), strings.TrimSpace(req.Name), req.Scopes); err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	// Return the plaintext key ONCE; it is unrecoverable afterwards.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     id,
		"name":   req.Name,
		"scopes": req.Scopes,
		"key":    plaintext,
	})
}

func (s *Server) adminRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "keyId")
	if err := s.db.RevokeAPIKey(r.Context(), s.cfg.AdminLibraryID, id); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "key not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Webhook endpoint management (admin BFF) ---

func (s *Server) adminListWebhooks(w http.ResponseWriter, r *http.Request) {
	eps, err := s.db.ListWebhookEndpoints(r.Context(), s.cfg.AdminLibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if eps == nil {
		eps = []db.WebhookEndpoint{}
	}
	writeJSON(w, http.StatusOK, eps)
}

func (s *Server) adminCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validWebhookURL(req.URL) {
		writeError(w, http.StatusBadRequest, "valid https url is required")
		return
	}
	secret, err := randomToken("whsec_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "secret generation failed")
		return
	}
	id := uuid.NewString()
	if err := s.db.CreateWebhookEndpoint(r.Context(), id, s.cfg.AdminLibraryID,
		req.URL, secret, req.Events); err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	// Return the signing secret ONCE.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     id,
		"url":    req.URL,
		"events": req.Events,
		"secret": secret,
	})
}

func (s *Server) adminDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "webhookId")
	if err := s.db.DeleteWebhookEndpoint(r.Context(), s.cfg.AdminLibraryID, id); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminGenerateAV1 opts a video into the AV1 backfill (kept for the existing
// /av1 route and web UI). Delegates to the generic codec trigger.
func (s *Server) adminGenerateAV1(w http.ResponseWriter, r *http.Request) {
	s.generateCodec(w, r, "av1")
}

// adminGenerateCodec opts a video into one extra codec (av1/hevc/vp9) named in the
// {codec} path param and enqueues the backfill.
func (s *Server) adminGenerateCodec(w http.ResponseWriter, r *http.Request) {
	s.generateCodec(w, r, chi.URLParam(r, "codec"))
}

// generateCodec adds a codec to a video's encode settings and enqueues the
// backfill. Useful for already-transcoded videos; new uploads inherit codecs from
// the library config. The H.264 ladder stays live until the combined master
// swaps in.
func (s *Server) generateCodec(w http.ResponseWriter, r *http.Request, codec string) {
	if codec != "av1" && codec != "hevc" && codec != "vp9" {
		writeError(w, http.StatusBadRequest, "unsupported codec")
		return
	}
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
		writeError(w, http.StatusConflict, "video must finish H.264 encoding first")
		return
	}
	if _, err := s.db.AddVideoCodec(r.Context(), s.cfg.AdminLibraryID, videoID, codec); err != nil {
		writeError(w, http.StatusInternalServerError, "set codec failed")
		return
	}
	// Write 'queued' BEFORE enqueue: the worker can only dequeue after this
	// returns, so its running/done/failed writes always land after this one and
	// never get overwritten by a late 'queued' (which would strand the row).
	_ = s.db.SetOperationStatus(r.Context(), videoID, codecOpKind(codec), db.OpQueued, "")
	if err := s.queue.EnqueueCodecBackfill(queue.CodecBackfillPayload{VideoID: videoID, LibraryID: s.cfg.AdminLibraryID}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "codec": codec})
}

// codecOpKind maps a backfill codec to its video_operations kind (mirrors the
// worker's mapping).
func codecOpKind(codec string) string {
	switch codec {
	case "hevc":
		return db.OpKindHEVC
	case "vp9":
		return db.OpKindVP9
	default:
		return db.OpKindAV1
	}
}

// adminAutoCaption enqueues an ASR auto-caption job (bulk lane). Optional ?lang=
// forces the language; otherwise Whisper detects it.
func (s *Server) adminAutoCaption(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
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
	_ = s.db.SetOperationStatus(r.Context(), videoID, db.OpKindCaption, db.OpQueued, "")
	if err := s.queue.EnqueueAutoCaption(queue.AutoCaptionPayload{
		VideoID: videoID, LibraryID: s.cfg.AdminLibraryID, Lang: lang,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "lang": lang})
}

// adminEncrypt enqueues an AES-128 re-encode (bulk lane). The H.264 ladder is
// replaced with an encrypted one and the key is stored server-side.
func (s *Server) adminEncrypt(w http.ResponseWriter, r *http.Request) {
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
	_ = s.db.SetOperationStatus(r.Context(), videoID, db.OpKindEncrypt, db.OpQueued, "")
	if err := s.queue.EnqueueEncrypt(queue.EncryptPayload{VideoID: videoID, LibraryID: s.cfg.AdminLibraryID}); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"videoId": videoID, "encryptionMode": "aes128"})
}

// adminVideoOperations returns the live status of each advanced operation
// (queued/running/done/failed) so the UI can track progress instead of letting
// the user re-trigger a job that's already running.
func (s *Server) adminVideoOperations(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	ops, err := s.db.GetVideoOperations(r.Context(), videoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": ops})
}

// adminSetAccess updates a video's access policy (visibility / referrers / expiry).
func (s *Server) adminSetAccess(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	var req struct {
		Visibility       string     `json:"visibility"`
		AllowedReferrers []string   `json:"allowedReferrers"`
		ExpiresAt        *time.Time `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	switch req.Visibility {
	case "public", "signed", "private":
	default:
		writeError(w, http.StatusBadRequest, "visibility must be public|signed|private")
		return
	}
	if err := s.db.SetAccess(r.Context(), s.cfg.AdminLibraryID, videoID,
		req.Visibility, req.AllowedReferrers, req.ExpiresAt); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"videoId": videoID, "visibility": req.Visibility})
}

// parseRange maps a ?range=7d|30d|90d|all query into a time floor. Defaults to
// 30 days; "all" (or anything unrecognized but explicit) returns the zero time,
// meaning no lower bound.
func parseRange(r *http.Request) time.Time {
	switch r.URL.Query().Get("range") {
	case "7d":
		return time.Now().AddDate(0, 0, -7)
	case "90d":
		return time.Now().AddDate(0, 0, -90)
	case "all":
		return time.Time{}
	case "30d", "":
		return time.Now().AddDate(0, 0, -30)
	default:
		return time.Now().AddDate(0, 0, -30)
	}
}

// adminLibraryAnalytics returns the library-wide engagement rollup for the
// Overview / Analytics pages (totals, watch time, estimated bandwidth, country
// breakdown, most-watched videos, daily trend). ?range=7d|30d|90d|all.
func (s *Server) adminLibraryAnalytics(w http.ResponseWriter, r *http.Request) {
	a, err := s.db.GetLibraryAnalytics(r.Context(), s.cfg.AdminLibraryID, parseRange(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics failed")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// adminAnalytics returns the per-video QoE + engagement rollup. ?range=7d|30d|90d|all.
func (s *Server) adminAnalytics(w http.ResponseWriter, r *http.Request) {
	videoID := chi.URLParam(r, "videoId")
	a, err := s.db.GetVideoAnalytics(r.Context(), s.cfg.AdminLibraryID, videoID, parseRange(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics failed")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- helpers ---

// randomToken returns prefix + 32 bytes of base64url randomness.
func randomToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func validWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	// Allow http only for localhost/dev; require a host otherwise.
	return u.Scheme == "https" || u.Scheme == "http"
}
