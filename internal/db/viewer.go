package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ViewerProgressUpdate is one progress beacon attributed to a known viewer.
type ViewerProgressUpdate struct {
	LibraryID string
	ViewerID  string
	VideoID   string
	Position  float64 // current playhead seconds
	Event     string  // start|playing|rebuffer|error|progress|ended
}

// ViewerProgress is a viewer's saved state for one video: the resume point plus
// completion. Used both for server-side resume and platform watch-history.
type ViewerProgress struct {
	VideoID        string    `json:"videoId"`
	Title          string    `json:"title,omitempty"`
	ViewerID       string    `json:"viewerId,omitempty"`
	Position       float64   `json:"position"`
	Duration       *float64  `json:"duration,omitempty"`
	WatchedPercent float64   `json:"watchedPercent"`
	Completed      bool      `json:"completed"`
	LastWatchedAt  time.Time `json:"lastWatchedAt"`
}

// completionTail is the fraction of duration past which a viewer is considered
// to have finished even without an explicit 'ended' event.
const completionTail = 0.95

// UpsertViewerProgress records a viewer's latest position for a video. Duration
// is resolved server-side from the videos row (so the client can't fake it);
// watched_seconds is a monotonic high-water mark (seeking back never loses
// progress) and completed is monotonic too. Idempotent on the (library, viewer,
// video) primary key, so repeated/out-of-order beacons are safe. If the video is
// unknown (or soft-deleted to a different library) nothing is inserted.
func (d *DB) UpsertViewerProgress(ctx context.Context, u ViewerProgressUpdate) error {
	// Every $4 (position) usage is cast to float8: Postgres otherwise deduces it
	// as int in the int-division/comparison contexts and float8 in the column
	// contexts, which conflicts ("inconsistent types deduced for parameter $4").
	_, err := d.pool.Exec(ctx, `
		INSERT INTO viewer_progress AS vp
		  (library_id, viewer_id, video_id, position, duration,
		   watched_seconds, watched_percent, completed, updated_at, last_watched_at)
		SELECT $1, $2, $3, $4::float8, v.duration_seconds::float8, $4::float8,
		  CASE WHEN v.duration_seconds > 0 THEN LEAST(100, $4::float8 / v.duration_seconds * 100) ELSE 0 END,
		  ($5 = 'ended') OR (v.duration_seconds > 0 AND $4::float8 >= v.duration_seconds * $6::float8),
		  now(), now()
		FROM videos v
		WHERE v.id = $3 AND v.library_id = $1
		ON CONFLICT (library_id, viewer_id, video_id) DO UPDATE SET
		  position        = $4::float8,
		  duration        = COALESCE(EXCLUDED.duration, vp.duration),
		  watched_seconds = GREATEST(vp.watched_seconds, $4::float8),
		  watched_percent = CASE WHEN COALESCE(EXCLUDED.duration, vp.duration) > 0
		      THEN LEAST(100, GREATEST(vp.watched_seconds, $4::float8) / COALESCE(EXCLUDED.duration, vp.duration) * 100)
		      ELSE vp.watched_percent END,
		  completed       = vp.completed OR EXCLUDED.completed,
		  updated_at      = now(),
		  last_watched_at = now()`,
		u.LibraryID, u.ViewerID, u.VideoID, u.Position, u.Event, completionTail)
	return err
}

// GetViewerProgress returns one viewer's saved state for a video, or ErrNotFound
// if the viewer has never watched it.
func (d *DB) GetViewerProgress(ctx context.Context, libraryID, viewerID, videoID string) (*ViewerProgress, error) {
	var p ViewerProgress
	err := d.pool.QueryRow(ctx, `
		SELECT video_id, position, duration, watched_percent, completed, last_watched_at
		FROM viewer_progress
		WHERE library_id=$1 AND viewer_id=$2 AND video_id=$3`,
		libraryID, viewerID, videoID,
	).Scan(&p.VideoID, &p.Position, &p.Duration, &p.WatchedPercent, &p.Completed, &p.LastWatchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListViewerHistory returns the videos a viewer has watched, newest-watched
// first, with titles joined in. limit caps the rows.
func (d *DB) ListViewerHistory(ctx context.Context, libraryID, viewerID string, limit int) ([]ViewerProgress, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT vp.video_id, COALESCE(v.title, ''), vp.position, vp.duration,
		       vp.watched_percent, vp.completed, vp.last_watched_at
		FROM viewer_progress vp
		LEFT JOIN videos v ON v.id = vp.video_id AND v.library_id = vp.library_id
		WHERE vp.library_id=$1 AND vp.viewer_id=$2
		ORDER BY vp.last_watched_at DESC
		LIMIT $3`,
		libraryID, viewerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanViewerProgress(rows, false)
}

// ListVideoViewers returns the viewers who have watched a video, newest-watched
// first. Used by the admin panel's per-video viewer list.
func (d *DB) ListVideoViewers(ctx context.Context, libraryID, videoID string, limit int) ([]ViewerProgress, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT vp.video_id, vp.viewer_id, vp.position, vp.duration,
		       vp.watched_percent, vp.completed, vp.last_watched_at
		FROM viewer_progress vp
		WHERE vp.library_id=$1 AND vp.video_id=$2
		ORDER BY vp.last_watched_at DESC
		LIMIT $3`,
		libraryID, videoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanViewerProgress(rows, true)
}

// scanViewerProgress reads progress rows. withViewer selects the viewer_id
// column (per-video listing) instead of the title column (per-viewer history).
func scanViewerProgress(rows pgx.Rows, withViewer bool) ([]ViewerProgress, error) {
	out := []ViewerProgress{}
	for rows.Next() {
		var p ViewerProgress
		var second string // title or viewer_id depending on the query
		if err := rows.Scan(&p.VideoID, &second, &p.Position, &p.Duration,
			&p.WatchedPercent, &p.Completed, &p.LastWatchedAt); err != nil {
			return nil, err
		}
		if withViewer {
			p.ViewerID = second
		} else {
			p.Title = second
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PruneViewerProgress deletes progress rows untouched within the retention
// window. Called only when ViewerProgressRetentionDays > 0.
func (d *DB) PruneViewerProgress(ctx context.Context, retention time.Duration) error {
	_, err := d.pool.Exec(ctx,
		`DELETE FROM viewer_progress WHERE last_watched_at < $1`, time.Now().Add(-retention))
	return err
}
