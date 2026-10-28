// dashboard/messages.ts — GET /api/v1/messages (paginated, filtered)

import type { Env } from "../types";
import { requireSession, sessionTenantId } from "../auth";
import { jsonResponse, errorResponse } from "../http";

export async function handleMessages(req: Request, env: Env): Promise<Response> {
  const session = await requireSession(req, env);
  if (!session) return errorResponse(401, "Unauthorized");

  const tenantId = sessionTenantId(session);
  const url = new URL(req.url);

  const domain = url.searchParams.get("domain") ?? null;
  const status = url.searchParams.get("status") ?? null;
  const since = url.searchParams.get("since") ?? null;
  const until = url.searchParams.get("until") ?? null;
  const limit = Math.min(parseInt(url.searchParams.get("limit") ?? "50", 10), 200);
  const cursor = url.searchParams.get("cursor") ?? null;

  // messages stores the recipient list in to_addr and keys the domain by
  // domain_id, so the name comes from a join. There is no status column: a
  // message's delivery state is the latest delivery_logs row for it.
  let query = `
    SELECT m.id, m.message_id, m.from_addr, m.to_addr, m.subject,
           d.domain AS domain, m.received_at,
           (SELECT dl.status FROM delivery_logs dl
             WHERE dl.message_id = m.id
             ORDER BY dl.attempt DESC LIMIT 1) AS status
    FROM messages m
    JOIN domains d ON d.id = m.domain_id
    WHERE m.tenant_id = ?
  `;
  const params: unknown[] = [tenantId];

  if (domain) { query += " AND d.domain = ?"; params.push(domain); }
  if (since) { query += " AND m.received_at >= ?"; params.push(since); }
  if (until) { query += " AND m.received_at <= ?"; params.push(until); }
  if (cursor) { query += " AND m.id < ?"; params.push(cursor); }

  if (status) {
    query += ` AND (SELECT dl.status FROM delivery_logs dl
                     WHERE dl.message_id = m.id
                     ORDER BY dl.attempt DESC LIMIT 1) = ?`;
    params.push(status);
  }

  query += " ORDER BY m.received_at DESC LIMIT ?";
  params.push(limit + 1);

  const result = await env.DB.prepare(query).bind(...params).all();
  const rows = result.results ?? [];

  const hasMore = rows.length > limit;
  const items = hasMore ? rows.slice(0, limit) : rows;
  const nextCursor = hasMore ? String((items[items.length - 1] as Record<string, unknown>).id) : null;

  return jsonResponse(200, { items, next_cursor: nextCursor, limit });
}
