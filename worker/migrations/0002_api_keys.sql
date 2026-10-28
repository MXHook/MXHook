-- Migration: API keys and rate limiting tables

CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  key_hash     TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  last_used_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash   ON api_keys (key_hash);

CREATE TABLE IF NOT EXISTS rate_limit_counters (
  key_id       TEXT    NOT NULL,
  window_start INTEGER NOT NULL,
  count        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, window_start)
);

-- Cleanup old windows automatically (application-level; D1 lacks TTL)
-- Periodically run: DELETE FROM rate_limit_counters WHERE window_start < unixepoch() - 300;
