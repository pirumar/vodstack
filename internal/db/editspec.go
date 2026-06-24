package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/pirumar/vodstack/internal/video"
)

// GetEditSpec returns the video's stored edit decision list, or nil if none.
// Returns ErrNotFound if the video is gone.
func (d *DB) GetEditSpec(ctx context.Context, videoID string) (*video.EditSpec, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT edit_spec FROM videos WHERE id=$1`, videoID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return video.ParseEditSpec(raw)
}

// SetEditSpec persists (or clears, when spec is nil) a video's edit decision list.
func (d *DB) SetEditSpec(ctx context.Context, videoID string, spec *video.EditSpec) error {
	if spec == nil {
		return d.exec(ctx,
			`UPDATE videos SET edit_spec=NULL, updated_at=now() WHERE id=$1`, videoID)
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE videos SET edit_spec=$2, updated_at=now() WHERE id=$1`, videoID, raw)
}
