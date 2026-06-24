// Package httpapi is the control-plane HTTP server: video CRUD, presigned
// uploads, transcode enqueue, and signed play URLs.
package httpapi

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/pirumar/vodstack/internal/config"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/metrics"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/token"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// Server bundles the dependencies the handlers need.
type Server struct {
	cfg     *config.Config
	db      *db.DB
	store   *storage.Store
	queue   *queue.Client
	signer  *token.Signer
	limiter *rateLimiter
	hooks   *webhooks.Dispatcher
}

func NewServer(cfg *config.Config, database *db.DB, store *storage.Store, q *queue.Client, signer *token.Signer) *Server {
	return &Server{
		cfg:     cfg,
		db:      database,
		store:   store,
		queue:   q,
		signer:  signer,
		limiter: newRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		hooks:   webhooks.NewDispatcher(database, q),
	}
}

// Router builds the HTTP routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(metrics.Middleware)

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Handle("/metrics", metrics.Handler())

	// Resumable uploads (tus). Mounted WITHOUT the request timeout — uploads can
	// run far longer than 60s — and with auth handled in its pre-create hook.
	// StripPrefix("/tus") because tusd's router expects the base path already
	// removed (it treats an empty trimmed path as the creation endpoint).
	if tus, err := s.newTusHandler(); err != nil {
		log.Printf("tus disabled: %v", err)
	} else {
		stripped := http.StripPrefix("/tus", tus)
		r.Handle("/tus", stripped)
		r.Handle("/tus/*", stripped)
	}

	// Public embeddable iframe player (no auth; mints a short token server-side).
	r.Get("/embed/{libraryId}/{videoId}", s.handleEmbed)
	r.Get("/embed/{libraryId}/{videoId}/play", s.handleEmbedPlay)
	r.Get("/embed/{libraryId}/{videoId}/heatmap", s.handleHeatmap)
	r.Get("/embed/{libraryId}/{videoId}/progress", s.handleEmbedProgress)
	r.Get("/embed/{libraryId}/{videoId}/search", s.handleEmbedSearch)

	// Public viewer-analytics beacon (no auth; CORS-open; IP-rate-limited).
	r.Post("/beacon", s.handleBeacon)
	r.Options("/beacon", s.handleBeacon)

	// AES-128 content-key delivery (gated by the playback token, not cached).
	r.Get("/keys/{libraryId}/{videoId}", s.handleKey)
	r.Options("/keys/{libraryId}/{videoId}", s.handleKey)

	// Everything below shares a 60s request timeout.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))

		// Admin BFF for the web panel (session-cookie auth).
		r.Route("/admin", s.adminRoutes)

		r.Route("/api/library/{libraryId}", func(r chi.Router) {
			r.Use(s.libraryAuth) // every library route requires a valid API key
			r.Use(s.rateLimit)   // then a per-credential token bucket

			r.Post("/videos", s.handleCreateVideo)
			r.Route("/videos/{videoId}", func(r chi.Router) {
				r.Get("/", s.handleGetVideo)
				r.Post("/upload-url", s.handleUploadURL)
				r.Post("/complete", s.handleComplete)
				r.Post("/fetch", s.handleFetch)
				r.Get("/play", s.handlePlay)
				r.Get("/download", s.handleDownload)
				r.Put("/folder", s.handleMoveVideo)
				r.Delete("/", s.handleDeleteVideo)
				r.Get("/viewer-progress", s.handleVideoViewerProgress)
			})

			// In-video search over this library's transcripts.
			r.Get("/search", s.handleLibrarySearch)

			// Viewer identity: mint per-viewer tokens + query watch history.
			r.Post("/viewer-token", s.handleMintViewerToken)
			r.Get("/viewers/{viewerId}/history", s.handleViewerHistory)

			// Folders: nested organization of this library's videos.
			r.Get("/folders", s.handleListFolders)
			r.Post("/folders", s.handleCreateFolder)
			r.Put("/folders/{folderId}", s.handleUpdateFolder)
			r.Delete("/folders/{folderId}", s.handleDeleteFolder)
		})
	})

	return r
}
