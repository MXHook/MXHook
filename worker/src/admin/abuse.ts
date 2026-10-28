// admin/abuse.ts — POST /admin/abuse/block, GET /admin/abuse/events

import type { Env } from "../types";
import { requireAdminAPIKey } from "../auth";
import { jsonResponse, errorResponse } from "../http";
import { addBlocklistEntry, recentAbuseEvents } from "../abuse";

export async function handleAddBlock(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  let body: { pattern: string; reason?: string };
  try {
    body = await req.json() as typeof body;
  } catch {
    return errorResponse(400, "Invalid JSON");
  }

  if (!body.pattern || body.pattern.trim() === "") {
    return errorResponse(400, "pattern is required");
  }

  await addBlocklistEntry(body.pattern.trim(), body.reason ?? "", env);
  return jsonResponse(201, { pattern: body.pattern.trim(), reason: body.reason ?? "" });
}

export async function handleAbuseEvents(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const url = new URL(req.url);
  const limit = Math.min(parseInt(url.searchParams.get("limit") ?? "50", 10), 200);

  const events = await recentAbuseEvents(limit, env);
  return jsonResponse(200, { events });
}
