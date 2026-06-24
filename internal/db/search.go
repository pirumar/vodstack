package db

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/pirumar/vodstack/internal/search"
)

// --- Search configuration (libraries.search_config JSONB) ---

// GetSearchConfig returns a library's in-video-search settings. An empty stored
// config yields the defaults (Normalize fills every field). ErrNotFound if the
// library is gone.
func (d *DB) GetSearchConfig(ctx context.Context, libraryID string) (search.Config, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx,
		`SELECT search_config FROM libraries WHERE id=$1`, libraryID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return search.Config{}, ErrNotFound
	}
	if err != nil {
		return search.Config{}, err
	}
	cfg := search.DefaultConfig()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg) // tolerate partial/legacy JSON; Normalize repairs it
	}
	cfg.Normalize()
	return cfg, nil
}

// SetSearchConfig persists a library's search settings (already normalized).
func (d *DB) SetSearchConfig(ctx context.Context, libraryID string, cfg search.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return d.exec(ctx,
		`UPDATE libraries SET search_config=$2 WHERE id=$1`, libraryID, raw)
}

// --- Search chunks (the index) ---

// ReplaceSearchChunks atomically swaps a (video, lang)'s indexed chunks: it
// deletes the old rows and inserts the new ones in one transaction, so a reindex
// never leaves a half-built index. vectors[i] is the embedding for chunks[i].
func (d *DB) ReplaceSearchChunks(ctx context.Context, libraryID, videoID, lang string, chunks []search.Chunk, vectors [][]float32, provider, model string) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM video_search_chunks WHERE library_id=$1 AND video_id=$2 AND lang=$3`,
		libraryID, videoID, lang); err != nil {
		return err
	}
	for i, c := range chunks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO video_search_chunks
			    (library_id, video_id, lang, chunk_index, start_sec, end_sec, text, embedding, provider, model)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$10)`,
			libraryID, videoID, lang, c.Index, c.StartSec, c.EndSec, c.Text,
			vectorString(vectors[i]), provider, model); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CountSearchChunks returns how many chunks are indexed for a video (UI status).
func (d *DB) CountSearchChunks(ctx context.Context, libraryID, videoID string) (int, error) {
	var n int
	err := d.pool.QueryRow(ctx,
		`SELECT count(*) FROM video_search_chunks WHERE library_id=$1 AND video_id=$2`,
		libraryID, videoID,
	).Scan(&n)
	return n, err
}

// SearchHit is one transcript match: a video + the moment to seek to + snippet.
type SearchHit struct {
	VideoID  string  `json:"videoId"`
	Title    string  `json:"title"`
	Lang     string  `json:"lang"`
	StartSec float64 `json:"startSec"`
	EndSec   float64 `json:"endSec"`
	Snippet  string  `json:"snippet"`
	Score    float64 `json:"score"`
}

// candidate is an internal row pulled by either scan.
type candidate struct {
	key string // video_id|lang|chunk_index
	hit SearchHit
}

// SearchChunks runs the hybrid search for a library. queryVec is the embedded
// query (nil to skip the semantic scan); queryText drives the lexical trigram
// scan (empty to skip it). videoID, when non-empty, restricts to one video. The
// two rankings are fused with Reciprocal Rank Fusion and the top `limit` hits
// returned, highest score first. Tenant-scoped by library_id throughout.
func (d *DB) SearchChunks(ctx context.Context, libraryID string, queryVec []float32, queryText, videoID string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 20
	}
	poolK := limit * 3
	if poolK < 30 {
		poolK = 30
	}

	var vidParam *string
	if videoID != "" {
		vidParam = &videoID
	}

	byKey := make(map[string]SearchHit)
	var vecRank, lexRank []string

	// Semantic scan (cosine distance).
	if len(queryVec) == search.EmbedDim {
		rows, err := d.pool.Query(ctx, `
			SELECT c.video_id, c.lang, c.chunk_index, c.start_sec, c.end_sec, c.text, v.title
			FROM video_search_chunks c
			JOIN videos v ON v.id = c.video_id
			WHERE c.library_id=$1 AND v.deleted_at IS NULL
			  AND ($2::uuid IS NULL OR c.video_id=$2::uuid)
			ORDER BY c.embedding <=> $3::vector
			LIMIT $4`,
			libraryID, vidParam, vectorString(queryVec), poolK)
		if err != nil {
			return nil, err
		}
		cands, err := scanCandidates(rows)
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			byKey[c.key] = c.hit
			vecRank = append(vecRank, c.key)
		}
	}

	// Lexical scan (trigram similarity / substring).
	if strings.TrimSpace(queryText) != "" {
		rows, err := d.pool.Query(ctx, `
			SELECT c.video_id, c.lang, c.chunk_index, c.start_sec, c.end_sec, c.text, v.title
			FROM video_search_chunks c
			JOIN videos v ON v.id = c.video_id
			WHERE c.library_id=$1 AND v.deleted_at IS NULL
			  AND ($2::uuid IS NULL OR c.video_id=$2::uuid)
			  AND (c.text ILIKE '%'||$3||'%' OR similarity(c.text, $3) > 0.1)
			ORDER BY similarity(c.text, $3) DESC
			LIMIT $4`,
			libraryID, vidParam, queryText, poolK)
		if err != nil {
			return nil, err
		}
		cands, err := scanCandidates(rows)
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			byKey[c.key] = c.hit
			lexRank = append(lexRank, c.key)
		}
	}

	// Fuse and order.
	scores := search.ReciprocalRankFusion(vecRank, lexRank)
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return scores[keys[i]] > scores[keys[j]] })

	out := make([]SearchHit, 0, limit)
	for _, k := range keys {
		if len(out) >= limit {
			break
		}
		hit := byKey[k]
		hit.Score = scores[k]
		out = append(out, hit)
	}
	return out, nil
}

func scanCandidates(rows pgx.Rows) ([]candidate, error) {
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var (
			h   SearchHit
			idx int
		)
		if err := rows.Scan(&h.VideoID, &h.Lang, &idx, &h.StartSec, &h.EndSec, &h.Snippet, &h.Title); err != nil {
			return nil, err
		}
		out = append(out, candidate{
			key: h.VideoID + "|" + h.Lang + "|" + strconv.Itoa(idx),
			hit: h,
		})
	}
	return out, rows.Err()
}

// vectorString renders a float slice as a pgvector text literal "[a,b,c]".
func vectorString(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
