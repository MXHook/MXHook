-- D1 initial schema for mxhook SaaS layer
-- Apply with: wrangler d1 migrations apply mxhook

CREATE TABLE IF NOT EXISTS tenants (
  id         TEXT PRIMARY KEY,          -- UUID
  name       TEXT NOT NULL,
  plan       TEXT NOT NULL DEFAULT 'free', -- free | pro | enterprise
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS domains (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  domain     TEXT NOT NULL UNIQUE,
  verified   INTEGER NOT NULL DEFAULT 0, -- 0 | 1
  txt_record TEXT NOT NULL,              -- DNS verification token
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_domains_tenant ON domains(tenant_id);
CREATE INDEX IF NOT EXISTS idx_domains_domain  ON domains(domain);

CREATE TABLE IF NOT EXISTS routes (
  id             TEXT PRIMARY KEY,
  tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  domain_id      TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  match_pattern  TEXT NOT NULL,   -- glob / prefix pattern matched against To address
  webhook_url    TEXT NOT NULL,
  webhook_secret TEXT NOT NULL,   -- HMAC-SHA256 signing key
  active         INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_routes_domain ON routes(domain_id);

CREATE TABLE IF NOT EXISTS messages (
  id          TEXT PRIMARY KEY,   -- message_id from email header or generated UUID
  tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  domain_id   TEXT NOT NULL REFERENCES domains(id),
  message_id  TEXT NOT NULL,      -- RFC-5322 Message-Id header value
  from_addr   TEXT NOT NULL,
  to_addr     TEXT NOT NULL,      -- comma-separated list
  subject     TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,      -- ISO-8601
  r2_key      TEXT NOT NULL       -- R2 object key: tenant_id/message_id
);

CREATE INDEX IF NOT EXISTS idx_messages_tenant    ON messages(tenant_id);
CREATE INDEX IF NOT EXISTS idx_messages_domain    ON messages(domain_id);
CREATE INDEX IF NOT EXISTS idx_messages_received  ON messages(received_at);

CREATE TABLE IF NOT EXISTS delivery_logs (
  id            TEXT PRIMARY KEY,
  message_id    TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  route_id      TEXT NOT NULL REFERENCES routes(id)   ON DELETE CASCADE,
  attempt       INTEGER NOT NULL DEFAULT 1,
  status        TEXT NOT NULL, -- success | failed | retrying
  http_status   INTEGER,       -- HTTP response code from webhook endpoint
  response_body TEXT,          -- first 4096 chars of webhook response
  delivered_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_dlogs_message ON delivery_logs(message_id);
CREATE INDEX IF NOT EXISTS idx_dlogs_route   ON delivery_logs(route_id);
