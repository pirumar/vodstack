-- Per-library default encoding settings (Bunny's "Encoding Tier" controls):
-- enabled resolutions, output codecs, MP4 fallback, original download, Early-Play,
-- multi-audio, and watermark. Stored as JSONB so the shape can evolve without
-- further migrations; an empty object means "use server defaults" (see
-- internal/encoding.DefaultConfig). Mirrors the player_config pattern (0010).
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS encoding_config JSONB NOT NULL DEFAULT '{}';

-- Per-video snapshot of the resolved settings taken at creation, so the AV1/HEVC/
-- VP9 backfills and the AES-128 re-encode all reuse the exact config the upload
-- was made with even if the library default changes later.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS encode_settings JSONB NOT NULL DEFAULT '{}';

-- Carry the legacy encode_profile codec choice into the unified codecs model so
-- existing 'h264+av1' videos keep their AV1 ladder. Normalize() fills the rest.
UPDATE videos SET encode_settings = jsonb_build_object(
    'codecs',
    CASE WHEN encode_profile = 'h264+av1' THEN '["h264","av1"]'::jsonb
         ELSE '["h264"]'::jsonb END)
WHERE encode_settings = '{}'::jsonb;
