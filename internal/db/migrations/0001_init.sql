-- Libraries are tenants/workspaces. Each owns an API key used by the
-- control-plane callers. The key is stored hashed.
CREATE TABLE IF NOT EXISTS libraries (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    api_key_hash TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Videos: the source of truth for this service. Status integers mirror Bunny
-- Stream (see internal/video/status.go).
CREATE TABLE IF NOT EXISTS videos (
    id                    UUID PRIMARY KEY,
    library_id            TEXT NOT NULL REFERENCES libraries(id),
    title                 TEXT NOT NULL,
    collection_id         TEXT,
    status                INT  NOT NULL DEFAULT 0,
    source_object         TEXT,
    duration_seconds      INT,
    width                 INT,
    height                INT,
    size_bytes            BIGINT,
    available_resolutions TEXT,
    thumbnail_file        TEXT,
    encode_progress       INT  NOT NULL DEFAULT 0,
    error_message         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_videos_library ON videos (library_id);
CREATE INDEX IF NOT EXISTS idx_videos_status  ON videos (status);
