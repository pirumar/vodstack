package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- Content keys (AES-128) ---

type ContentKey struct {
	KeyID  string
	KeyHex string
	IVHex  string
}

// SaveContentKey stores a video's AES key + IV and marks the video encrypted.
func (d *DB) SaveContentKey(ctx context.Context, keyID, libraryID, videoID, keyHex, ivHex string) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO content_keys (key_id, library_id, video_id, key_hex, iv_hex)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (key_id) DO UPDATE SET key_hex=EXCLUDED.key_hex, iv_hex=EXCLUDED.iv_hex`,
		keyID, libraryID, videoID, keyHex, ivHex); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE videos SET encryption_mode='aes128', key_id=$3, updated_at=now()
		 WHERE id=$1 AND library_id=$2`, videoID, libraryID, keyID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetContentKeyForVideo returns the AES key for a video (used by the key
// endpoint after it has validated the playback token).
func (d *DB) GetContentKeyForVideo(ctx context.Context, videoID string) (*ContentKey, error) {
	var k ContentKey
	err := d.pool.QueryRow(ctx, `
		SELECT key_id, key_hex, iv_hex FROM content_keys WHERE video_id=$1
		ORDER BY created_at DESC LIMIT 1`, videoID,
	).Scan(&k.KeyID, &k.KeyHex, &k.IVHex)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// --- Access control ---

type VideoAccess struct {
	Visibility       string     `json:"visibility"`
	AllowedReferrers []string   `json:"allowedReferrers"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}

// GetAccess returns a video's access policy (scoped to the library).
func (d *DB) GetAccess(ctx context.Context, libraryID, videoID string) (*VideoAccess, error) {
	var a VideoAccess
	err := d.pool.QueryRow(ctx, `
		SELECT visibility, allowed_referrers, expires_at
		FROM videos WHERE id=$1 AND library_id=$2 AND deleted_at IS NULL`,
		videoID, libraryID,
	).Scan(&a.Visibility, &a.AllowedReferrers, &a.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SetAccess updates a video's access policy.
func (d *DB) SetAccess(ctx context.Context, libraryID, videoID, visibility string, referrers []string, expiresAt *time.Time) error {
	if referrers == nil {
		referrers = []string{}
	}
	tag, err := d.pool.Exec(ctx, `
		UPDATE videos SET visibility=$3, allowed_referrers=$4, expires_at=$5, updated_at=now()
		WHERE id=$1 AND library_id=$2`,
		videoID, libraryID, visibility, referrers, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
