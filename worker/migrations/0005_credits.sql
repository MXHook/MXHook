-- Migration: Credits table for prepaid usage

CREATE TABLE IF NOT EXISTS credits (
  id         TEXT    PRIMARY KEY,
  tenant_id  TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  amount     INTEGER NOT NULL CHECK (amount > 0),
  used       INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0),
  expires_at TEXT,   -- NULL = never expires
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_credits_tenant     ON credits (tenant_id);
CREATE INDEX IF NOT EXISTS idx_credits_expires    ON credits (expires_at);
CREATE INDEX IF NOT EXISTS idx_credits_available  ON credits (tenant_id, used, expires_at);
