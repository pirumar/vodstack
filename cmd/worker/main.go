// Command worker consumes transcode jobs and produces HLS output.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/config"
	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/metrics"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/storage"
	"github.com/pirumar/vodstack/internal/transcode"
	"github.com/pirumar/vodstack/internal/webhooks"
	"github.com/pirumar/vodstack/internal/worker"
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

	// Apply migrations too (idempotent) so the worker never races the API's
	// schema setup on a cold start.
	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	store, err := storage.New(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioUseSSL, cfg.MinioBucket)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	tc := transcode.New(cfg.FFmpegBin, cfg.FFprobeBin)

	// The worker both delivers webhooks and emits encode-result events, so it
	// needs an enqueue client + dispatcher of its own.
	q := queue.NewClient(cfg.RedisAddr)
	defer q.Close()
	wh := webhooks.NewDispatcher(database, q)

	wk := worker.New(cfg, database, store, tc, wh, q)

	// Expose /metrics for Prometheus.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		log.Printf("worker metrics on %s", cfg.MetricsAddr)
		if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()

	// Periodically purge expired trash + prune old analytics (and once at startup).
	go func() {
		wk.PurgeExpiredTrash(ctx)
		wk.PrunePlaybackEvents(ctx)
		wk.PruneViewerProgress(ctx)
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			wk.PurgeExpiredTrash(ctx)
			wk.PrunePlaybackEvents(ctx)
			wk.PruneViewerProgress(ctx)
		}
	}()

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		asynq.Config{
			Concurrency: cfg.WorkerConcurrency,
			// Default queue drains before bulk so admin uploads beat migration.
			// Webhooks ride their own lane so deliveries and transcodes never
			// starve each other.
			Queues: map[string]int{
				queue.QueueDefault:  6,
				queue.QueueWebhooks: 3,
				queue.QueueBulk:     1,
			},
		},
	)

	log.Printf("vodstack worker started (concurrency=%d)", cfg.WorkerConcurrency)
	if err := srv.Run(wk.Mux()); err != nil {
		log.Fatalf("worker: %v", err)
	}
}
