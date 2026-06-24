-- Persistent anonymous viewer id (held in the player's localStorage). session_id
-- stays ephemeral per playback session; visitor_id identifies the same browser
-- across sessions so we count unique viewers, not page loads. Older rows are
-- NULL — analytics uses COALESCE(visitor_id, session_id) to count them.
ALTER TABLE playback_events ADD COLUMN IF NOT EXISTS visitor_id TEXT;
