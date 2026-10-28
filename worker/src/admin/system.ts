// admin/system.ts — GET /admin/health (connectivity checks)

import type { Env } from "../types";
import { requireAdminAPIKey } from "../auth";
import { jsonResponse, errorResponse } from "../http";

interface ComponentHealth {
  status: "ok" | "degraded" | "down";
  latency_ms?: number;
  error?: string;
}

async function checkD1(env: Env): Promise<ComponentHealth> {
  const start = Date.now();
  try {
    await env.DB.prepare("SELECT 1").first();
    return { status: "ok", latency_ms: Date.now() - start };
  } catch (e) {
    return { status: "down", error: String(e), latency_ms: Date.now() - start };
  }
}

async function checkR2(env: Env): Promise<ComponentHealth> {
  const start = Date.now();
  try {
    const probe = `_health/${Date.now()}`;
    await env.EMAIL_STORE.put(probe, "1");
    await env.EMAIL_STORE.delete(probe);
    return { status: "ok", latency_ms: Date.now() - start };
  } catch (e) {
    return { status: "down", error: String(e), latency_ms: Date.now() - start };
  }
}

async function checkQueue(env: Env): Promise<ComponentHealth> {
  const start = Date.now();
  try {
    // Send a no-op probe message to verify queue is reachable.
    await env.EMAIL_QUEUE.send({ type: "_health_probe" });
    return { status: "ok", latency_ms: Date.now() - start };
  } catch (e) {
    return { status: "down", error: String(e), latency_ms: Date.now() - start };
  }
}

export async function handleAdminHealth(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const [d1, r2, queue] = await Promise.all([
    checkD1(env),
    checkR2(env),
    checkQueue(env),
  ]);

  const allOk = [d1, r2, queue].every(c => c.status === "ok");
  const anyDown = [d1, r2, queue].some(c => c.status === "down");
  const overall = allOk ? "ok" : anyDown ? "degraded" : "degraded";

  return jsonResponse(allOk ? 200 : 503, {
    status: overall,
    timestamp: new Date().toISOString(),
    components: { d1, r2, queue },
  });
}
