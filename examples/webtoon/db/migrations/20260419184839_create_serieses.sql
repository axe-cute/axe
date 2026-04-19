-- Migration: Create serieses table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS serieses (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title    VARCHAR(255)    NOT NULL,
    description    TEXT    NOT NULL,
    genre    VARCHAR(255)    NOT NULL,
    author    VARCHAR(255)    NOT NULL,
    cover_url    VARCHAR(255)    NOT NULL,
    status    VARCHAR(255)    NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_serieses_created ON serieses(created_at DESC);

CREATE OR REPLACE TRIGGER set_serieses_updated_at
    BEFORE UPDATE ON serieses
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
