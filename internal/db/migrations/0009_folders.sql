-- Nested folders for organizing a library's videos (Bunny-style collections,
-- but with a parent_id so folders can nest arbitrarily).
--   parent_id NULL -> a root-level folder
--   videos.folder_id NULL -> the video sits at the library root
-- Deleting a folder cascades to its sub-folders; videos inside fall back to the
-- root (folder_id -> NULL) rather than being deleted.
CREATE TABLE IF NOT EXISTS folders (
    id         UUID PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id),
    parent_id  UUID REFERENCES folders(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_folders_library ON folders (library_id);
CREATE INDEX IF NOT EXISTS idx_folders_parent  ON folders (parent_id);

ALTER TABLE videos ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_videos_folder ON videos (folder_id);
