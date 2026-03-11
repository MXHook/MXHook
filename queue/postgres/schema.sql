CREATE TABLE IF NOT EXISTS mxhook_queue (
    id          TEXT PRIMARY KEY,
    body        BYTEA NOT NULL,
    meta        JSONB NOT NULL DEFAULT '{}',
    state       TEXT NOT NULL DEFAULT 'pending', -- pending, processing, acked, nacked
    attempts    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_mxhook_queue_state ON mxhook_queue (state, created_at);
