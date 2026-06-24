package db

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PlaybackEvent is one viewer beacon.
type PlaybackEvent struct {
	VideoID    string
	LibraryID  string
	SessionID  string
	VisitorID  string // persistent per-browser id; "" for storage-blocked/old players
	EventType  string
	Position   *float64
	Value      *float64
	Bitrate    *int
	Resolution string
	Country    string
	Device     string
}

// InsertPlaybackEvent appends a viewer event (append-only, hot path — keep lean).
func (d *DB) InsertPlaybackEvent(ctx context.Context, e PlaybackEvent) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO playback_events
		  (video_id, library_id, session_id, visitor_id, event_type, position, value, bitrate, resolution, country, device)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.VideoID, e.LibraryID, e.SessionID, nullIfEmpty(e.VisitorID), e.EventType, e.Position, e.Value,
		e.Bitrate, nullIfEmpty(e.Resolution), nullIfEmpty(e.Country), nullIfEmpty(e.Device))
	return err
}

// CountryStat is one row of the per-country engagement breakdown.
type CountryStat struct {
	Country      string  `json:"country"`      // ISO-3166 alpha-2 (CF-IPCountry)
	Starts       int     `json:"starts"`       // playback starts from this country
	Sessions     int     `json:"sessions"`     // distinct viewers
	WatchSeconds float64 `json:"watchSeconds"` // total watch time (per-session high-water sum)
}

// VideoAnalytics is a per-video QoE + engagement rollup.
type VideoAnalytics struct {
	Sessions          int           `json:"sessions"`          // distinct viewers
	Starts            int           `json:"starts"`            // playback starts (≈ views)
	AvgStartupMs      float64       `json:"avgStartupMs"`      // time-to-first-frame
	Rebuffers         int           `json:"rebuffers"`
	Errors            int           `json:"errors"`
	Completions       int           `json:"completions"`       // 'ended' events
	TotalWatchSeconds float64       `json:"totalWatchSeconds"` // sum of per-session high-water positions
	AvgWatchSeconds   float64       `json:"avgWatchSeconds"`   // total / sessions-with-position
	EstBandwidthBytes int64         `json:"estBandwidthBytes"` // ESTIMATE: Σ watched-fraction × size
	ByCountry         []CountryStat `json:"byCountry"`
}

// GetVideoAnalytics aggregates the event stream for one video. since bounds the
// window (zero = all time).
func (d *DB) GetVideoAnalytics(ctx context.Context, libraryID, videoID string, since time.Time) (*VideoAnalytics, error) {
	a := &VideoAnalytics{ByCountry: []CountryStat{}}
	where, args := eventScope(libraryID, videoID, since)
	err := d.pool.QueryRow(ctx, `
		SELECT
		  COUNT(DISTINCT COALESCE(visitor_id, session_id)),
		  COUNT(*) FILTER (WHERE event_type='start'),
		  COALESCE(AVG(value) FILTER (WHERE event_type='start'), 0),
		  COUNT(*) FILTER (WHERE event_type='rebuffer'),
		  COUNT(*) FILTER (WHERE event_type='error'),
		  COUNT(*) FILTER (WHERE event_type='ended')
		FROM playback_events
		WHERE `+where, args...,
	).Scan(&a.Sessions, &a.Starts, &a.AvgStartupMs, &a.Rebuffers, &a.Errors, &a.Completions)
	if err != nil {
		return nil, err
	}

	if a.TotalWatchSeconds, a.AvgWatchSeconds, a.EstBandwidthBytes, err =
		d.watchAndBandwidth(ctx, libraryID, videoID, since); err != nil {
		return nil, err
	}
	if a.ByCountry, err = d.countryBreakdown(ctx, libraryID, videoID, since); err != nil {
		return nil, err
	}
	return a, nil
}

// eventScope builds the shared WHERE clause + positional args for a playback_events
// query: always the library, optionally a single video and a time floor.
func eventScope(libraryID, videoID string, since time.Time) (string, []any) {
	where := "library_id=$1"
	args := []any{libraryID}
	if videoID != "" {
		args = append(args, videoID)
		where += " AND video_id=$" + itoa(len(args))
	}
	if !since.IsZero() {
		args = append(args, since)
		where += " AND created_at >= $" + itoa(len(args))
	}
	return where, args
}

