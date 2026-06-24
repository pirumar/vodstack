// Package db is the PostgreSQL data-access layer (pgx). It owns the video
// lifecycle state machine and library/API-key lookups. Queries are hand-written
// against pgxpool to keep the build free of code-generation tooling.
package db

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pirumar/vodstack/internal/video"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var ErrNotFound = errors.New("not found")

type DB struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	// Verify connectivity early with a bounded retry so we tolerate the DB
	// container coming up slightly after us in docker-compose.
	var pingErr error
	for i := 0; i < 30; i++ {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr = pool.Ping(c)
		cancel()
		if pingErr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", pingErr)
	}
	return &DB{pool: pool}, nil
}

func (d *DB) Close() { d.pool.Close() }

// migrateLockKey is an arbitrary fixed key for the session-level advisory lock
// that serializes migration runs (the API and worker both call Migrate on cold
// start; without this they race on concurrent CREATE TABLE IF NOT EXISTS, which
// Postgres does not serialize and which fails on pg_type).
const migrateLockKey int64 = 0x667A656D6967 // "fzemig"

// Migrate applies any embedded migrations that have not yet run. It is a
// minimal forward-only migrator: each *.sql file runs once, tracked in
// schema_migrations. Files are applied in lexical order. A Postgres advisory
// lock makes concurrent callers wait rather than race.
func (d *DB) Migrate(ctx context.Context) error {
	// Hold the advisory lock on a dedicated connection for the whole run so a
	// second process blocks here until the first finishes (then finds nothing
	// to do).
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migrate conn: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("acquire migrate lock: %w", err)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateLockKey)

	_, err = d.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := d.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := d.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// --- Libraries ---

type Library struct {
	ID         string
	Name       string
	APIKeyHash string
}

// UpsertLibrary creates or updates a library (used for the dev seed).
func (d *DB) UpsertLibrary(ctx context.Context, id, name, apiKeyHash string) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO libraries (id, name, api_key_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, api_key_hash = EXCLUDED.api_key_hash`,
		id, name, apiKeyHash)
	return err
}

// CreateLibrary provisions a new tenant. api_key_hash is left empty (the legacy
// seed-key path); real credentials live in api_keys (see CreateAPIKey).
func (d *DB) CreateLibrary(ctx context.Context, id, name string) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO libraries (id, name, api_key_hash) VALUES ($1, $2, '')`,
		id, name)
	return err
}

// LibraryInfo is a tenant summary with its live (non-revoked) key count.
type LibraryInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	KeyCount   int       `json:"keyCount"`
	VideoCount int       `json:"videoCount"`
}

// ListLibraries returns every tenant with key/video counts (newest first).
func (d *DB) ListLibraries(ctx context.Context) ([]LibraryInfo, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT l.id, l.name, l.created_at,
		       (SELECT count(*) FROM api_keys k WHERE k.library_id=l.id AND k.revoked_at IS NULL),
		       (SELECT count(*) FROM videos v WHERE v.library_id=l.id AND v.deleted_at IS NULL)
		FROM libraries l ORDER BY l.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryInfo
	for rows.Next() {
		var li LibraryInfo
		if err := rows.Scan(&li.ID, &li.Name, &li.CreatedAt, &li.KeyCount, &li.VideoCount); err != nil {
			return nil, err
		}
		out = append(out, li)
	}
	return out, rows.Err()
}

