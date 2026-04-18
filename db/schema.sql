-- db/schema.sql
-- Minimal schema for sqlc code generation (framework reference implementation).
-- This is NOT a migration file — it lives only in the axe framework repo for
-- sqlc to generate typed query helpers used by internal/repository/.
--
-- User-facing projects own their own db/migrations/ and db/schema.sql.

CREATE TABLE IF NOT EXISTS users (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    email         VARCHAR(255) NOT NULL UNIQUE,
    name          VARCHAR(255) NOT NULL,
    password_hash TEXT         NOT NULL,
    role          VARCHAR(20)  NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    active        BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
