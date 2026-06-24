-- Library-level player customization (mirrors Bunny's per-library player
-- settings): UI language, font, primary color, captions appearance, which
-- controls show, playback speed options, custom CSS, heatmap/resume/compact
-- toggles. Stored as JSONB so the shape can evolve without further migrations;
-- an empty object means "use server defaults" (see internal/player.DefaultConfig).
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS player_config JSONB NOT NULL DEFAULT '{}';
