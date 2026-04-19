-- Migration: Create episodes table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS episodes (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title    VARCHAR(255)    NOT NULL,
    episode_number    BIGINT    NOT NULL,
    thumbnail_url    VARCHAR(255)    NOT NULL,
    published    BOOLEAN    NOT NULL,
    series_id UUID NOT NULL REFERENCES seriess(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_episodes_created ON episodes(created_at DESC);

CREATE OR REPLACE TRIGGER set_episodes_updated_at
    BEFORE UPDATE ON episodes
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
