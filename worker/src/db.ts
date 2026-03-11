// Typed D1 query helpers — one function per table operation needed by the worker.

import type { Route } from './types.js';

// ---------------------------------------------------------------------------
// Row types (mirror SQL schema)
// ---------------------------------------------------------------------------

export interface TenantRow {
  id:         string;
  name:       string;
  plan:       string;
  created_at: string;
}

export interface DomainRow {
  id:         string;
  tenant_id:  string;
  domain:     string;
  verified:   number;
  txt_record: string;
  created_at: string;
}

export interface MessageRow {
  id:          string;
  tenant_id:   string;
  domain_id:   string;
  message_id:  string;
  from_addr:   string;
  to_addr:     string;
  subject:     string;
  received_at: string;
  r2_key:      string;
}

export interface DeliveryLogRow {
  id:            string;
  message_id:    string;
  route_id:      string;
  attempt:       number;
  status:        string;
  http_status:   number | null;
  response_body: string | null;
  delivered_at:  string;
}

// ---------------------------------------------------------------------------
// Domain helpers
// ---------------------------------------------------------------------------

/** Find a verified domain row by domain name. */
export async function findDomain(
  db: D1Database,
  domain: string,
): Promise<DomainRow | null> {
  return db
    .prepare('SELECT * FROM domains WHERE domain = ?1 AND verified = 1 LIMIT 1')
    .bind(domain)
    .first<DomainRow>();
}

// ---------------------------------------------------------------------------
// Route helpers
// ---------------------------------------------------------------------------

/** Return all active routes for a given domain. */
export async function findRoutesByDomain(
  db: D1Database,
  domainId: string,
): Promise<Route[]> {
  const result = await db
    .prepare('SELECT * FROM routes WHERE domain_id = ?1 AND active = 1')
    .bind(domainId)
    .all<Route>();
  return result.results;
}

// ---------------------------------------------------------------------------
// Message helpers
// ---------------------------------------------------------------------------

export interface InsertMessageParams {
  id:          string;
  tenant_id:   string;
  domain_id:   string;
  message_id:  string;
  from_addr:   string;
  to_addr:     string;
  subject:     string;
  received_at: string;
  r2_key:      string;
}

/** Insert a message row. Idempotent via INSERT OR IGNORE. */
export async function insertMessage(
  db: D1Database,
  p: InsertMessageParams,
): Promise<void> {
  await db
    .prepare(
      `INSERT OR IGNORE INTO messages
         (id, tenant_id, domain_id, message_id, from_addr, to_addr, subject, received_at, r2_key)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
    )
    .bind(p.id, p.tenant_id, p.domain_id, p.message_id, p.from_addr, p.to_addr, p.subject, p.received_at, p.r2_key)
    .run();
}

// ---------------------------------------------------------------------------
// Delivery log helpers
// ---------------------------------------------------------------------------

export interface InsertDeliveryLogParams {
  id:            string;
  message_id:    string;
  route_id:      string;
  attempt:       number;
  status:        string;
  http_status:   number | null;
  response_body: string | null;
}

/** Append a delivery log entry. */
export async function insertDeliveryLog(
  db: D1Database,
  p: InsertDeliveryLogParams,
): Promise<void> {
  await db
    .prepare(
      `INSERT INTO delivery_logs
         (id, message_id, route_id, attempt, status, http_status, response_body)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
    )
    .bind(p.id, p.message_id, p.route_id, p.attempt, p.status, p.http_status ?? null, p.response_body ?? null)
    .run();
}
