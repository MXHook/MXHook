// statuspage.ts — public /status and admin incident management

import type { Env } from "./types";
import { requireAdminAPIKey } from "./auth";
import { jsonResponse, errorResponse, htmlResponse } from "./http";

type ComponentStatus = "operational" | "degraded" | "outage";

const COMPONENTS = ["smtp_ingestion", "webhook_delivery", "d1", "r2"] as const;
type Component = (typeof COMPONENTS)[number];

interface UptimeEvent {
  id: number;
  component: Component;
  status: ComponentStatus;
  message: string;
  started_at: string;
  resolved_at: string | null;
}

// GET /status — JSON service health
export async function handleStatusJSON(_req: Request, env: Env): Promise<Response> {
  const data = await buildStatusData(env);
  const allOp = Object.values(data.components).every(c => c.status === "operational");
  return jsonResponse(allOp ? 200 : 503, data);
}

// GET /status.html — HTML status page
export async function handleStatusHTML(_req: Request, env: Env): Promise<Response> {
  const data = await buildStatusData(env);
  return htmlResponse(200, renderStatusPage(data));
}

// POST /admin/status/incident — create incident (admin only)
export async function handleCreateIncident(req: Request, env: Env): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  let body: { component: Component; status: ComponentStatus; message: string };
  try {
    body = await req.json() as typeof body;
  } catch {
    return errorResponse(400, "Invalid JSON");
  }

  if (!COMPONENTS.includes(body.component)) {
    return errorResponse(400, `component must be one of: ${COMPONENTS.join(", ")}`);
  }
  if (!["operational", "degraded", "outage"].includes(body.status)) {
    return errorResponse(400, "Invalid status");
  }

  const result = await env.DB.prepare(
    `INSERT INTO uptime_events (component, status, message, started_at)
     VALUES (?, ?, ?, ?)`,
  )
    .bind(body.component, body.status, body.message ?? "", new Date().toISOString())
    .run();

  return jsonResponse(201, { id: result.meta.last_row_id, ...body });
}

// POST /admin/status/resolve/:id — resolve incident
export async function handleResolveIncident(
  req: Request,
  env: Env,
  id: string,
): Promise<Response> {
  if (!requireAdminAPIKey(req, env)) return errorResponse(403, "Forbidden");

  const existing = await env.DB.prepare(
    "SELECT id FROM uptime_events WHERE id = ? AND resolved_at IS NULL",
  )
    .bind(id)
    .first();

  if (!existing) return errorResponse(404, "Incident not found or already resolved");

  await env.DB.prepare(
    "UPDATE uptime_events SET resolved_at = ? WHERE id = ?",
  )
    .bind(new Date().toISOString(), id)
    .run();

  return jsonResponse(200, { id, resolved: true });
}

// Compute status data: current state + 90-day uptime per component + recent incidents
async function buildStatusData(env: Env) {
  const ninetyDaysAgo = new Date(Date.now() - 90 * 24 * 60 * 60 * 1000).toISOString();

  // Active (unresolved) incidents
  const activeResult = await env.DB.prepare(
    `SELECT id, component, status, message, started_at
     FROM uptime_events
     WHERE resolved_at IS NULL
     ORDER BY started_at DESC`,
  ).all<UptimeEvent>();
  const active: UptimeEvent[] = (activeResult.results ?? []) as UptimeEvent[];

  // Recent resolved incidents (last 7 days)
  const recentResult = await env.DB.prepare(
    `SELECT id, component, status, message, started_at, resolved_at
     FROM uptime_events
     WHERE resolved_at IS NOT NULL AND started_at >= ?
     ORDER BY started_at DESC
     LIMIT 20`,
  )
    .bind(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString())
    .all<UptimeEvent>();
  const recent: UptimeEvent[] = (recentResult.results ?? []) as UptimeEvent[];

  // Uptime: % of 90-day window NOT in outage
  const uptimeResult = await env.DB.prepare(
    `SELECT component,
            SUM(
              CAST((julianday(COALESCE(resolved_at, datetime('now'))) -
                    julianday(MAX(started_at, ?))) * 86400 AS INTEGER)
            ) AS outage_seconds
     FROM uptime_events
     WHERE status = 'outage' AND started_at >= ?
     GROUP BY component`,
  )
    .bind(ninetyDaysAgo, ninetyDaysAgo)
    .all<{ component: string; outage_seconds: number }>();

  const outageMap: Record<string, number> = {};
  (uptimeResult.results ?? []).forEach(r => {
    outageMap[r.component] = r.outage_seconds ?? 0;
  });

  const windowSeconds = 90 * 24 * 3600;
  const components: Record<string, { status: ComponentStatus; uptime_pct: number }> = {};

  for (const comp of COMPONENTS) {
    const activeIncident = active.find(a => a.component === comp);
    const outageSec = outageMap[comp] ?? 0;
    const uptimePct = Math.max(0, ((windowSeconds - outageSec) / windowSeconds) * 100);

    components[comp] = {
      status: activeIncident ? activeIncident.status : "operational",
      uptime_pct: Math.round(uptimePct * 100) / 100,
    };
  }

  return {
    updated_at: new Date().toISOString(),
    components,
    active_incidents: active,
    recent_incidents: recent,
  };
}

