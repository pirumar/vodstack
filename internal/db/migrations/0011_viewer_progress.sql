-- Phase 6: per-viewer progress. The platform mints a signed viewer token (vt)
-- identifying a stable end-user; the player carries it on beacons. We keep ONE
-- live row per (library, viewer, video): last playhead position (the resume
-- point), a monotonic watched high-water mark, derived watched-percentage and a
-- completed flag. playback_events stays the append-only QoE stream; this table
-- is the queryable per-viewer rollup (watch history + resume).
CREATE TABLE IF NOT EXISTS viewer_progress (
    library_id      TEXT             NOT NULL,
    viewer_id       TEXT             NOT NULL,
    video_id        UUID             NOT NULL,
    position        DOUBLE PRECISION NOT NULL DEFAULT 0,  -- last playhead seconds (resume point)
    duration        DOUBLE PRECISION,                     -- video length seconds (may be unknown)
    watched_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,  -- monotonic high-water mark
    watched_percent DOUBLE PRECISION NOT NULL DEFAULT 0,  -- 0..100, derived when duration known
    completed       BOOLEAN          NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    last_watched_at TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (library_id, viewer_id, video_id)
);

-- "history by viewer": list a viewer's videos newest-watched first.
CREATE INDEX IF NOT EXISTS idx_viewer_progress_history
    ON viewer_progress (library_id, viewer_id, last_watched_at DESC);

-- "viewers of a video": admin panel per-video viewer list.
CREATE INDEX IF NOT EXISTS idx_viewer_progress_video
    ON viewer_progress (library_id, video_id, last_watched_at DESC);