func (d *DB) GetLibrary(ctx context.Context, id string) (*Library, error) {
	var l Library
	err := d.pool.QueryRow(ctx,
		`SELECT id, name, api_key_hash FROM libraries WHERE id=$1`, id,
	).Scan(&l.ID, &l.Name, &l.APIKeyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// --- Videos ---

func (d *DB) CreateVideo(ctx context.Context, id, libraryID, title string, collectionID, folderID *string) (*video.Video, error) {
	// Snapshot the library's current encoding defaults into the video so every
	// transcode/backfill uses the config in effect at upload time, even if the
	// library default changes later. An absent/'{}' library config snapshots '{}',
	// which Normalize resolves to server defaults on read.
	_, err := d.pool.Exec(ctx, `
		INSERT INTO videos (id, library_id, title, collection_id, folder_id, status, encode_settings)
		VALUES ($1, $2, $3, $4, $5, 0,
		        COALESCE((SELECT encoding_config FROM libraries WHERE id=$2), '{}'::jsonb))`,
		id, libraryID, title, collectionID, folderID)
	if err != nil {
		return nil, err
	}
	return d.GetVideo(ctx, libraryID, id)
}

func (d *DB) GetVideo(ctx context.Context, libraryID, id string) (*video.Video, error) {
	var v video.Video
	var status int
	var chapters, tags []byte
	err := d.pool.QueryRow(ctx, `
		SELECT id, library_id, title, collection_id, folder_id, status, source_object,
		       duration_seconds, width, height, size_bytes, available_resolutions,
		       thumbnail_file, encode_progress, error_message, thumbnails_vtt, chapters,
		       description, tags
		FROM videos WHERE id=$1 AND library_id=$2 AND deleted_at IS NULL`,
		id, libraryID,
	).Scan(&v.ID, &v.LibraryID, &v.Title, &v.CollectionID, &v.FolderID, &status, &v.SourceObject,
		&v.DurationSeconds, &v.Width, &v.Height, &v.SizeBytes, &v.AvailableResolutions,
		&v.ThumbnailFile, &v.EncodeProgress, &v.ErrorMessage, &v.ThumbnailsVTT, &chapters,
		&v.Description, &tags)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Status = video.Status(status)
	v.Chapters = chapters
	v.Tags = parseTags(tags)
	return &v, nil
}

// parseTags decodes a JSONB tags array into a string slice (always non-nil).
func parseTags(raw []byte) []string {
	tags := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &tags)
	}
	return tags
}

