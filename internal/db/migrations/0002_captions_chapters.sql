-- Seek-preview WebVTT (filename under the HLS prefix) and YouTube-style chapters
-- (a JSON array of {start, title}) live on the video row.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS thumbnails_vtt TEXT;
ALTER TABLE videos ADD COLUMN IF NOT EXISTS chapters       JSONB;

-- Caption/subtitle tracks: one row per language, the VTT stored in MinIO under
-- the video's HLS prefix.
CREATE TABLE IF NOT EXISTS captions (
    id         UUID PRIMARY KEY,
    video_id   UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    lang       TEXT NOT NULL,
    label      TEXT NOT NULL,
    object     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (video_id, lang)
);
