-- Migration: Create episode_comment_likes table — 2026-04-25

CREATE TABLE IF NOT EXISTS episode_comment_likes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    comment_id  UUID NOT NULL REFERENCES episode_comments(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One like per (comment, user)
CREATE UNIQUE INDEX IF NOT EXISTS idx_episode_comment_likes_user_comment
    ON episode_comment_likes (comment_id, user_id);

-- Fast count by comment
CREATE INDEX IF NOT EXISTS idx_episode_comment_likes_comment
    ON episode_comment_likes (comment_id);
