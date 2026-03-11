// types.ts — shared Cloudflare Worker bindings and interfaces

export interface Env {
  // D1 database (schema: tenants, domains, routes, messages, delivery_logs)
  DB: D1Database;

  // R2 bucket for raw email storage
  EMAIL_STORE: R2Bucket;

  // Queue binding
  EMAIL_QUEUE: Queue;

  // Secrets
  SESSION_SECRET: string;
  ADMIN_API_KEY: string;

  // Non-secret env vars
  LOG_LEVEL: string;
}

// EmailEvent is the payload published by fly.io SMTP edge nodes.
export interface EmailEvent {
  message_id: string;
  from:        string;
  to:          string[];    // one or more recipients
  subject:     string;
  received_at: string;      // ISO-8601
  raw:         string;      // full RFC-5322 message
  tenant_id?:  string;      // resolved by worker after domain lookup
  // abuse check fields
  id?:         string;
  senderIp?:   string;
  body?:       string;
  domain?:     string;
  tenantId?:   string;
}

// Route row from D1.
export interface Route {
  id:             string;
  tenant_id:      string;
  domain_id:      string;
  match_pattern:  string;
  webhook_url:    string;
  webhook_secret: string;
  active:         number; // 1 | 0
}

export interface Session {
  tenantId: string;
  userId: string;
  email: string;
  isAdmin: boolean;
  expiresAt: number;
}
