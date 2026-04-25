-- Migration: Add root_comment_id to episode_comments — 2026-04-25
--
-- Threads stay 1 level deep visually but we keep both:
--   parent_comment_id : direct reply target (drives @user mention)
--   root_comment_id   : top-level ancestor (groups replies into the same thread)
-- Backfill replies created before this migration: parent == root (we used to flatten).

ALTER TABLE episode_comments
    ADD COLUMN IF NOT EXISTS root_comment_id UUID
        REFERENCES episode_comments(id) ON DELETE CASCADE;

UPDATE episode_comments
SET    root_comment_id = parent_comment_id
WHERE  parent_comment_id IS NOT NULL
  AND  root_comment_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_episode_comments_root
    ON episode_comments (root_comment_id);
