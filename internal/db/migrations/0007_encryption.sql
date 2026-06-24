-- Phase 5: AES-128 HLS encryption. Budget-appropriate middle ground between
-- signed-URLs-only and full multi-DRM: segments are encrypted at encode time and
-- the key is served by the API only to holders of a valid playback token. Stops
-- casual download/hotlinking, not a determined attacker (the honest limit).
CREATE TABLE IF NOT EXISTS content_keys (
    key_id     UUID PRIMARY KEY,
    library_id TEXT NOT NULL,
    video_id   UUID NOT NULL,
    key_hex    TEXT NOT NULL,   -- 16-byte AES-128 key, hex
    iv_hex     TEXT NOT NULL,   -- 16-byte IV, hex
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_content_keys_video ON content_keys (video_id);

ALTER TABLE videos ADD COLUMN IF NOT EXISTS encryption_mode TEXT NOT NULL DEFAULT 'none'; -- none | aes128
ALTER TABLE videos ADD COLUMN IF NOT EXISTS key_id UUID;
