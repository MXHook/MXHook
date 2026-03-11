// dashboard/billing.ts — GET /api/v1/billing/usage

import type { Env } from "../types";
import { requireSession, sessionTenantId } from "../auth";
import { jsonResponse, errorResponse } from "../http";

const PLAN_LIMITS: Record<string, { messages: number; domains: number; webhooks: number }> = {
  free:       { messages: 1_000,  domains: 1,  webhooks: 1 },
  starter:    { messages: 10_000, domains: 5,  webhooks: 5 },
  pro:        { messages: 100_000, domains: 20, webhooks: 20 },
  enterprise: { messages: -1,     domains: -1, webhooks: -1 }, // unlimited
};

export async function handleBillingUsage(req: Request, env: Env): Promise<Response> {
  const session = await requireSession(req, env);
  if (!session) return errorResponse(401, "Unauthorized");

  const tenantId = sessionTenantId(session);

  const tenant = await env.DB.prepare(
    "SELECT plan, billing_period_start FROM tenants WHERE id = ?",
  )
    .bind(tenantId)
    .first<{ plan: string; billing_period_start: string }>();

  if (!tenant) return errorResponse(404, "Tenant not found");

  const periodStart = tenant.billing_period_start ?? new Date(
    new Date().getFullYear(),
    new Date().getMonth(),
    1,
  ).toISOString();

  const [msgRow, domainRow] = await Promise.all([
    env.DB.prepare(
      "SELECT COUNT(*) AS count FROM messages WHERE tenant_id = ? AND received_at >= ?",
    )
      .bind(tenantId, periodStart)
      .first<{ count: number }>(),
    env.DB.prepare(
      "SELECT COUNT(*) AS count FROM domains WHERE tenant_id = ?",
    )
      .bind(tenantId)
      .first<{ count: number }>(),
  ]);

  const plan = tenant.plan ?? "free";
  const limits = PLAN_LIMITS[plan] ?? PLAN_LIMITS.free;

  return jsonResponse(200, {
    plan,
    period_start: periodStart,
    usage: {
      messages: { used: msgRow?.count ?? 0, limit: limits.messages },
      domains:  { used: domainRow?.count ?? 0, limit: limits.domains },
    },
  });
}