// Minimal HTML status page — no external deps
function renderStatusPage(data: ReturnType<typeof buildStatusData> extends Promise<infer T> ? T : never): string {
  const statusColor = (s: ComponentStatus) => ({
    operational: "#00e5a0",
    degraded:    "#f59e0b",
    outage:      "#ff4d4d",
  }[s] ?? "#6b7280");

  const statusLabel = (s: ComponentStatus) => ({
    operational: "Operational",
    degraded:    "Degraded",
    outage:      "Outage",
  }[s] ?? s);

  const allOp = Object.values(data.components).every(c => c.status === "operational");

  const compRows = Object.entries(data.components).map(([name, c]) => `
    <div style="display:flex;align-items:center;justify-content:space-between;
         padding:14px 0;border-bottom:1px solid #1e2128;">
      <div>
        <div style="font-weight:500;">${name.replace(/_/g, " ")}</div>
        <div style="font-size:12px;color:#6b7280;margin-top:2px;font-family:monospace;">
          ${c.uptime_pct}% uptime (90d)
        </div>
      </div>
      <span style="background:${statusColor(c.status)}22;color:${statusColor(c.status)};
            padding:3px 10px;border-radius:3px;font-size:12px;font-family:monospace;font-weight:500;">
        ${statusLabel(c.status)}
      </span>
    </div>
  `).join("");

  const incidentRows = data.active_incidents.length
    ? data.active_incidents.map(i => `
      <div style="padding:12px 0;border-bottom:1px solid #1e2128;">
        <div style="display:flex;gap:8px;align-items:center;">
          <span style="color:#f59e0b;font-size:12px;font-family:monospace;">[ACTIVE]</span>
          <strong style="font-size:14px;">${i.component.replace(/_/g, " ")}</strong>
        </div>
        <div style="font-size:13px;color:#b0b5c0;margin-top:4px;">${i.message}</div>
        <div style="font-size:11px;color:#6b7280;margin-top:4px;font-family:monospace;">
          Since: ${new Date(i.started_at).toLocaleString()}
        </div>
      </div>
    `).join("")
    : "<div style='color:#6b7280;font-size:13px;padding:16px 0;'>No active incidents.</div>";

  const recentRows = data.recent_incidents.length
    ? data.recent_incidents.map(i => `
      <div style="padding:10px 0;border-bottom:1px solid #1e2128;">
        <div style="display:flex;gap:8px;align-items:center;">
          <span style="color:#00e5a0;font-size:11px;font-family:monospace;">[RESOLVED]</span>
          <span style="font-size:13px;">${i.component.replace(/_/g, " ")}</span>
        </div>
        <div style="font-size:12px;color:#b0b5c0;margin-top:2px;">${i.message}</div>
        <div style="font-size:11px;color:#6b7280;margin-top:2px;font-family:monospace;">
          ${new Date(i.started_at).toLocaleString()} — ${i.resolved_at ? new Date(i.resolved_at).toLocaleString() : ""}
        </div>
      </div>
    `).join("")
    : "";

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>MXHook Status</title>
  <link href="https://fonts.bunny.net/css?family=ibm-plex-mono:400,500|ibm-plex-sans:400,500,600" rel="stylesheet" />
  <style>
    body { background:#0d0f14; color:#e8eaf0; font-family:'IBM Plex Sans',sans-serif;
           font-size:14px; margin:0; padding:0; }
    .wrap { max-width:720px; margin:0 auto; padding:40px 24px; }
    .banner {
      background:${allOp ? "rgba(0,229,160,0.08)" : "rgba(245,158,11,0.08)"};
      border:1px solid ${allOp ? "rgba(0,229,160,0.3)" : "rgba(245,158,11,0.3)"};
      border-radius:6px; padding:20px 24px; margin-bottom:32px;
      display:flex; align-items:center; gap:12px;
    }
    .banner-dot {
      width:12px; height:12px; border-radius:50%;
      background:${allOp ? "#00e5a0" : "#f59e0b"};
      flex-shrink:0;
    }
    .banner-title { font-size:18px; font-weight:600; }
    .section-title { font-size:13px; text-transform:uppercase; letter-spacing:0.08em;
                     color:#6b7280; margin-bottom:12px; }
    .card { background:#161921; border:1px solid #1e2128; border-radius:6px;
            padding:0 20px; margin-bottom:24px; }
    .updated { font-size:11px; color:#6b7280; font-family:monospace; text-align:right; margin-top:16px; }
    a { color:#0091ff; }
  </style>
</head>
<body>
<div class="wrap">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:32px;">
    <div style="font-family:'IBM Plex Mono',monospace;font-size:20px;color:#00e5a0;">
      mx<span style="color:#e8eaf0;">hook</span>
    </div>
    <a href="/status" style="font-size:12px;font-family:monospace;">JSON API</a>
  </div>

  <div class="banner">
    <div class="banner-dot"></div>
    <div class="banner-title">
      ${allOp ? "All systems operational" : "Service disruption in progress"}
    </div>
  </div>

  <div class="section-title">Components</div>
  <div class="card">${compRows}</div>

  <div class="section-title">Active Incidents</div>
  <div class="card" style="padding:0 20px;">${incidentRows}</div>

  ${data.recent_incidents.length ? `
  <div class="section-title">Recent Incidents (7d)</div>
  <div class="card" style="padding:0 20px;">${recentRows}</div>
  ` : ""}

  <div class="updated">Last updated: ${new Date(data.updated_at).toLocaleString()}</div>
</div>
</body>
</html>`;
}
