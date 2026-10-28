-- Retention: a successful acknowledgement deletes the row outright, so the
-- 'acked' state never persists. Nacked rows are retained with failure_reason
-- for the control API.
CREATE TABLE IF NOT EXISTS mxhook_queue (
    id          TEXT PRIMARY KEY,
    payload     JSONB NOT NULL,
    state       TEXT NOT NULL DEFAULT 'pending'
                CHECK (state IN ('pending', 'processing', 'acked', 'nacked')),
    attempts    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until TIMESTAMPTZ,
    lease_token  TEXT,
    failure_reason TEXT
);

-- CREATE TABLE IF NOT EXISTS skips the constraint on databases created before
-- it was introduced, so add it separately to keep both backends in step.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'mxhook_queue_state_check'
    ) THEN
        ALTER TABLE mxhook_queue ADD CONSTRAINT mxhook_queue_state_check
            CHECK (state IN ('pending', 'processing', 'acked', 'nacked'));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_mxhook_queue_state ON mxhook_queue (state, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mxhook_queue_lease_token
    ON mxhook_queue (lease_token) WHERE lease_token IS NOT NULL;
