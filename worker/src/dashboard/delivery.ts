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
    `SELECT id, message_id, webhook_url, status, attempt, response_status,
            response_body, error, attempted_at, duration_ms
     FROM delivery_attempts
     WHERE message_id = ?
     ORDER BY attempt ASC`,
  )
    .bind(messageId)
    .all();

  return jsonResponse(200, { message_id: messageId, deliveries: result.results ?? [] });
}
