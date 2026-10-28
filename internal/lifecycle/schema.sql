CREATE TABLE IF NOT EXISTS mxhook_lifecycle (
    id          BIGSERIAL PRIMARY KEY,
    message_id  TEXT NOT NULL,
    state       TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT '',
    at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mxhook_lifecycle_message_id ON mxhook_lifecycle (message_id);
CREATE INDEX IF NOT EXISTS idx_mxhook_lifecycle_state ON mxhook_lifecycle (state, at);
