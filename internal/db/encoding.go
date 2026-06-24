package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pirumar/vodstack/internal/encoding"
)

// GetEncodingConfig returns a library's default encoding settings. An empty
// stored object yields server defaults (Normalize fills every field). Returns
// ErrNotFound if the library is gone.
func (d *DB) GetEncodingConfig(ctx context.Context, libraryID string) (encoding.Config, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT encoding_config FROM libraries WHERE id=$1`, libraryID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return encoding.Config{}, ErrNotFound
	}
	if err != nil {
		return encoding.Config{}, err
	}
	cfg := decodeEncodingConfig(raw)
	return cfg, nil
}

// SetEncodingConfig persists a library's default encoding settings (already
// normalized by the caller).
func (d *DB) SetEncodingConfig(ctx context.Context, libraryID string, cfg encoding.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE libraries SET encoding_config=$2 WHERE id=$1`, libraryID, raw)
}

// GetEncodeSettings returns the per-video snapshot of the resolved encoding
// settings (filled with defaults via Normalize). The worker reads this as the
// single source of truth for what to produce.
func (d *DB) GetEncodeSettings(ctx context.Context, videoID string) (encoding.Config, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT encode_settings FROM videos WHERE id=$1`, videoID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return encoding.Config{}, ErrNotFound
	}
	if err != nil {
		return encoding.Config{}, err
	}
	return decodeEncodingConfig(raw), nil
}

// SetEncodeSettings overwrites a video's encode settings snapshot.
func (d *DB) SetEncodeSettings(ctx context.Context, videoID string, cfg encoding.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE videos SET encode_settings=$2, updated_at=now() WHERE id=$1`, videoID, raw)
}

// AddVideoCodec appends a codec to a video's encode_settings.Codecs (idempotent)
// and returns the normalized config. Used by the admin "produce AV1/HEVC/VP9"
// triggers, which add one codec to an existing video.
func (d *DB) AddVideoCodec(ctx context.Context, libraryID, videoID, codec string) (encoding.Config, error) {
	cfg, err := d.GetEncodeSettings(ctx, videoID)
	if err != nil {
		return encoding.Config{}, err
	}
	if !cfg.HasCodec(codec) {
		cfg.Codecs = append(cfg.Codecs, codec)
		cfg.Normalize()
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return encoding.Config{}, err
	}
	if err := d.exec(ctx,
		`UPDATE videos SET encode_settings=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		videoID, libraryID, raw); err != nil {
		return encoding.Config{}, err
	}
	return cfg, nil
}

// decodeEncodingConfig unmarshals a stored JSONB blob into a normalized Config,
// tolerating empty/partial/legacy data.
func decodeEncodingConfig(raw []byte) encoding.Config {
	var cfg encoding.Config
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg) // tolerate partial/legacy JSON; Normalize repairs it
	}
	cfg.Normalize()
	return cfg
}