// watchAndBandwidth derives watch time and an ESTIMATED egress from the event
// stream. Watch time per session is the high-water playhead (MAX(position));
// estimated bytes is that fraction of the video's stored size — segments are
// served by the CDN/MinIO, so the true byte count isn't visible to us.
func (d *DB) watchAndBandwidth(ctx context.Context, libraryID, videoID string, since time.Time) (total, avg float64, estBytes int64, err error) {
	where, args := eventScope(libraryID, videoID, since)
	var sessions int
	var est float64
	err = d.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(s.mp), 0),
		       COUNT(*),
		       COALESCE(SUM(LEAST(1.0, GREATEST(0, s.mp / NULLIF(v.duration_seconds, 0))) * v.size_bytes), 0)
		FROM (
		  SELECT session_id, video_id, MAX(position) AS mp
		  FROM playback_events
		  WHERE `+where+` AND position IS NOT NULL
		  GROUP BY session_id, video_id
		) s
		JOIN videos v ON v.id = s.video_id`, args...,
	).Scan(&total, &sessions, &est)
	if err != nil {
		return 0, 0, 0, err
	}
	if sessions > 0 {
		avg = total / float64(sessions)
	}
	return total, avg, int64(est), nil
}

// countryBreakdown returns per-country starts/sessions/watch-time, ranked by
// starts. Rows without a resolved country are excluded.
func (d *DB) countryBreakdown(ctx context.Context, libraryID, videoID string, since time.Time) ([]CountryStat, error) {
	where, args := eventScope(libraryID, videoID, since)

	// starts + distinct sessions per country
	rows, err := d.pool.Query(ctx, `
		SELECT country,
		       COUNT(*) FILTER (WHERE event_type='start'),
		       COUNT(DISTINCT COALESCE(visitor_id, session_id))
		FROM playback_events
		WHERE `+where+` AND country IS NOT NULL AND country <> ''
		GROUP BY country`, args...)
	if err != nil {
		return nil, err
	}
	byCC := map[string]*CountryStat{}
	for rows.Next() {
		var c CountryStat
		if err := rows.Scan(&c.Country, &c.Starts, &c.Sessions); err != nil {
			rows.Close()
			return nil, err
		}
		cc := c
		byCC[c.Country] = &cc
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// watch time per country (per-session high-water summed)
	wRows, err := d.pool.Query(ctx, `
		SELECT country, COALESCE(SUM(mp), 0) FROM (
		  SELECT session_id, country, MAX(position) AS mp
		  FROM playback_events
		  WHERE `+where+` AND position IS NOT NULL AND country IS NOT NULL AND country <> ''
		  GROUP BY session_id, country
		) t GROUP BY country`, args...)
	if err != nil {
		return nil, err
	}
	for wRows.Next() {
		var cc string
		var watch float64
		if err := wRows.Scan(&cc, &watch); err != nil {
			wRows.Close()
			return nil, err
		}
		if c, ok := byCC[cc]; ok {
			c.WatchSeconds = watch
		}
	}
	wRows.Close()
	if err := wRows.Err(); err != nil {
		return nil, err
	}

	out := make([]CountryStat, 0, len(byCC))
	for _, c := range byCC {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Starts != out[j].Starts {
			return out[i].Starts > out[j].Starts
		}
		return out[i].Sessions > out[j].Sessions
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out, nil
}

// itoa is a tiny strconv.Itoa alias kept local so the SQL builders read cleanly.
func itoa(n int) string { return strconv.Itoa(n) }

// qualify prefixes the playback_events columns in an eventScope WHERE clause with
// a table alias, for queries that JOIN videos (where library_id/created_at would
// otherwise be ambiguous).
func qualify(where, alias string) string {
	where = strings.ReplaceAll(where, "library_id", alias+".library_id")
	where = strings.ReplaceAll(where, "created_at", alias+".created_at")
	where = strings.ReplaceAll(where, "video_id", alias+".video_id")
	return where
}

// TopVideo is a single row in the library's most-watched ranking.
type TopVideo struct {
	VideoID  string `json:"videoId"`
	Title    string `json:"title"`
	Sessions int    `json:"sessions"`
	Starts   int    `json:"starts"`
}

// DailyPoint is one day in the library's engagement trend.
type DailyPoint struct {
	Date     string `json:"date"` // YYYY-MM-DD
	Sessions int    `json:"sessions"`
	Starts   int    `json:"starts"`
}

// LibraryAnalytics is a library-wide QoE + engagement rollup for the panel.
type LibraryAnalytics struct {
	Sessions          int           `json:"sessions"`
	Starts            int           `json:"starts"`
	AvgStartupMs      float64       `json:"avgStartupMs"`
	Rebuffers         int           `json:"rebuffers"`
	Errors            int           `json:"errors"`
	Completions       int           `json:"completions"`
	TotalWatchSeconds float64       `json:"totalWatchSeconds"`
	AvgWatchSeconds   float64       `json:"avgWatchSeconds"`
	EstBandwidthBytes int64         `json:"estBandwidthBytes"`
	ByCountry         []CountryStat `json:"byCountry"`
	TopVideos         []TopVideo    `json:"topVideos"`
	Daily             []DailyPoint  `json:"daily"`
}

// GetLibraryAnalytics aggregates the whole library's event stream over the given
// window (since zero = all time): headline totals, watch time, estimated
// bandwidth, per-country breakdown, the most-watched videos, and a daily trend.
func (d *DB) GetLibraryAnalytics(ctx context.Context, libraryID string, since time.Time) (*LibraryAnalytics, error) {
	a := &LibraryAnalytics{ByCountry: []CountryStat{}, TopVideos: []TopVideo{}, Daily: []DailyPoint{}}
	where, args := eventScope(libraryID, "", since)

	err := d.pool.QueryRow(ctx, `
		SELECT
		  COUNT(DISTINCT COALESCE(visitor_id, session_id)),
		  COUNT(*) FILTER (WHERE event_type='start'),
		  COALESCE(AVG(value) FILTER (WHERE event_type='start'), 0),
		  COUNT(*) FILTER (WHERE event_type='rebuffer'),
		  COUNT(*) FILTER (WHERE event_type='error'),
		  COUNT(*) FILTER (WHERE event_type='ended')
		FROM playback_events
		WHERE `+where, args...,
	).Scan(&a.Sessions, &a.Starts, &a.AvgStartupMs, &a.Rebuffers, &a.Errors, &a.Completions)
	if err != nil {
		return nil, err
	}

	if a.TotalWatchSeconds, a.AvgWatchSeconds, a.EstBandwidthBytes, err =
		d.watchAndBandwidth(ctx, libraryID, "", since); err != nil {
		return nil, err
	}
	if a.ByCountry, err = d.countryBreakdown(ctx, libraryID, "", since); err != nil {
		return nil, err
	}

	// Most-watched videos (joined for titles), ranked by playback starts.
	topRows, err := d.pool.Query(ctx, `
		SELECT pe.video_id, COALESCE(v.title, ''),
		       COUNT(DISTINCT COALESCE(pe.visitor_id, pe.session_id)) AS sessions,
		       COUNT(*) FILTER (WHERE pe.event_type='start') AS starts
		FROM playback_events pe
		LEFT JOIN videos v ON v.id = pe.video_id
		WHERE `+qualify(where, "pe")+`
		GROUP BY pe.video_id, v.title
		ORDER BY starts DESC, sessions DESC
		LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	defer topRows.Close()
	for topRows.Next() {
		var t TopVideo
		if err := topRows.Scan(&t.VideoID, &t.Title, &t.Sessions, &t.Starts); err != nil {
			return nil, err
		}
		a.TopVideos = append(a.TopVideos, t)
	}
	if err := topRows.Err(); err != nil {
		return nil, err
	}

	// Daily engagement trend: bounded by the window, defaulting to 30 days when
	// the window is unbounded so the chart stays readable.
	dayArgs := append([]any{}, args...)
	dayFloor := "created_at >= now() - interval '30 days'"
	if !since.IsZero() {
		dayFloor = "created_at >= $" + itoa(len(args)) // since is the last arg eventScope added
	}
	dayRows, err := d.pool.Query(ctx, `
		SELECT created_at::date AS day,
		       COUNT(DISTINCT COALESCE(visitor_id, session_id)) AS sessions,
		       COUNT(*) FILTER (WHERE event_type='start') AS starts
		FROM playback_events
		WHERE library_id=$1 AND `+dayFloor+`
		GROUP BY day
		ORDER BY day`, dayArgs...)
	if err != nil {
		return nil, err
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var day time.Time
		var p DailyPoint
		if err := dayRows.Scan(&day, &p.Sessions, &p.Starts); err != nil {
			return nil, err
		}
		p.Date = day.Format("2006-01-02")
		a.Daily = append(a.Daily, p)
	}
	if err := dayRows.Err(); err != nil {
		return nil, err
	}

	return a, nil
}

