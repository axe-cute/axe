-- episode_pages: one row per image panel inside an episode.
--
-- Why a separate table (not a JSON column on episodes)?
--   - Panels are fetched independently of episode metadata (different cache TTL)
--   - We want to transform/reorder without rewriting the whole episode row
--   - Variants (thumb / medium / full) join cleanly on (episode_id, page_num)
--
-- status flow:
--   uploaded  → original in storage, not yet processed
--   processing → asynq worker picked it up
--   ready     → all variants written, safe to serve
--   failed    → transform errored (keep original, manual retry)

CREATE TABLE episode_pages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    episode_id  UUID NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    page_num    INT NOT NULL,

    -- Storage keys. original_key is the admin upload; variants populated
    -- by the transform worker.
    original_key   TEXT NOT NULL,
    thumb_key      TEXT,  -- 400px wide, JPEG
    medium_key     TEXT,  -- 1200px wide, JPEG (main reader variant)

    width_px    INT,
    height_px   INT,
    bytes       BIGINT,
    content_type TEXT,

    status      TEXT NOT NULL DEFAULT 'uploaded',
    error       TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT episode_pages_page_num_positive CHECK (page_num > 0),
    CONSTRAINT episode_pages_status_valid CHECK (status IN ('uploaded','processing','ready','failed')),
    CONSTRAINT episode_pages_unique_page UNIQUE (episode_id, page_num)
);

-- Primary read pattern: "give me all ready pages for this episode in order".
-- Partial index filters out uploaded/processing/failed rows at scan time.
CREATE INDEX idx_episode_pages_ready
    ON episode_pages (episode_id, page_num)
    WHERE status = 'ready';

-- Admin dashboard: "show me anything that failed so I can retry".
CREATE INDEX idx_episode_pages_failed
    ON episode_pages (episode_id, created_at DESC)
    WHERE status = 'failed';
