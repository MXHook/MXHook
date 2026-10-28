-- Migration: Billing columns and indexes on tenants
-- 0001_initial.sql creates tenants without Stripe fields; add them here so the
-- migration set is self-contained.

ALTER TABLE tenants ADD COLUMN stripe_customer_id TEXT;
ALTER TABLE tenants ADD COLUMN plan_expires_at    TEXT;

CREATE INDEX IF NOT EXISTS idx_tenants_plan            ON tenants (plan);
CREATE INDEX IF NOT EXISTS idx_tenants_stripe_customer ON tenants (stripe_customer_id);
