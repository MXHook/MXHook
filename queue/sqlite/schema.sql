-- Retention: a successful acknowledgement deletes the row outright, so the
-- 'acked' state never persists. Nacked rows are retained with failure_reason
-- for the control API.
CREATE TABLE IF NOT EXISTS mxhook_queue (
    id           TEXT PRIMARY KEY,
    payload      BLOB NOT NULL,
    state        TEXT NOT NULL DEFAULT 'pending'
                 CHECK (state IN ('pending', 'processing', 'acked', 'nacked')),
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    locked_until TEXT,
    lease_token  TEXT,
    failure_reason TEXT
);

CREATE INDEX IF NOT EXISTS idx_mxhook_queue_state
    ON mxhook_queue (state, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mxhook_queue_lease_token
    ON mxhook_queue (lease_token) WHERE lease_token IS NOT NULL;
