// types.ts — shared Cloudflare Worker bindings and interfaces

export interface Env {
  DB: D1Database;
  BUCKET: R2Bucket;
  EMAIL_QUEUE: Queue;
  SESSION_SECRET: string;
  ADMIN_API_KEY: string;
}

export interface Session {
  tenantId: string;
  userId: string;
  email: string;
  isAdmin: boolean;
  expiresAt: number;
}
