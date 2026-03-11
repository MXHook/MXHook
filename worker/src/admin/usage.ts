// admin/usage.ts — GET /admin/usage (top 10 tenants by message volume this month)

import type { Env } from "../types";
import { requireAdminAPIKey } from "../auth";
import { jsonResponse, errorResponse } from "../http";

export async function handleAdminUsage(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const now = new Date();
  const periodStart = new Date(now.getFullYear(), now.getMonth(), 1).toISOString();

  const result = await env.DB.prepare(
    `SELECT t.id, t.name, t.email, t.plan,
            COUNT(m.id) AS message_count,
            COUNT(DISTINCT m.domain) AS active_domains
     FROM tenants t
     LEFT JOIN messages m ON m.tenant_id = t.id AND m.received_at >= ?
     GROUP BY t.id
     ORDER BY message_count DESC
     LIMIT 10`,
  )
    .bind(periodStart)
    .all();

  return jsonResponse(200, {
    period_start: periodStart,
    top_tenants: result.results ?? [],
  });
}
