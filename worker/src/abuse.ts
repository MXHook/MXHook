// abuse.ts — rate limiting, blocklist, and spam scoring

import type { Env } from "./types";

export type AbuseAction = "allow" | "warn" | "block";

export interface AbuseReport {
  score: number;
  reasons: string[];
  action: AbuseAction;
}

// Spam keywords: pairs of [pattern, score]
const SPAM_SIGNALS: Array<[RegExp, number, string]> = [
  [/\bcasino\b/i,          3, "keyword:casino"],
  [/\bslot.?machine\b/i,   3, "keyword:slot-machine"],
  [/\bpoker\b/i,           2, "keyword:poker"],
  [/\bpills\b/i,           3, "keyword:pills"],
  [/\bviagra\b/i,          5, "keyword:viagra"],
  [/\bcialis\b/i,          5, "keyword:cialis"],
  [/\bpharmacy\b/i,        2, "keyword:pharmacy"],
  [/\bcryptocurrency\b/i,  2, "keyword:crypto"],
  [/\bcrypto.?invest/i,    4, "keyword:crypto-invest"],
  [/\bget.?rich.?quick\b/i, 5, "keyword:get-rich-quick"],
  [/\bmake.?\$\d+/i,       3, "keyword:make-money"],
  [/\bweight.?loss\b/i,    2, "keyword:weight-loss"],
  [/\bclickbait\b/i,       2, "keyword:clickbait"],
  [/\bunsubscribe.{0,20}link/i, 1, "pattern:unsub-link"],
  [/https?:\/\/[^\s]{60,}/i, 1, "pattern:long-url"],
];

const RATE_LIMIT_WINDOW_S = 3600; // 1 hour
const RATE_LIMIT_MAX      = 100;  // messages per window per IP

export interface InboundMessage {
  id: string;
  senderIp: string;
  from: string;
  to: string[];
  subject: string;
  body: string;
  domain: string;
  tenantId: string;
  receivedAt: string;
}

// checkAbuse returns an AbuseReport for the given message.
export async function checkAbuse(msg: InboundMessage, env: Env): Promise<AbuseReport> {
  const reasons: string[] = [];
  let score = 0;

  // 1. Rate limiting by sender IP
  const rateExceeded = await checkRateLimit(msg.senderIp, env);
  if (rateExceeded) {
    score += 10;
    reasons.push(`rate_limit:ip:${msg.senderIp}`);
  }

  // 2. Blocklist check (email + domain patterns)
  const blocked = await checkBlocklist(msg.from, env);
  if (blocked) {
    score += 20;
    reasons.push(`blocklist:${msg.from}`);
  }

  // 3. Spam scoring on subject + body
  const spamScore = scoreSpam(msg.subject, msg.body, reasons);
  score += spamScore;

  // Determine action
  let action: AbuseAction;
  if (score >= 15 || blocked) {
    action = "block";
  } else if (score >= 5) {
    action = "warn";
  } else {
    action = "allow";
  }

  // Log to abuse_events if not allowed
  if (action !== "allow") {
    await logAbuseEvent(msg, score, reasons, action, env);
  }

  return { score, reasons, action };
}

// Rate limit: count msgs from IP in last hour via D1.
async function checkRateLimit(ip: string, env: Env): Promise<boolean> {
  const windowStart = new Date(Date.now() - RATE_LIMIT_WINDOW_S * 1000).toISOString();

  const row = await env.DB.prepare(
    "SELECT COUNT(*) AS cnt FROM abuse_events WHERE sender_ip = ? AND created_at >= ?",
  )
    .bind(ip, windowStart)
    .first<{ cnt: number }>();

  return (row?.cnt ?? 0) >= RATE_LIMIT_MAX;
}

// Blocklist: match from address or domain against blocked_senders.
async function checkBlocklist(from: string, env: Env): Promise<boolean> {
  const domain = from.includes("@") ? from.split("@")[1] : from;

  const row = await env.DB.prepare(
    `SELECT id FROM blocked_senders
     WHERE pattern = ? OR pattern = ? OR ? LIKE pattern
     LIMIT 1`,
  )
    .bind(from.toLowerCase(), domain.toLowerCase(), from.toLowerCase())
    .first();

  return row !== null;
}

// Score spam signals on subject + body text.
function scoreSpam(subject: string, body: string, reasons: string[]): number {
  const text = `${subject} ${body}`;
  let total = 0;
  for (const [re, pts, label] of SPAM_SIGNALS) {
    if (re.test(text)) {
      total += pts;
      reasons.push(label);
    }
  }
  return total;
}

async function logAbuseEvent(
  msg: InboundMessage,
  score: number,
  reasons: string[],
  action: AbuseAction,
  env: Env,
): Promise<void> {
  await env.DB.prepare(
    `INSERT INTO abuse_events
       (message_id, tenant_id, sender_ip, from_addr, score, reasons, action, created_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
  )
    .bind(
      msg.id,
      msg.tenantId,
      msg.senderIp,
      msg.from,
      score,
      JSON.stringify(reasons),
      action,
      new Date().toISOString(),
    )
    .run()
    .catch(() => {}); // non-fatal
}

// Admin: add a pattern to the blocklist.
export async function addBlocklistEntry(
  pattern: string,
  reason: string,
  env: Env,
): Promise<void> {
  await env.DB.prepare(
    "INSERT OR REPLACE INTO blocked_senders (pattern, reason, created_at) VALUES (?, ?, ?)",
  )
    .bind(pattern.toLowerCase(), reason, new Date().toISOString())
    .run();
}

// Admin: fetch recent abuse events.
export async function recentAbuseEvents(
  limit: number,
  env: Env,
): Promise<unknown[]> {
  const result = await env.DB.prepare(
    `SELECT * FROM abuse_events ORDER BY created_at DESC LIMIT ?`,
  )
    .bind(limit)
    .all();
  return result.results ?? [];
}