// heatmapMinSamples is the number of positioned events required before a
// heatmap is exposed — mirrors Bunny's "after enough data is collected" so a
// handful of views don't produce a misleading curve.
const heatmapMinSamples = 30

// GetHeatmap returns a normalized watchtime curve for a video: buckets values in
// [0,1] across the duration, where higher means more viewer activity at that
// point. Returns (nil, nil) when there isn't enough data yet. buckets caps the
// resolution; duration is the video length in seconds.
func (d *DB) GetHeatmap(ctx context.Context, libraryID, videoID string, buckets, duration int) ([]float64, error) {
	if buckets <= 0 || duration <= 0 {
		return nil, nil
	}

	rows, err := d.pool.Query(ctx, `
		SELECT LEAST($3 - 1, GREATEST(0, FLOOR(position / $4::float * $3)::int)) AS bucket,
		       COUNT(*) AS n
		FROM playback_events
		WHERE video_id=$1 AND library_id=$2 AND position IS NOT NULL AND position >= 0
		GROUP BY bucket`,
		videoID, libraryID, buckets, duration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make([]float64, buckets)
	var total, max float64
	for rows.Next() {
		var bucket int
		var n float64
		if err := rows.Scan(&bucket, &n); err != nil {
			return nil, err
		}
		if bucket >= 0 && bucket < buckets {
			counts[bucket] = n
			total += n
			if n > max {
				max = n
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if total < heatmapMinSamples || max == 0 {
		return nil, nil // not enough data yet
	}
	for i := range counts {
		counts[i] /= max // normalize to [0,1]
	}
	return counts, nil
}

// PrunePlaybackEvents deletes events older than the retention window.
func (d *DB) PrunePlaybackEvents(ctx context.Context, retention time.Duration) error {
	_, err := d.pool.Exec(ctx,
		`DELETE FROM playback_events WHERE created_at < $1`, time.Now().Add(-retention))
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
