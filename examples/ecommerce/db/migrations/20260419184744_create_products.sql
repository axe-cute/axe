-- Migration: Create products table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS products (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name    VARCHAR(255)    NOT NULL,
    description    TEXT    NOT NULL,
    price    DECIMAL(18,2)    NOT NULL,
    stock    BIGINT    NOT NULL,
    image_url    VARCHAR(255)    NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_products_created ON products(created_at DESC);

CREATE OR REPLACE TRIGGER set_products_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
