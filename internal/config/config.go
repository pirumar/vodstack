// Package config loads service configuration from environment variables.
// Both the API and the worker share this struct; each reads the subset it needs.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr string

	DatabaseURL string
	RedisAddr   string

	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
	MinioBucket    string

	PublicBaseURL string
	TokenSecret   string
	UploadURLTTL  int // seconds

	// EmbedBaseURL is the externally reachable API base used to build <iframe>
	// embed snippets (e.g. https://stream.example.com). Empty disables the
	// copy-embed button's absolute URL (it falls back to a relative path).
	EmbedBaseURL string

	// KeyBaseURL is the externally reachable API base baked into AES-128 playlist
	// key URIs (the player fetches the key from KeyBaseURL/keys/<lib>/<id>).
	KeyBaseURL string

	ScratchDir        string
	TusDir            string // where tus stores in-flight resumable-upload chunks
	WorkerConcurrency int
	FFmpegBin         string
	FFprobeBin        string

	// WhisperURL is the faster-whisper ASR sidecar (auto-captions). Empty
	// disables auto-captioning.
	WhisperURL string

	SeedLibraryID     string
	SeedLibraryAPIKey string

	// Admin panel (BFF). The panel operates on AdminLibraryID server-side; the
	// library API key is never exposed to the browser.
	AdminPassword   string
	AdminLibraryID  string
	AdminSessionTTL int // seconds

	// Worker ops.
	MetricsAddr           string // worker /metrics listen address
	TrashRetentionDays    int    // days a soft-deleted video stays recoverable
	PlaybackRetentionDays int    // days of viewer analytics events to keep

	// Viewer tokens + per-viewer progress (resume / watch history).
	ViewerTokenTTL              int // default viewer-token lifetime, seconds
	ViewerTokenMaxTTL           int // hard clamp on requested ttl, seconds
	ViewerProgressRetentionDays int // days of per-viewer progress to keep (0 = forever)

	// Per-API-key rate limiting on the library API (token bucket).
	RateLimitRPS   float64 // sustained requests/sec per credential
	RateLimitBurst int     // burst capacity
}

// Load reads configuration from the environment, applying sane defaults for
// local development. Required values without a default cause an error.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:          env("HTTP_ADDR", ":8080"),
		DatabaseURL:       env("DATABASE_URL", ""),
		RedisAddr:         env("REDIS_ADDR", "localhost:6379"),
		MinioEndpoint:     env("MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey:    env("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey:    env("MINIO_SECRET_KEY", "minioadmin"),
		MinioUseSSL:       envBool("MINIO_USE_SSL", false),
		MinioBucket:       env("MINIO_BUCKET", "vodstack-videos"),
		PublicBaseURL:     env("PUBLIC_BASE_URL", "http://localhost:8081"),
		TokenSecret:       env("TOKEN_SECRET", "dev-insecure-change-me"),
		UploadURLTTL:      envInt("UPLOAD_URL_TTL", 3600),
		EmbedBaseURL:      env("EMBED_BASE_URL", ""),
		KeyBaseURL:        env("KEY_BASE_URL", ""),
		ScratchDir:        env("SCRATCH_DIR", "./scratch"),
		TusDir:            env("TUS_DIR", "./scratch/tus"),
		WorkerConcurrency: envInt("WORKER_CONCURRENCY", 2),
		FFmpegBin:         env("FFMPEG_BIN", "ffmpeg"),
		FFprobeBin:        env("FFPROBE_BIN", "ffprobe"),
		WhisperURL:        env("WHISPER_URL", ""),
		SeedLibraryID:     env("SEED_LIBRARY_ID", ""),
		SeedLibraryAPIKey: env("SEED_LIBRARY_API_KEY", ""),
		AdminPassword:      env("ADMIN_PASSWORD", ""),
		AdminLibraryID:     env("ADMIN_LIBRARY_ID", env("SEED_LIBRARY_ID", "default")),
		AdminSessionTTL:    envInt("ADMIN_SESSION_TTL", 86400),
		MetricsAddr:           env("METRICS_ADDR", ":9091"),
		TrashRetentionDays:    envInt("TRASH_RETENTION_DAYS", 15),
		PlaybackRetentionDays: envInt("PLAYBACK_RETENTION_DAYS", 90),
		ViewerTokenTTL:              envInt("VIEWER_TOKEN_TTL", 21600),     // 6h, covers a long session
		ViewerTokenMaxTTL:           envInt("VIEWER_TOKEN_MAX_TTL", 86400), // 24h cap
		ViewerProgressRetentionDays: envInt("VIEWER_PROGRESS_RETENTION_DAYS", 0),
		RateLimitRPS:       envFloat("RATE_LIMIT_RPS", 20),
		RateLimitBurst:     envInt("RATE_LIMIT_BURST", 40),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
