-- Pre-upload browser edit decision list (trim/crop/rotate/flip), produced in the
-- browser and applied server-side during the existing ffmpeg HLS transcode.
-- Stored as JSONB so the EditSpec shape (see internal/video.EditSpec) can evolve
-- without further migrations; NULL means "no edit", leaving the pipeline unchanged.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS edit_spec JSONB;
