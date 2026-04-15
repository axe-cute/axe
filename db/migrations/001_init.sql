-- Migration: 001_init
-- Description: Create initial schema (users, outbox_events)
-- Created: 2026-04-15

-- ── Users ─────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email        VARCHAR(255) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    password_hash TEXT        NOT NULL,
    role         VARCHAR(20)  NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_active_created ON users(active, created_at DESC);

-- ── Outbox Events ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS outbox_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate    TEXT        NOT NULL,   -- e.g. "user", "order"
    event_type   TEXT        NOT NULL,   -- e.g. "UserRegistered", "OrderPlaced"
    payload      JSONB       NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    retries      INT         NOT NULL DEFAULT 0
);

-- Partial index: only unprocessed events (poller efficiency)
CREATE INDEX IF NOT EXISTS idx_outbox_unprocessed
    ON outbox_events(created_at ASC)
    WHERE processed_at IS NULL;

-- ── updated_at trigger ────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER set_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
