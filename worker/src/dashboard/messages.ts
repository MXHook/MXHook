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

  let query = `
    SELECT id, message_id, from_addr, to_addrs, subject, domain, status, received_at
    FROM messages
    WHERE tenant_id = ?
  `;
  const params: unknown[] = [tenantId];

  if (domain) { query += " AND domain = ?"; params.push(domain); }
  if (status) { query += " AND status = ?"; params.push(status); }
  if (since) { query += " AND received_at >= ?"; params.push(since); }
  if (until) { query += " AND received_at <= ?"; params.push(until); }
  if (cursor) { query += " AND id < ?"; params.push(cursor); }

  query += " ORDER BY received_at DESC LIMIT ?";
  params.push(limit + 1);

  const result = await env.DB.prepare(query).bind(...params).all();
  const rows = result.results ?? [];

  const hasMore = rows.length > limit;
  const items = hasMore ? rows.slice(0, limit) : rows;
  const nextCursor = hasMore ? String((items[items.length - 1] as Record<string, unknown>).id) : null;

  return jsonResponse(200, { items, next_cursor: nextCursor, limit });
}
