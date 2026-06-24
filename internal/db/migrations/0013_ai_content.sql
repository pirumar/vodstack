-- AI content generation from transcripts: an LLM (configured per library, an
-- OpenAI-compatible /chat/completions router) reads a video's transcript and
-- produces a description/summary, tags, and chapters. Description + tags live on
-- the video row; chapters reuse the existing videos.chapters column + chapters.vtt.
ALTER TABLE videos ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE videos ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]';

-- Library-level LLM router configuration (provider endpoint, model, API key),
-- mirroring the search_config / player_config JSONB pattern.
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS llm_config JSONB NOT NULL DEFAULT '{}';
