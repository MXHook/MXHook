// dashboard/domains.ts — domain list with verification status and route count

import type { Env } from "../types";
import { requireSession, sessionTenantId } from "../auth";
import { jsonResponse, errorResponse } from "../http";

export async function handleDomains(req: Request, env: Env): Promise<Response> {
  const session = await requireSession(req, env);
  if (!session) return errorResponse(401, "Unauthorized");

  const tenantId = sessionTenantId(session);

  const result = await env.DB.prepare(
    `SELECT d.id, d.name, d.verified, d.verification_token,
            d.spf_ok, d.dkim_ok, d.created_at,
            COUNT(r.id) AS route_count
     FROM domains d
     LEFT JOIN routes r ON r.domain_id = d.id
     WHERE d.tenant_id = ?
     GROUP BY d.id
     ORDER BY d.created_at DESC`,
  )
    .bind(tenantId)
    .all();

  return jsonResponse(200, { domains: result.results ?? [] });
}
