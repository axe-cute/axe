-- Migration: Create reviews table — Generated: 2026-04-19

CREATE TABLE IF NOT EXISTS reviews (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    body    TEXT    NOT NULL,
    rating    BIGINT    NOT NULL,
    product_id UUID NOT NULL REFERENCES products(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reviews_created ON reviews(created_at DESC);

CREATE OR REPLACE TRIGGER set_reviews_updated_at
    BEFORE UPDATE ON reviews
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
