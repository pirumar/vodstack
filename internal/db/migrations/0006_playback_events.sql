-- Phase 4: viewer analytics / QoE. The player POSTs lightweight beacons to a
-- public endpoint; each becomes a row here. Append-only and high-volume — read
-- via aggregates, and lean on Prometheus for live dashboards. A periodic prune
-- (like the trash purge) keeps it bounded.
CREATE TABLE IF NOT EXISTS playback_events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    video_id    UUID NOT NULL,
    library_id  TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    event_type  TEXT NOT NULL,           -- start | playing | rebuffer | error | progress | ended
    position    DOUBLE PRECISION,        -- playhead seconds
    value       DOUBLE PRECISION,        -- event-specific (startup ms, watch %, etc.)
    bitrate     INT,                     -- current variant bitrate (bps)
    resolution  TEXT,                    -- e.g. "720p"
    country     TEXT,
    device      TEXT,                    -- coarse UA class (desktop/mobile/tv)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_playback_events_video ON playback_events (video_id, created_at DESC);
