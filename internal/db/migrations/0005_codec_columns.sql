-- Phase 3: codec efficiency. AV1 renditions are produced asynchronously on the
-- bulk lane and live alongside the always-present H.264 ladder.
--
-- encode_profile selects what the pipeline produces:
--   'h264'      -> H.264 only (default; fast, ships immediately)
--   'h264+av1'  -> H.264 first, then an AV1 backfill that swaps in a combined master
-- av1_resolutions mirrors available_resolutions but for the AV1 ladder (NULL
-- until the backfill completes).
ALTER TABLE videos ADD COLUMN IF NOT EXISTS encode_profile  TEXT NOT NULL DEFAULT 'h264';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS av1_resolutions TEXT;
