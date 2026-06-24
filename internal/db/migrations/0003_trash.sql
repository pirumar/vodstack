-- Soft delete: a non-null deleted_at means the video is in the trash. It stays
-- recoverable for a retention window, then a background job purges it for good.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_videos_deleted_at ON videos (deleted_at);
