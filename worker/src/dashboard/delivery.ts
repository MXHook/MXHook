// dashboard/delivery.ts — GET /api/v1/messages/:id/deliveries

import type { Env } from "../types";
import { requireSession, sessionTenantId } from "../auth";
import { jsonResponse, errorResponse } from "../http";

export async function handleDeliveries(
  req: Request,
  env: Env,
  messageId: string,
): Promise<Response> {
  const session = await requireSession(req, env);
  if (!session) return errorResponse(401, "Unauthorized");

  const tenantId = sessionTenantId(session);

  // Verify message belongs to this tenant
  const msg = await env.DB.prepare(
    "SELECT id FROM messages WHERE id = ? AND tenant_id = ?",
  )
    .bind(messageId, tenantId)
    .first();

  if (!msg) return errorResponse(404, "Message not found");

  const result = await env.DB.prepare(
    // The table is delivery_logs; webhook_url lives on the route it targeted.
    // No per-attempt duration or separate error text is stored.
    `SELECT dl.id, dl.message_id, dl.route_id, r.webhook_url, dl.status,
            dl.attempt, dl.http_status, dl.response_body, dl.delivered_at
     FROM delivery_logs dl
     LEFT JOIN routes r ON r.id = dl.route_id
     WHERE dl.message_id = ?
     ORDER BY dl.attempt ASC`,
  )
    .bind(messageId)
    .all();

  return jsonResponse(200, { message_id: messageId, deliveries: result.results ?? [] });
}
