-- Migration: Add parent_comment_id to episode_comments for threaded replies — 2026-04-25

ALTER TABLE episode_comments
    ADD COLUMN IF NOT EXISTS parent_comment_id UUID
        REFERENCES episode_comments(id) ON DELETE CASCADE;

-- Fast lookup of replies belonging to a parent
CREATE INDEX IF NOT EXISTS idx_episode_comments_parent
    ON episode_comments (parent_comment_id);
