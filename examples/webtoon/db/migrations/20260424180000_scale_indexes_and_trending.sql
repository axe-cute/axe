-- Migration: add composite indexes and trending_score column for scale.
-- Generated: 2026-04-24
--
-- After this migration:
--   • Browse queries (genre + status + newest) hit an index instead of a seq scan.
--   • Reader queries (episodes of a series, in order) hit an index.
--   • "My library" (bookmarks of a user, newest first) hits an index.
--   • series.trending_score is a raw SQL-owned field (outside Ent) that the
--     periodic job in internal/jobs/trending.go recomputes from Redis counters.

-- Episodes: the hottest query — list episodes of series X in order.
CREATE INDEX IF NOT EXISTS idx_episodes_series_num
    ON episodes (series_id, episode_number);

-- Series: catalog browse by (genre, status) newest-first.
CREATE INDEX IF NOT EXISTS idx_series_genre_status_created
    ON series (genre, status, created_at DESC);

-- Bookmarks: user's library newest-first.
CREATE INDEX IF NOT EXISTS idx_bookmarks_user_created
    ON bookmarks (user_id, created_at DESC);

-- trending_score: refreshed by periodic job; read by /serieses/trending.
ALTER TABLE series
    ADD COLUMN IF NOT EXISTS trending_score DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Trending lookups: top-N newest-first by trending_score.
CREATE INDEX IF NOT EXISTS idx_series_trending
    ON series (trending_score DESC)
    WHERE trending_score > 0;
