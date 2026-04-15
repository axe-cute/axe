-- 001_init_sqlite.sql
-- Initial schema for SQLite (dev/test use).
--
-- Differences from 001_init.sql (Postgres):
--   • UUID stored as TEXT (SQLite has no native UUID type)
--   • DATETIME instead of TIMESTAMPTZ
--   • DEFAULT (datetime('now')) instead of NOW()
--   • No CREATE EXTENSION needed
--   • Auto-increment via INTEGER PRIMARY KEY (not used here — we supply UUIDs)
--   • Index names kept identical for tooling consistency

CREATE TABLE IF NOT EXISTS users (
    id         TEXT        NOT NULL PRIMARY KEY,          -- UUID as TEXT
    email      TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL DEFAULT '',
    password_hash TEXT      NOT NULL,
    role       TEXT        NOT NULL DEFAULT 'user',
    created_at DATETIME    NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_users_email      ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_role       ON users (role);
CREATE INDEX IF NOT EXISTS idx_users_created_at ON users (created_at);

-- ── Outbox ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS outbox_events (
    id           TEXT        NOT NULL PRIMARY KEY,
    aggregate_id TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    payload      TEXT        NOT NULL DEFAULT '{}',      -- JSON as TEXT
    status       TEXT        NOT NULL DEFAULT 'pending',
    retry_count  INTEGER     NOT NULL DEFAULT 0,
    created_at   DATETIME    NOT NULL DEFAULT (datetime('now')),
    processed_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_outbox_status     ON outbox_events (status);
CREATE INDEX IF NOT EXISTS idx_outbox_created_at ON outbox_events (created_at);
