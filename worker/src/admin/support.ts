// admin/support.ts — POST /admin/tenants/:id/impersonate

import type { Env } from "../types";
import { requireAdminAPIKey, createSessionToken } from "../auth";
import { jsonResponse, errorResponse } from "../http";

// Generates a 30-minute impersonation session for the given tenant.
export async function handleImpersonate(req: Request, env: Env, tenantId: string): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const tenant = await env.DB.prepare(
    "SELECT id, name, email FROM tenants WHERE id = ?",
  )
    .bind(tenantId)
    .first<{ id: string; name: string; email: string }>();

  if (!tenant) return errorResponse(404, "Tenant not found");

  const session = {
    tenantId: tenant.id,
    userId: `admin_impersonate_${tenant.id}`,
    email: tenant.email,
    isAdmin: false,
    expiresAt: Date.now() + 30 * 60 * 1000, // 30 min
  };

  const token = await createSessionToken(session, env.SESSION_SECRET);

  // Audit log
  await env.DB.prepare(
    "INSERT INTO audit_log (action, target_id, metadata, created_at) VALUES (?, ?, ?, ?)",
  )
    .bind("admin.impersonate", tenantId, JSON.stringify({ tenant_email: tenant.email }), new Date().toISOString())
    .run()
    .catch(() => {}); // non-fatal

  return jsonResponse(200, {
    token,
    tenant_id: tenant.id,
    tenant_email: tenant.email,
    expires_in: 1800,
    note: "Token valid for 30 minutes. Use as Bearer token for dashboard API.",
  });
}
