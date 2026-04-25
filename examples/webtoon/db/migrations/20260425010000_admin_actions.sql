-- admin_actions: audit trail for every admin mutation.
--
-- Write amplification is low (admin actions are infrequent vs. user reads),
-- so we index for the common query: "show me recent activity, optionally
-- filtered by actor or subject".
--
-- Kept as a single flat table (not per-entity) so the admin UI can show a
-- unified timeline.

CREATE TABLE admin_actions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      UUID,                          -- JWT sub (may be NULL on system actions)
    actor_email   TEXT,                          -- denormalised for UI
    action        TEXT NOT NULL,                 -- e.g. 'series.create', 'page.delete'
    subject_type  TEXT,                          -- 'series' | 'episode' | 'page' | ...
    subject_id    UUID,
    status        INT  NOT NULL,                 -- HTTP status code (for filtering failures)
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip            INET,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Timeline view (most common admin query).
CREATE INDEX idx_admin_actions_timeline
    ON admin_actions (created_at DESC);

-- "What did this admin do?" — secondary filter.
CREATE INDEX idx_admin_actions_by_actor
    ON admin_actions (actor_id, created_at DESC)
    WHERE actor_id IS NOT NULL;

-- "What happened to this series/episode?" — audit-per-subject.
CREATE INDEX idx_admin_actions_by_subject
    ON admin_actions (subject_type, subject_id, created_at DESC)
    WHERE subject_type IS NOT NULL;
