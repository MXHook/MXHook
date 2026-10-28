// Webhook delivery with HMAC-SHA256 signing and up to MAX_ATTEMPTS retries.

import type { EmailEvent } from './types.js';

const MAX_ATTEMPTS = 3;
const RETRY_DELAYS_MS = [0, 1_000, 5_000]; // immediate, 1 s, 5 s

export interface DeliveryResult {
  attempt:       number;
  status:        'success' | 'failed';
  httpStatus:    number | null;
  responseBody:  string | null;
}

/**
 * Deliver an email event payload to a webhook URL.
 * Signs the body with HMAC-SHA256 using `secret`.
 * Retries up to MAX_ATTEMPTS times on non-2xx or network error.
 */
export async function deliverWebhook(
  webhookUrl: string,
  secret:     string,
  event:      EmailEvent,
): Promise<DeliveryResult> {
  const body = JSON.stringify(event);
  const signature = await sign(secret, body);

  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const delay = RETRY_DELAYS_MS[attempt - 1] ?? 5_000;
    if (delay > 0) await sleep(delay);

    const result = await trySend(webhookUrl, body, signature, attempt);
    if (result.status === 'success') return result;

    // On last attempt, return the failure result.
    if (attempt === MAX_ATTEMPTS) return result;
  }

  // Unreachable, but satisfies TS.
  return { attempt: MAX_ATTEMPTS, status: 'failed', httpStatus: null, responseBody: null };
}

async function trySend(
  url:       string,
  body:      string,
  signature: string,
  attempt:   number,
): Promise<DeliveryResult> {
  try {
    const resp = await fetch(url, {
      method:  'POST',
      headers: {
        'Content-Type':        'application/json',
        'X-MXHook-Signature':  `sha256=${signature}`,
        'X-MXHook-Attempt':    String(attempt),
      },
      body,
    });

    const responseBody = await resp.text().then(t => t.slice(0, 4096));
    const success = resp.status >= 200 && resp.status < 300;

    return {
      attempt,
      status:       success ? 'success' : 'failed',
      httpStatus:   resp.status,
      responseBody,
    };
  } catch (err) {
    return {
      attempt,
      status:       'failed',
      httpStatus:   null,
      responseBody: String(err),
    };
  }
}

/** HMAC-SHA256 hex digest using Web Crypto. */
async function sign(secret: string, body: string): Promise<string> {
  const enc = new TextEncoder();
  const key = await crypto.subtle.importKey(
    'raw',
    enc.encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const sig = await crypto.subtle.sign('HMAC', key, enc.encode(body));
  return Array.from(new Uint8Array(sig))
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}
