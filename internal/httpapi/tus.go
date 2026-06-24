package httpapi

import (
	"fmt"
	"net/http"
	"os"

	xslog "golang.org/x/exp/slog" // tusd v2 still uses the experimental slog

	"github.com/tus/tusd/v2/pkg/filestore"
	tushandler "github.com/tus/tusd/v2/pkg/handler"

	"github.com/pirumar/vodstack/internal/auth"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/video"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// tusBasePath is where the resumable-upload protocol is mounted. It sits OUTSIDE
// the library group (auth happens in the pre-create hook) and outside the global
// request timeout (uploads can take a long time).
const tusBasePath = "/tus/"

// newTusHandler builds the routed resumable-upload handler. tusd's router keys
// off the request path with the base path ALREADY stripped (it does
// strings.Trim(path,"/") and treats ""==creation), so server.go mounts this
// behind http.StripPrefix("/tus", …). Chunks land on local scratch (filestore);
// on completion the finished file is streamed to MinIO under the canonical raw
// key and a transcode is enqueued — the same finalize the legacy admin PUT and
// presigned-complete paths do.
func (s *Server) newTusHandler() (*tushandler.Handler, error) {
	if err := os.MkdirAll(s.cfg.TusDir, 0o777); err != nil {
		return nil, fmt.Errorf("tus dir: %w", err)
	}
	store := filestore.New(s.cfg.TusDir)
	composer := tushandler.NewStoreComposer()
	store.UseIn(composer)

	return tushandler.NewHandler(tushandler.Config{
		BasePath:      tusBasePath, // used to build Location URLs (/tus/<id>)
		StoreComposer: composer,
		Logger:        xslog.Default(),
		// Behind Cloudflare Tunnel + the web nginx, the request reaches tusd over
		// plain HTTP, so without this tusd builds an http:// Location header — which
		// the HTTPS panel rejects as mixed content and uploads fail. Respect the
		// X-Forwarded-Proto/Host that Cloudflare sets so the Location is https://.
		RespectForwardedHeaders:   true,
		PreUploadCreateCallback:   s.tusPreCreate,
		PreFinishResponseCallback: s.tusPreFinish,
	})
}

// tusPreCreate authenticates the creator and validates the target video before an
// upload is allowed. Auth mirrors the rest of the API: a valid admin session
// cookie (panel) OR a library API key (public). The resolved library id is
// written back into the upload metadata for the finish hook.
func (s *Server) tusPreCreate(hook tushandler.HookEvent) (tushandler.HTTPResponse, tushandler.FileInfoChanges, error) {
	meta := hook.Upload.MetaData
	videoID := meta["videoId"]
	if videoID == "" {
		return forbid("videoId metadata is required")
	}

	libraryID, ok := s.tusAuth(hook, meta["libraryId"])
	if !ok {
		return forbid("unauthorized")
	}

	// The video must exist in this library (and not be trashed).
	if _, err := s.db.GetVideo(hook.Context, libraryID, videoID); err != nil {
		return forbid("video not found")
	}

	// Persist the resolved identity so the finish hook does not re-derive it.
	changes := tushandler.FileInfoChanges{MetaData: tushandler.MetaData{
		"videoId":   videoID,
		"libraryId": libraryID,
		"filetype":  meta["filetype"],
	}}
	return tushandler.HTTPResponse{}, changes, nil
}

// tusAuth resolves the library the request is authorized for. Returns false if
// neither an admin session nor a valid API key is presented.
func (s *Server) tusAuth(hook tushandler.HookEvent, metaLibrary string) (string, bool) {
	r := &http.Request{Header: hook.HTTPRequest.Header}

	// Admin session cookie -> the admin library.
	if c, err := r.Cookie(sessionCookie); err == nil && s.signer.ValidSession(c.Value) {
		return s.cfg.AdminLibraryID, true
	}

	// Otherwise an API key for the metadata-named library.
	if metaLibrary == "" {
		return "", false
	}
	key := bearerToken(r)
	if key == "" {
		return "", false
	}
	lib, err := s.db.GetLibrary(hook.Context, metaLibrary)
	if err != nil {
		return "", false
	}
	keyHash := auth.HashAPIKey(key)
	if _, err := s.db.LookupAPIKey(hook.Context, metaLibrary, keyHash); err == nil {
		return metaLibrary, true
	}
	if lib.APIKeyHash != "" && auth.Equal(keyHash, lib.APIKeyHash) {
		return metaLibrary, true
	}
	return "", false
}

// tusPreFinish runs when the upload is complete: move it to MinIO and enqueue the
// transcode, then clean up the local chunks.
func (s *Server) tusPreFinish(hook tushandler.HookEvent) (tushandler.HTTPResponse, error) {
	ctx := hook.Context
	meta := hook.Upload.MetaData
	videoID := meta["videoId"]
	libraryID := meta["libraryId"]
	localPath := hook.Upload.Storage["Path"]
	if videoID == "" || libraryID == "" || localPath == "" {
		return tushandler.HTTPResponse{}, fmt.Errorf("incomplete upload metadata")
	}

	ct := meta["filetype"]
	if ct == "" {
		ct = "application/octet-stream"
	}

	object := storage.RawObjectKey(videoID)
	if err := s.store.UploadFile(ctx, object, localPath, ct); err != nil {
		return tushandler.HTTPResponse{}, fmt.Errorf("store upload: %w", err)
	}
	if err := s.db.SetUploaded(ctx, videoID, object); err != nil {
		return tushandler.HTTPResponse{}, fmt.Errorf("mark uploaded: %w", err)
	}
	if err := s.queue.EnqueueTranscode(queue.TranscodePayload{
		VideoID: videoID, LibraryID: libraryID, SourceObject: object,
	}); err != nil {
		return tushandler.HTTPResponse{}, fmt.Errorf("enqueue: %w", err)
	}
	s.hooks.Dispatch(ctx, webhooks.Event{
		Type:      webhooks.EventVideoUploaded,
		LibraryID: libraryID,
		VideoID:   videoID,
		Data:      map[string]any{"status": int(video.StatusUploaded), "source": "tus"},
	})

	// Local chunks are now redundant.
	_ = os.Remove(localPath)
	_ = os.Remove(localPath + ".info")
	return tushandler.HTTPResponse{}, nil
}

func forbid(msg string) (tushandler.HTTPResponse, tushandler.FileInfoChanges, error) {
	return tushandler.HTTPResponse{StatusCode: http.StatusForbidden, Body: msg},
		tushandler.FileInfoChanges{}, fmt.Errorf("%s", msg)
}
