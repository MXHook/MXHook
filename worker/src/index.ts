// index.ts — Cloudflare Worker entry point for MXHook SaaS

import type { Env, EmailEvent } from "./types";
import { processEmailEvent } from "./router";
import { htmlResponse, errorResponse } from "./http";

// Dashboard
import { handleMessages }      from "./dashboard/messages";
import { handleDeliveries }    from "./dashboard/delivery";
import { handleDomains }       from "./dashboard/domains";
import { handleBillingUsage }  from "./dashboard/billing";

// Admin
import { handleListTenants, handleGetTenant, handlePatchTenant } from "./admin/tenants";
import { handleAdminUsage }    from "./admin/usage";
import { handleImpersonate }   from "./admin/support";
import { handleAdminHealth }   from "./admin/system";
import { handleAddBlock, handleAbuseEvents } from "./admin/abuse";

// Status page
import {
  handleStatusJSON,
  handleStatusHTML,
  handleCreateIncident,
  handleResolveIncident,
} from "./statuspage";

// UI assets (embedded)
import dashboardHtml from "./ui/index.html";
import adminHtml     from "./ui/admin.html";

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url    = new URL(req.url);
    const path   = url.pathname;
    const method = req.method;

    // ── Static UI ──────────────────────────────────────────────────────────
    if (method === "GET" && path === "/dashboard") {
      return htmlResponse(200, dashboardHtml as string);
    }
    if (method === "GET" && path === "/admin/panel") {
      return htmlResponse(200, adminHtml as string);
    }

    // ── Status page ────────────────────────────────────────────────────────
    if (method === "GET" && path === "/status") {
      return handleStatusJSON(req, env);
    }
    if (method === "GET" && path === "/status.html") {
      return handleStatusHTML(req, env);
    }

    // ── Well-known ─────────────────────────────────────────────────────────
    if (method === "GET" && path === "/.well-known/security.txt") {
      return new Response(
        `Contact: mailto:security@mxhook.dev\nExpires: 2027-01-01T00:00:00.000Z\n`,
        { headers: { "Content-Type": "text/plain" } },
      );
    }

    // ── Customer dashboard API ─────────────────────────────────────────────
    if (path.startsWith("/api/v1/")) {
      const sub = path.slice("/api/v1".length);

      if (method === "GET" && sub === "/messages") {
        return handleMessages(req, env);
      }
      const deliveriesMatch = sub.match(/^\/messages\/([^/]+)\/deliveries$/);
      if (method === "GET" && deliveriesMatch) {
        return handleDeliveries(req, env, deliveriesMatch[1]);
      }
      if (method === "GET" && sub === "/domains") {
        return handleDomains(req, env);
      }
      if (method === "GET" && sub === "/billing/usage") {
        return handleBillingUsage(req, env);
      }
    }

    // ── Admin API ──────────────────────────────────────────────────────────
    if (path.startsWith("/admin/")) {
      const sub = path.slice("/admin".length);

      if (method === "GET"  && sub === "/tenants") return handleListTenants(req, env);
      if (method === "GET"  && sub === "/usage")   return handleAdminUsage(req, env);
      if (method === "GET"  && sub === "/health")  return handleAdminHealth(req, env);

      if (method === "GET"  && sub === "/abuse/events") return handleAbuseEvents(req, env);
      if (method === "POST" && sub === "/abuse/block")  return handleAddBlock(req, env);

      if (method === "POST" && sub === "/status/incident") return handleCreateIncident(req, env);

      const tenantMatch = sub.match(/^\/tenants\/([^/]+)$/);
      if (tenantMatch) {
        if (method === "GET")   return handleGetTenant(req, env, tenantMatch[1]);
        if (method === "PATCH") return handlePatchTenant(req, env, tenantMatch[1]);
      }

      const impersonateMatch = sub.match(/^\/tenants\/([^/]+)\/impersonate$/);
      if (method === "POST" && impersonateMatch) {
        return handleImpersonate(req, env, impersonateMatch[1]);
      }

      const resolveMatch = sub.match(/^\/status\/resolve\/([^/]+)$/);
      if (method === "POST" && resolveMatch) {
        return handleResolveIncident(req, env, resolveMatch[1]);
      }
    }

    // ── Health ─────────────────────────────────────────────────────────────
    if (method === "GET" && path === "/health") {
      return new Response(JSON.stringify({ status: "ok" }), {
        headers: { "Content-Type": "application/json" },
      });
    }

    return errorResponse(404, "Not found");
  },

  // Queue consumer — process inbound emails from fly.io SMTP edge nodes
  async queue(batch: MessageBatch<unknown>, env: Env): Promise<void> {
    const { checkAbuse } = await import("./abuse");

    for (const msg of batch.messages) {
      const event = msg.body as EmailEvent;
      try {
        const report = await checkAbuse(event, env);

        if (report.action === "block") {
          console.log(`[abuse] blocked message ${event.message_id ?? event.id} score=${report.score}`);
          msg.ack();
          continue;
        }

        if (report.action === "warn") {
          console.warn(`[abuse] warn message ${event.message_id ?? event.id} score=${report.score} reasons=${report.reasons}`);
        }

        await processEmailEvent(event, env);
        msg.ack();
      } catch (err) {
        console.error(`[queue] error processing ${event.message_id ?? event.id}:`, err);
        msg.retry();
      }
    }
  },
} satisfies ExportedHandler<Env>;
