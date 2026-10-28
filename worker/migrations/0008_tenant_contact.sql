-- Migration: 0008_tenant_contact.sql
-- Tenant contact address, lifecycle status, and the admin audit trail.
-- 0001_initial.sql creates tenants without email/status; the admin endpoints
-- read and write both, so add them here.

-- Nullable: SQLite cannot add a NOT NULL column without a default, and an
-- empty-string default would be indistinguishable from a real missing address.
-- NULL states "contact unknown" honestly for rows created before this column.
ALTER TABLE tenants ADD COLUMN email TEXT;

-- SQLite ALTER TABLE ADD COLUMN cannot carry a CHECK constraint, so the value
-- set (active | suspended | cancelled) is enforced in the admin handler.
ALTER TABLE tenants ADD COLUMN status TEXT NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_tenants_email  ON tenants (email);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants (status);

CREATE TABLE IF NOT EXISTS audit_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  action     TEXT NOT NULL,             -- e.g. admin.impersonate
  target_id  TEXT NOT NULL,             -- subject of the action, usually tenant id
  metadata   TEXT NOT NULL DEFAULT '{}', -- JSON object
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_action  ON audit_log (action, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_target  ON audit_log (target_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log (created_at);
