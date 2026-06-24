package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// APIKey is a revocable per-library credential. The plaintext key is only ever
// held by the caller; we store the hash (internal/auth.HashAPIKey).
type APIKey struct {
	ID         string     `json:"id"`
	LibraryID  string     `json:"libraryId"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// CreateAPIKey inserts a new key for a library.
func (d *DB) CreateAPIKey(ctx context.Context, id, libraryID, keyHash, name string, scopes []string) error {
	if scopes == nil {
		scopes = []string{}
	}
	_, err := d.pool.Exec(ctx, `
		INSERT INTO api_keys (id, library_id, key_hash, name, scopes)
		VALUES ($1, $2, $3, $4, $5)`,
		id, libraryID, keyHash, name, scopes)
	return err
}

// APIKeyLookup is the minimal identity resolved from a presented key.
type APIKeyLookup struct {
	ID        string
	LibraryID string
	Scopes    []string
}

// LookupAPIKey resolves a (non-revoked) key by its hash. The library id must
// match so a key cannot be used against another tenant's path.
func (d *DB) LookupAPIKey(ctx context.Context, libraryID, keyHash string) (*APIKeyLookup, error) {
	var k APIKeyLookup
	err := d.pool.QueryRow(ctx, `
		SELECT id, library_id, scopes
		FROM api_keys
		WHERE key_hash=$1 AND library_id=$2 AND revoked_at IS NULL`,
		keyHash, libraryID,
	).Scan(&k.ID, &k.LibraryID, &k.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// TouchAPIKey records last use. Best-effort; callers ignore the error.
func (d *DB) TouchAPIKey(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return err
}

// ListAPIKeys returns a library's keys (no hashes), newest first.
func (d *DB) ListAPIKeys(ctx context.Context, libraryID string) ([]APIKey, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, name, scopes, created_at, last_used_at, revoked_at
		FROM api_keys WHERE library_id=$1 ORDER BY created_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.LibraryID, &k.Name, &k.Scopes,
			&k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey marks a key revoked (idempotent). Scoped to the library so one
// tenant cannot revoke another's key.
func (d *DB) RevokeAPIKey(ctx context.Context, libraryID, id string) error {
	tag, err := d.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND library_id=$2 AND revoked_at IS NULL`,
		id, libraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
