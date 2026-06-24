-- Phase 5: per-video access control.
--   visibility 'public'  -> embeddable anywhere (default)
--   visibility 'signed'  -> only via a freshly minted token (no public embed page)
--   visibility 'private' -> embed allowed only from an allowed_referrers origin
-- allowed_referrers: optional origin allowlist (e.g. {https://app.example.com}).
-- expires_at: optional hard cutoff after which no new play tokens are minted.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS visibility        TEXT NOT NULL DEFAULT 'public';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS allowed_referrers TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS expires_at        TIMESTAMPTZ;
