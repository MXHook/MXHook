-- Migration: 0006_abuse.sql
-- Abuse detection: rate-limit event log and sender blocklist

CREATE TABLE IF NOT EXISTS abuse_events (
  id          INTEGER  PRIMARY KEY AUTOINCREMENT,
  message_id  TEXT     NOT NULL,
  tenant_id   TEXT     NOT NULL,
  sender_ip   TEXT     NOT NULL,
  from_addr   TEXT     NOT NULL,
  score       INTEGER  NOT NULL DEFAULT 0,
  reasons     TEXT     NOT NULL DEFAULT '[]', -- JSON array
  action      TEXT     NOT NULL CHECK(action IN ('allow', 'warn', 'block')),
  created_at  TEXT     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_abuse_events_ip         ON abuse_events(sender_ip, created_at);
CREATE INDEX IF NOT EXISTS idx_abuse_events_tenant     ON abuse_events(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_abuse_events_message    ON abuse_events(message_id);

CREATE TABLE IF NOT EXISTS blocked_senders (
  id          INTEGER  PRIMARY KEY AUTOINCREMENT,
  pattern     TEXT     NOT NULL UNIQUE, -- email addr, domain, or LIKE glob
  reason      TEXT     NOT NULL DEFAULT '',
  created_at  TEXT     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_blocked_senders_pattern ON blocked_senders(pattern);
