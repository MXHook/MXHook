// auth.ts — session + API key auth for Workers

import type { Env, Session } from "./types";

// Decode JWT-like session from Authorization: Bearer <token>
// Sessions are HMAC-signed JSON payloads stored as base64url.
export async function requireSession(req: Request, env: Env): Promise<Session | null> {
  const authHeader = req.headers.get("Authorization") ?? "";
  const cookieHeader = req.headers.get("Cookie") ?? "";

  let token: string | null = null;

  if (authHeader.startsWith("Bearer ")) {
    token = authHeader.slice(7);
  } else {
    const match = cookieHeader.match(/session=([^;]+)/);
    if (match) token = match[1];
  }

  if (!token) return null;

  try {
    return await verifySessionToken(token, env.SESSION_SECRET);
  } catch {
    return null;
  }
}

export function sessionTenantId(session: Session): string {
  return session.tenantId;
}

export function requireAdminAPIKey(req: Request, env: Env): boolean {
  const key = req.headers.get("X-Admin-Key") ?? req.headers.get("Authorization")?.slice(7);
  return key === env.ADMIN_API_KEY;
}

// Creates a signed session token (base64url(payload).base64url(sig))
export async function createSessionToken(session: Session, secret: string): Promise<string> {
  const payload = btoa(JSON.stringify(session));
  const sig = await hmacSign(payload, secret);
  return `${payload}.${sig}`;
}

async function verifySessionToken(token: string, secret: string): Promise<Session> {
  const dot = token.lastIndexOf(".");
  if (dot === -1) throw new Error("malformed token");

  const payload = token.slice(0, dot);
  const sig = token.slice(dot + 1);
  const expected = await hmacSign(payload, secret);

  if (!timingSafeEqual(sig, expected)) throw new Error("invalid signature");

  const session: Session = JSON.parse(atob(payload));
  if (session.expiresAt < Date.now()) throw new Error("token expired");

  return session;
}

async function hmacSign(data: string, secret: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(data));
  return btoa(String.fromCharCode(...new Uint8Array(sig)));
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}
