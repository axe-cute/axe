-- Migration: Create posts table — Generated: 2026-04-15

CREATE TABLE IF NOT EXISTS posts (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title    VARCHAR(255)    NOT NULL,
    body    TEXT    NOT NULL,
    published    BOOLEAN    NOT NULL,
    views    BIGINT    NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC);

CREATE OR REPLACE TRIGGER set_posts_updated_at
    BEFORE UPDATE ON posts
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
