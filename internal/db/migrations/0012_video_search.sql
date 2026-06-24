-- In-video search: hybrid semantic (pgvector) + lexical (pg_trgm) over transcript
-- chunks. The worker parses each caption VTT into ~30s windows, embeds them with
-- the library's configured provider (local / Gemini / Voyage), and stores one row
-- per chunk. A query embeds the search string with the SAME provider, runs a
-- cosine-distance ANN scan plus a trigram scan, and fuses the two rankings.
--
-- Embeddings are fixed at 1024 dimensions across all providers so the column /
-- index never changes shape; switching providers re-embeds (vectors from
-- different models are not comparable), which is why provider/model are stored.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS video_search_chunks (
    library_id  TEXT             NOT NULL,
    video_id    UUID             NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    lang        TEXT             NOT NULL,
    chunk_index INT              NOT NULL,
    start_sec   DOUBLE PRECISION NOT NULL,  -- seek target for this chunk
    end_sec     DOUBLE PRECISION NOT NULL,
    text        TEXT             NOT NULL,  -- the chunk transcript (for snippet + trgm)
    embedding   vector(1024),              -- provider-agnostic fixed dimension
    provider    TEXT             NOT NULL,  -- which embedder produced it (stale detect)
    model       TEXT             NOT NULL,
    created_at  TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (library_id, video_id, lang, chunk_index)
);

-- Semantic ANN: cosine distance over the embedding.
CREATE INDEX IF NOT EXISTS idx_vsc_vec
    ON video_search_chunks USING hnsw (embedding vector_cosine_ops);

-- Lexical: trigram match for exact terms / proper nouns (Turkish has no built-in
-- FTS dictionary, so trigram similarity carries the keyword side).
CREATE INDEX IF NOT EXISTS idx_vsc_trgm
    ON video_search_chunks USING gin (text gin_trgm_ops);

-- Per-video scoping (single-video search + cascade cleanups).
CREATE INDEX IF NOT EXISTS idx_vsc_video
    ON video_search_chunks (library_id, video_id);

-- Library-level search configuration (provider, model, API key, toggles), mirroring
-- the player_config JSONB pattern from 0010.
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS search_config JSONB NOT NULL DEFAULT '{}';
