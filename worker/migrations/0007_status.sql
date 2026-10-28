-- Migration: 0007_status.sql
-- Service status and incident tracking

CREATE TABLE IF NOT EXISTS uptime_events (
  id          INTEGER  PRIMARY KEY AUTOINCREMENT,
  component   TEXT     NOT NULL CHECK(component IN ('smtp_ingestion','webhook_delivery','d1','r2')),
  status      TEXT     NOT NULL CHECK(status IN ('operational','degraded','outage')),
  message     TEXT     NOT NULL DEFAULT '',
  started_at  TEXT     NOT NULL,
  resolved_at TEXT     NULL
);

CREATE INDEX IF NOT EXISTS idx_uptime_events_component ON uptime_events(component, started_at);
CREATE INDEX IF NOT EXISTS idx_uptime_events_active    ON uptime_events(resolved_at) WHERE resolved_at IS NULL;
