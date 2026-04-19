-- Migration: Create bookmarks table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS bookmarks (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    series_id    UUID    NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bookmarks_created ON bookmarks(created_at DESC);

CREATE OR REPLACE TRIGGER set_bookmarks_updated_at
    BEFORE UPDATE ON bookmarks
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
