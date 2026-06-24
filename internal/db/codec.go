package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetEncodeProfile returns a video's encode profile ('h264' or 'h264+av1').
func (d *DB) GetEncodeProfile(ctx context.Context, id string) (string, error) {
	var p string
	err := d.pool.QueryRow(ctx, `SELECT encode_profile FROM videos WHERE id=$1`, id).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return p, err
}

// SetEncodeProfile updates the encode profile (scoped to the library).
func (d *DB) SetEncodeProfile(ctx context.Context, libraryID, id, profile string) error {
	tag, err := d.pool.Exec(ctx,
		`UPDATE videos SET encode_profile=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, profile)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAV1Resolutions records the AV1 ladder once the backfill completes.
func (d *DB) SetAV1Resolutions(ctx context.Context, id, resolutions string) error {
	return d.exec(ctx,
		`UPDATE videos SET av1_resolutions=$2, updated_at=now() WHERE id=$1`, id, resolutions)
}
