CREATE TABLE IF NOT EXISTS mxhook_rules (
    id               TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    domain           TEXT NOT NULL,
    recipient_pattern TEXT NOT NULL,
    target           TEXT NOT NULL DEFAULT 'webhook',
    webhook_url      TEXT NOT NULL DEFAULT '',
    webhook_secret   TEXT NOT NULL DEFAULT '',
    is_catch_all     BOOLEAN NOT NULL DEFAULT false,
    priority         INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mxhook_rules_domain ON mxhook_rules (domain);
