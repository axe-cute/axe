-- Migration: Add view_count to episodes — 2026-04-25

ALTER TABLE episodes
    ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0;