// ListVideos returns all videos for a library, newest first.
func (d *DB) ListVideos(ctx context.Context, libraryID string) ([]video.Video, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, title, collection_id, folder_id, status, source_object,
		       duration_seconds, width, height, size_bytes, available_resolutions,
		       thumbnail_file, encode_progress, error_message, thumbnails_vtt, chapters,
		       description, tags
		FROM videos WHERE library_id=$1 AND deleted_at IS NULL
		ORDER BY created_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

// ListVideosInFolder returns the videos directly inside a folder, newest first.
// folderID nil lists videos at the library root (folder_id IS NULL).
func (d *DB) ListVideosInFolder(ctx context.Context, libraryID string, folderID *string) ([]video.Video, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, title, collection_id, folder_id, status, source_object,
		       duration_seconds, width, height, size_bytes, available_resolutions,
		       thumbnail_file, encode_progress, error_message, thumbnails_vtt, chapters,
		       description, tags
		FROM videos
		WHERE library_id=$1 AND deleted_at IS NULL
		  AND ($2::uuid IS NULL AND folder_id IS NULL OR folder_id=$2::uuid)
		ORDER BY created_at DESC`, libraryID, folderID)
	if err != nil {
		return nil, err
	}
	return scanVideos(rows)
}

// scanVideos materializes a video result set whose columns match the standard
// metadata projection (no deleted_at).
func scanVideos(rows pgx.Rows) ([]video.Video, error) {
	defer rows.Close()
	var out []video.Video
	for rows.Next() {
		var v video.Video
		var status int
		var chapters, tags []byte
		if err := rows.Scan(&v.ID, &v.LibraryID, &v.Title, &v.CollectionID, &v.FolderID, &status,
			&v.SourceObject, &v.DurationSeconds, &v.Width, &v.Height, &v.SizeBytes,
			&v.AvailableResolutions, &v.ThumbnailFile, &v.EncodeProgress, &v.ErrorMessage,
			&v.ThumbnailsVTT, &chapters, &v.Description, &tags); err != nil {
			return nil, err
		}
		v.Status = video.Status(status)
		v.Chapters = chapters
		v.Tags = parseTags(tags)
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetUploaded marks the raw object as present and moves to status=1.
func (d *DB) SetUploaded(ctx context.Context, id, sourceObject string) error {
	return d.exec(ctx, `
		UPDATE videos SET source_object=$2, status=1, updated_at=now()
		WHERE id=$1`, id, sourceObject)
}

func (d *DB) SetStatus(ctx context.Context, id string, status video.Status, progress int) error {
	return d.exec(ctx, `
		UPDATE videos SET status=$2, encode_progress=$3, updated_at=now()
		WHERE id=$1`, id, int(status), progress)
}

// FinishInfo carries the probed/encoded metadata written when a video finishes.
type FinishInfo struct {
	DurationSeconds      int
	Width                int
	Height               int
	SizeBytes            int64
	AvailableResolutions string
	ThumbnailFile        string
	ThumbnailsVTT        string
}

func (d *DB) SetFinished(ctx context.Context, id string, info FinishInfo) error {
	var vtt *string
	if info.ThumbnailsVTT != "" {
		vtt = &info.ThumbnailsVTT
	}
	return d.exec(ctx, `
		UPDATE videos SET status=4, encode_progress=100,
		       duration_seconds=$2, width=$3, height=$4, size_bytes=$5,
		       available_resolutions=$6, thumbnail_file=$7, thumbnails_vtt=$8,
		       error_message=NULL, updated_at=now()
		WHERE id=$1`,
		id, info.DurationSeconds, info.Width, info.Height, info.SizeBytes,
		info.AvailableResolutions, info.ThumbnailFile, vtt)
}

// SetChapters stores the chapters JSON (a marshaled []video.Chapter), or NULL.
func (d *DB) SetChapters(ctx context.Context, libraryID, id string, chapters []byte) error {
	tag, err := d.pool.Exec(ctx,
		`UPDATE videos SET chapters=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, chapters)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTitle renames a video. Used by the admin metadata editor.
func (d *DB) SetTitle(ctx context.Context, libraryID, id, title string) error {
	return d.exec(ctx,
		`UPDATE videos SET title=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, title)
}

// SetDescription stores an LLM-generated description/summary.
func (d *DB) SetDescription(ctx context.Context, libraryID, id, description string) error {
	return d.exec(ctx,
		`UPDATE videos SET description=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, description)
}

// SetTags stores LLM-generated content tags (a JSON string array).
func (d *DB) SetTags(ctx context.Context, libraryID, id string, tags []string) error {
	if tags == nil {
		tags = []string{}
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE videos SET tags=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, raw)
}

// SetThumbnail points a video's poster at a new file under its HLS prefix
// (custom poster set by an admin: uploaded image or grabbed frame). Does not
// touch status — the video stays whatever it was.
func (d *DB) SetThumbnail(ctx context.Context, libraryID, id, filename string) error {
	return d.exec(ctx,
		`UPDATE videos SET thumbnail_file=$3, updated_at=now() WHERE id=$1 AND library_id=$2`,
		id, libraryID, filename)
}

// --- Captions ---

func (d *DB) AddCaption(ctx context.Context, id, videoID, lang, label, object string) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO captions (id, video_id, lang, label, object)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (video_id, lang) DO UPDATE SET label=EXCLUDED.label, object=EXCLUDED.object`,
		id, videoID, lang, label, object)
	return err
}

func (d *DB) ListCaptions(ctx context.Context, videoID string) ([]video.Caption, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT lang, label, object FROM captions WHERE video_id=$1 ORDER BY lang`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []video.Caption
	for rows.Next() {
		var c video.Caption
		if err := rows.Scan(&c.Lang, &c.Label, &c.Object); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) DeleteCaption(ctx context.Context, videoID, lang string) (string, error) {
	var object string
	err := d.pool.QueryRow(ctx,
		`DELETE FROM captions WHERE video_id=$1 AND lang=$2 RETURNING object`, videoID, lang,
	).Scan(&object)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return object, err
}

func (d *DB) SetFailed(ctx context.Context, id, msg string) error {
	return d.exec(ctx, `
		UPDATE videos SET status=5, error_message=$2, updated_at=now()
		WHERE id=$1`, id, msg)
}

// SoftDelete moves a video to the trash (recoverable). It stops being usable
// because GetVideo/ListVideos filter out deleted rows, so no new play tokens
// are issued.
func (d *DB) SoftDelete(ctx context.Context, libraryID, id string) error {
	return d.exec(ctx, `
		UPDATE videos SET deleted_at=now(), updated_at=now()
		WHERE id=$1 AND library_id=$2 AND deleted_at IS NULL`, id, libraryID)
}

// Restore brings a trashed video back.
func (d *DB) Restore(ctx context.Context, libraryID, id string) error {
	return d.exec(ctx, `
		UPDATE videos SET deleted_at=NULL, updated_at=now()
		WHERE id=$1 AND library_id=$2 AND deleted_at IS NOT NULL`, id, libraryID)
}

// PurgeVideo hard-deletes a video row (caller purges its objects). Works whether
// or not it is trashed.
func (d *DB) PurgeVideo(ctx context.Context, libraryID, id string) error {
	tag, err := d.pool.Exec(ctx,
		`DELETE FROM videos WHERE id=$1 AND library_id=$2`, id, libraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTrashed returns the trashed videos for a library, newest-deleted first.
func (d *DB) ListTrashed(ctx context.Context, libraryID string) ([]video.Video, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, title, collection_id, folder_id, status, source_object,
		       duration_seconds, width, height, size_bytes, available_resolutions,
		       thumbnail_file, encode_progress, error_message, thumbnails_vtt, chapters,
		       description, tags, deleted_at
		FROM videos WHERE library_id=$1 AND deleted_at IS NOT NULL
		ORDER BY deleted_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []video.Video
	for rows.Next() {
		var v video.Video
		var status int
		var chapters, tags []byte
		if err := rows.Scan(&v.ID, &v.LibraryID, &v.Title, &v.CollectionID, &v.FolderID, &status,
			&v.SourceObject, &v.DurationSeconds, &v.Width, &v.Height, &v.SizeBytes,
			&v.AvailableResolutions, &v.ThumbnailFile, &v.EncodeProgress, &v.ErrorMessage,
			&v.ThumbnailsVTT, &chapters, &v.Description, &tags, &v.DeletedAt); err != nil {
			return nil, err
		}
		v.Status = video.Status(status)
		v.Chapters = chapters
		v.Tags = parseTags(tags)
		out = append(out, v)
	}
	return out, rows.Err()
}

// TrashEntry identifies an expired trashed video to purge.
type TrashEntry struct {
	ID        string
	LibraryID string
}

// ListExpiredTrash returns videos trashed longer ago than retention.
func (d *DB) ListExpiredTrash(ctx context.Context, retention time.Duration) ([]TrashEntry, error) {
	cutoff := time.Now().Add(-retention)
	rows, err := d.pool.Query(ctx,
		`SELECT id, library_id FROM videos WHERE deleted_at IS NOT NULL AND deleted_at < $1`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrashEntry
	for rows.Next() {
		var e TrashEntry
		if err := rows.Scan(&e.ID, &e.LibraryID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Advanced-operation kinds (one row per (video, kind) in video_operations) and
// their lifecycle states. Shared by the API (sets queued), worker (sets
// running/done/failed), and the operations status endpoint.
const (
	OpKindAV1         = "av1"
	OpKindHEVC        = "hevc"
	OpKindVP9         = "vp9"
	OpKindCaption     = "caption"
	OpKindAIContent   = "ai_content"
	OpKindEncrypt     = "encrypt"
	OpKindSearchIndex = "search_index"
	OpKindPoster      = "poster"

	OpQueued  = "queued"
	OpRunning = "running"
	OpDone    = "done"
	OpFailed  = "failed"
)

// VideoOperation is the live status of one advanced operation for a video.
type VideoOperation struct {
	Kind      string  `json:"kind"`
	Status    string  `json:"status"`
	Error     *string `json:"error,omitempty"`
	UpdatedAt string  `json:"updatedAt"`
}

// SetOperationStatus upserts the current status of one (video, kind) operation.
// errMsg is stored only for the failed state; pass "" otherwise.
func (d *DB) SetOperationStatus(ctx context.Context, videoID, kind, status, errMsg string) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO video_operations (video_id, kind, status, error, updated_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), now())
		ON CONFLICT (video_id, kind)
		DO UPDATE SET status = EXCLUDED.status, error = EXCLUDED.error, updated_at = now()`,
		videoID, kind, status, errMsg)
	return err
}

// GetVideoOperations returns the recorded status of every advanced operation
// that has been triggered for the video (empty slice if none).
func (d *DB) GetVideoOperations(ctx context.Context, videoID string) ([]VideoOperation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT kind, status, error, updated_at
		FROM video_operations WHERE video_id=$1 ORDER BY kind`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VideoOperation{}
	for rows.Next() {
		var op VideoOperation
		var updated time.Time
		if err := rows.Scan(&op.Kind, &op.Status, &op.Error, &updated); err != nil {
			return nil, err
		}
		op.UpdatedAt = updated.UTC().Format(time.RFC3339)
		out = append(out, op)
	}
	return out, rows.Err()
}
