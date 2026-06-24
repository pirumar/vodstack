-- video_operations tracks the live state of each long-running advanced operation
-- (AV1, captions, AI content, encryption, search index) so the admin UI can show
-- queued/running/done/failed instead of leaving the user guessing. One row per
-- (video, kind): a re-trigger overwrites the previous run's status.
CREATE TABLE IF NOT EXISTS video_operations (
    video_id   UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,   -- 'av1' | 'caption' | 'ai_content' | 'encrypt' | 'search_index'
    status     TEXT NOT NULL,   -- 'queued' | 'running' | 'done' | 'failed'
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (video_id, kind)
);
