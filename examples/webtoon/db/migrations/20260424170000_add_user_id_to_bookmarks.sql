-- Migration: Add user_id to bookmarks — 2026-04-24

ALTER TABLE bookmarks
    ADD COLUMN IF NOT EXISTS user_id VARCHAR(255) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_bookmarks_user_id ON bookmarks(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_bookmarks_user_series
    ON bookmarks(user_id, series_id);
