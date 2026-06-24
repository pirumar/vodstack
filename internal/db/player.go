package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pirumar/vodstack/internal/player"
)

// GetPlayerConfig returns a library's player customization. A library with no
// stored settings (the default empty '{}') yields the server defaults, since
// Normalize fills every zero field. Returns ErrNotFound if the library is gone.
func (d *DB) GetPlayerConfig(ctx context.Context, libraryID string) (player.Config, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT player_config FROM libraries WHERE id=$1`, libraryID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return player.Config{}, ErrNotFound
	}
	if err != nil {
		return player.Config{}, err
	}
	var cfg player.Config
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg) // tolerate partial/legacy JSON; Normalize repairs it
	}
	cfg.Normalize()
	return cfg, nil
}

// SetPlayerConfig persists a library's player customization (already normalized
// by the caller).
func (d *DB) SetPlayerConfig(ctx context.Context, libraryID string, cfg player.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE libraries SET player_config=$2 WHERE id=$1`, libraryID, raw)
}
