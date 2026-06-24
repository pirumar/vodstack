// Command api is the vodstack control-plane HTTP server.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pirumar/vodstack/internal/auth"
	"github.com/pirumar/vodstack/internal/config"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/httpapi"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/token"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	seedLibrary(ctx, database, cfg)

	store, err := storage.New(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioUseSSL, cfg.MinioBucket)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		log.Fatalf("ensure bucket: %v", err)
	}

	q := queue.NewClient(cfg.RedisAddr)
	defer q.Close()

	signer := token.NewSigner(cfg.TokenSecret)

	srv := httpapi.NewServer(cfg, database, store, q, signer)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("vodstack api listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	log.Println("api shut down")
}

// seedLibrary creates a dev library from env (SEED_LIBRARY_ID / API_KEY) so the
// service is usable out of the box. No-op if either value is empty.
func seedLibrary(ctx context.Context, database *db.DB, cfg *config.Config) {
	if cfg.SeedLibraryID == "" || cfg.SeedLibraryAPIKey == "" {
		return
	}
	if err := database.UpsertLibrary(ctx, cfg.SeedLibraryID, "Default (seeded)", auth.HashAPIKey(cfg.SeedLibraryAPIKey)); err != nil {
		log.Printf("seed library: %v", err)
		return
	}
	log.Printf("seeded library %q", cfg.SeedLibraryID)
}
