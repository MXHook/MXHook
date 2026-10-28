CREATE TABLE IF NOT EXISTS mxhook_state_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS mxhook_routes (
    id TEXT PRIMARY KEY,
    domain TEXT NOT NULL,
    recipient_pattern TEXT NOT NULL,
    target TEXT NOT NULL CHECK (target IN ('webhook', 'storage')),
    webhook_url TEXT NOT NULL DEFAULT '',
    webhook_secret BYTEA NOT NULL DEFAULT ''::bytea,
    is_catch_all BOOLEAN NOT NULL DEFAULT FALSE,
    priority INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS mxhook_domains (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mxhook_routes_enabled
    ON mxhook_routes (enabled, priority DESC, created_at);

CREATE TABLE IF NOT EXISTS mxhook_events (
    id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    raw_path TEXT NOT NULL,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mxhook_events_received_at
    ON mxhook_events (received_at);

CREATE TABLE IF NOT EXISTS mxhook_ingress_failures (
    message_id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS mxhook_deliveries (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES mxhook_events(id) ON DELETE CASCADE,
    route_id TEXT NOT NULL,
    webhook_url TEXT NOT NULL,
    webhook_secret BYTEA NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'processing', 'retry', 'succeeded', 'dead')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    replay_generation INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    lease_token TEXT,
    locked_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(message_id, route_id)
);

CREATE INDEX IF NOT EXISTS idx_mxhook_deliveries_due
    ON mxhook_deliveries (state, next_attempt_at, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mxhook_deliveries_lease
    ON mxhook_deliveries (lease_token) WHERE lease_token IS NOT NULL;

CREATE TABLE IF NOT EXISTS mxhook_delivery_attempts (
    delivery_id TEXT NOT NULL REFERENCES mxhook_deliveries(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    outcome TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    finished_at TIMESTAMPTZ NOT NULL,
    replay_generation INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (delivery_id, replay_generation, attempt_no)
);

INSERT INTO mxhook_state_migrations(version, applied_at)
VALUES (1, NOW())
ON CONFLICT(version) DO NOTHING;
