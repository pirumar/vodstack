package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pirumar/vodstack/internal/llm"
)

// GetLLMConfig returns a library's LLM router settings (libraries.llm_config
// JSONB). An empty stored config yields the defaults. ErrNotFound if the library
// is gone.
func (d *DB) GetLLMConfig(ctx context.Context, libraryID string) (llm.Config, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT llm_config FROM libraries WHERE id=$1`, libraryID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return llm.Config{}, ErrNotFound
	}
	if err != nil {
		return llm.Config{}, err
	}
	cfg := llm.DefaultConfig()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg) // tolerate partial/legacy JSON; Normalize repairs it
	}
	cfg.Normalize()
	return cfg, nil
}

// SetLLMConfig persists a library's LLM settings (already normalized).
func (d *DB) SetLLMConfig(ctx context.Context, libraryID string, cfg llm.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE libraries SET llm_config=$2 WHERE id=$1`, libraryID, raw)
}
