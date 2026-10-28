// admin/tenants.ts — GET/PATCH /admin/tenants

import type { Env } from "../types";
import { requireAdminAPIKey } from "../auth";
import { jsonResponse, errorResponse } from "../http";

const VALID_PLANS = ["free", "starter", "pro", "enterprise"] as const;

export async function handleListTenants(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const url = new URL(req.url);
  const limit = Math.min(parseInt(url.searchParams.get("limit") ?? "50", 10), 200);
  const cursor = url.searchParams.get("cursor") ?? null;

  let query = `
    SELECT id, name, email, plan, status, created_at,
           (SELECT COUNT(*) FROM domains WHERE tenant_id = tenants.id) AS domain_count
    FROM tenants
  `;
  const params: unknown[] = [];
  if (cursor) { query += " WHERE id > ?"; params.push(cursor); }
  query += " ORDER BY created_at DESC LIMIT ?";
  params.push(limit + 1);

  const result = await env.DB.prepare(query).bind(...params).all();
  const rows = result.results ?? [];
  const hasMore = rows.length > limit;
  const items = hasMore ? rows.slice(0, limit) : rows;
  const nextCursor = hasMore ? String((items[items.length - 1] as Record<string, unknown>).id) : null;

  return jsonResponse(200, { items, next_cursor: nextCursor });
}

export async function handleGetTenant(req: Request, env: Env, id: string): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const tenant = await env.DB.prepare(
    `SELECT t.id, t.name, t.email, t.plan, t.status, t.created_at,
            COUNT(DISTINCT d.id) AS domain_count,
            COUNT(DISTINCT m.id) AS message_count
     FROM tenants t
     LEFT JOIN domains d ON d.tenant_id = t.id
     LEFT JOIN messages m ON m.tenant_id = t.id
     WHERE t.id = ?
     GROUP BY t.id`,
  )
    .bind(id)
    .first();

  if (!tenant) return errorResponse(404, "Tenant not found");
  return jsonResponse(200, tenant);
}

export async function handlePatchTenant(req: Request, env: Env, id: string): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  let body: Record<string, unknown>;
  try {
    body = await req.json() as Record<string, unknown>;
  } catch {
    return errorResponse(400, "Invalid JSON");
  }

  const updates: string[] = [];
  const params: unknown[] = [];

  if (body.plan !== undefined) {
    if (!VALID_PLANS.includes(body.plan as (typeof VALID_PLANS)[number])) {
      return errorResponse(400, `Invalid plan; must be one of: ${VALID_PLANS.join(", ")}`);
    }
    updates.push("plan = ?");
    params.push(body.plan);
  }

  if (body.status !== undefined) {
    if (!["active", "suspended", "cancelled"].includes(body.status as string)) {
      return errorResponse(400, "Invalid status");
    }
    updates.push("status = ?");
    params.push(body.status);
  }

  if (!updates.length) return errorResponse(400, "Nothing to update");

  params.push(id);
  await env.DB.prepare(`UPDATE tenants SET ${updates.join(", ")} WHERE id = ?`)
    .bind(...params)
    .run();

  const updated = await env.DB.prepare("SELECT * FROM tenants WHERE id = ?").bind(id).first();
  return jsonResponse(200, updated);
}
