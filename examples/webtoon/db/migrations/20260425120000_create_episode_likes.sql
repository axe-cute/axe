-- Migration: Create episode_likes table — 2026-04-25

CREATE TABLE IF NOT EXISTS episode_likes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    episode_id  UUID NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fast lookup: has this user liked this episode?
CREATE UNIQUE INDEX IF NOT EXISTS idx_episode_likes_user_episode
    ON episode_likes (episode_id, user_id);

-- Fast count: how many likes for an episode?
CREATE INDEX IF NOT EXISTS idx_episode_likes_episode
    ON episode_likes (episode_id);
