-- Migration: Create orders table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS orders (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    total    DECIMAL(18,2)    NOT NULL,
    status    VARCHAR(255)    NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at DESC);

CREATE OR REPLACE TRIGGER set_orders_updated_at
    BEFORE UPDATE ON orders
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
