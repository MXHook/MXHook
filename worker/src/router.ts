// Route matching and full email event processing pipeline.

import type { Env, EmailEvent, Route } from './types.js';
import { findDomain, findRoutesByDomain, insertMessage, insertDeliveryLog } from './db.js';
import { deliverWebhook } from './webhook.js';

/** Process one email event: resolve domain → match routes → deliver webhooks. */
export async function processEmailEvent(event: EmailEvent, env: Env): Promise<void> {
  // Determine recipient domains from To list.
  const domains = extractDomains(event.to);
  if (domains.length === 0) {
    console.warn('router: no recipient domains', event.message_id);
    return;
  }

  for (const domain of domains) {
    const domainRow = await findDomain(env.DB, domain);
    if (!domainRow) {
      console.info('router: domain not found or unverified', domain);
      continue;
    }

    const routes = await findRoutesByDomain(env.DB, domainRow.id);
    const matched = matchRoutes(routes, event.to);

    if (matched.length === 0) {
      console.info('router: no matching routes', domain);
      continue;
    }

    // Store raw email in R2.
    const r2Key = `${domainRow.tenant_id}/${event.message_id}`;
    await storeRaw(env.EMAIL_STORE, r2Key, event.raw);

    // Persist message record.
    const msgId = crypto.randomUUID();
    await insertMessage(env.DB, {
      id:          msgId,
      tenant_id:   domainRow.tenant_id,
      domain_id:   domainRow.id,
      message_id:  event.message_id,
      from_addr:   event.from,
      to_addr:     event.to.join(','),
      subject:     event.subject,
      received_at: event.received_at,
      r2_key:      r2Key,
    });

    // Deliver to each matched route.
    for (const route of matched) {
      const enriched: EmailEvent = { ...event, tenant_id: domainRow.tenant_id };
      const result = await deliverWebhook(route.webhook_url, route.webhook_secret, enriched);

      await insertDeliveryLog(env.DB, {
        id:            crypto.randomUUID(),
        message_id:    msgId,
        route_id:      route.id,
        attempt:       result.attempt,
        status:        result.status,
        http_status:   result.httpStatus,
        response_body: result.responseBody,
      });

      console.info(
        'router: delivered',
        event.message_id,
        '→', route.webhook_url,
        'status', result.httpStatus,
        'attempt', result.attempt,
      );
    }
  }
}

/** Match routes whose match_pattern applies to at least one To address. */
function matchRoutes(routes: Route[], toAddrs: string[]): Route[] {
  return routes.filter(r => toAddrs.some(addr => patternMatches(r.match_pattern, addr)));
}

/**
 * Pattern matching — supports:
 *   - exact address:      "alerts@example.com"
 *   - wildcard prefix:    "*@example.com"
 *   - local-part prefix:  "support+*@example.com"
 */
function patternMatches(pattern: string, addr: string): boolean {
  if (pattern === '*') return true;
  if (!pattern.includes('*')) return pattern.toLowerCase() === addr.toLowerCase();

  // Convert glob to regex: escape special chars except *.
  const re = new RegExp(
    '^' +
      pattern
        .toLowerCase()
        .replace(/[.+^${}()|[\]\\]/g, '\\$&')
        .replace(/\*/g, '.*') +
      '$',
    'i',
  );
  return re.test(addr);
}

/** Extract unique hostnames from a list of email addresses. */
function extractDomains(addrs: string[]): string[] {
  const seen = new Set<string>();
  for (const addr of addrs) {
    const at = addr.lastIndexOf('@');
    if (at !== -1) seen.add(addr.slice(at + 1).toLowerCase());
  }
  return [...seen];
}

/** Store raw email bytes in R2. */
async function storeRaw(bucket: R2Bucket, key: string, raw: string): Promise<void> {
  await bucket.put(key, raw, {
    httpMetadata: { contentType: 'message/rfc822' },
  });
}
