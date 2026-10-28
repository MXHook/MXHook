import type { EmailEvent, IngressMessageV1 } from "./types";

const schemaVersion = "mxhook.message.v1";

export async function decodeIngressMessage(input: unknown): Promise<EmailEvent> {
  if (!isRecord(input)) throw new Error("ingress: message must be an object");
  // Version switch: v1 envelopes carry schema_version; the legacy edge
  // format predates versioning and has none. Anything else is a format we
  // do not understand and must not guess at.
  if (input.schema_version === undefined) {
    return decodeLegacyMessage(input);
  }
  if (input.schema_version !== schemaVersion) {
    throw new Error(`ingress: unsupported schema ${String(input.schema_version)}`);
  }
  return decodeV1Message(input);
}

async function decodeV1Message(input: Record<string, unknown>): Promise<EmailEvent> {
  const message = input as unknown as IngressMessageV1;
  requireString(message.id, "id");
  requireString(message.trace_id, "trace_id");
  requireString(message.source, "source");
  requireString(message.received_at, "received_at");
  requireString(message.raw, "raw");
  requireString(message.raw_sha256, "raw_sha256");
  if (!Array.isArray(message.envelope_to) || message.envelope_to.length === 0 ||
      message.envelope_to.some(value => typeof value !== "string" || value === "")) {
    throw new Error("ingress: envelope_to must contain recipients");
  }

  const rawBytes = decodeBase64(message.raw);
  if (!Number.isSafeInteger(message.raw_size) || message.raw_size !== rawBytes.byteLength) {
    throw new Error("ingress: raw size mismatch");
  }
  const digest = await crypto.subtle.digest("SHA-256", rawBytes);
  if (hex(new Uint8Array(digest)) !== message.raw_sha256) {
    throw new Error("ingress: raw digest mismatch");
  }

  const raw = new TextDecoder().decode(rawBytes);
  const recipientDomain = domainOf(message.envelope_to[0] ?? "");
  return {
    message_id: message.id,
    // envelope_from is legitimately empty for bounces (null sender);
    // coerce non-strings to "" so the field never vanishes from JSON.
    from: asString(message.envelope_from),
    to: [...message.envelope_to],
    subject: header(raw, "subject"),
    received_at: message.received_at,
    raw,
    id: message.id,
    senderIp: "unknown",
    body: raw,
    domain: recipientDomain,
    tenantId: "",
    receivedAt: message.received_at,
  };
}

// decodeLegacyMessage accepts the pre-v1 edge event so the in-flight
// backlog published before the envelope upgrade drains instead of
// dead-lettering. Remove once every producer publishes mxhook.message.v1.
function decodeLegacyMessage(input: Record<string, unknown>): EmailEvent {
  requireString(input.message_id, "message_id");
  requireString(input.raw, "raw");
  if (!Array.isArray(input.to) || input.to.length === 0 ||
      input.to.some(value => typeof value !== "string" || value === "")) {
    throw new Error("ingress: to must contain recipients");
  }
  const to = input.to as string[];
  const raw = input.raw;
  const receivedAt = asString(input.received_at) || new Date().toISOString();
  const recipientDomain = asString(input.domain) || domainOf(to[0] ?? "");
  return {
    message_id: input.message_id,
    from: asString(input.from),
    to: [...to],
    subject: asString(input.subject) || header(raw, "subject"),
    received_at: receivedAt,
    raw,
    id: asString(input.id) || input.message_id,
    senderIp: asString(input.senderIp) || "unknown",
    body: asString(input.body) || raw,
    domain: recipientDomain,
    tenantId: asString(input.tenantId),
    receivedAt,
  };
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requireString(value: unknown, field: string): asserts value is string {
  if (typeof value !== "string" || value === "") throw new Error(`ingress: ${field} is required`);
}

function decodeBase64(value: string): Uint8Array {
  let decoded: string;
  try {
    decoded = atob(value);
  } catch {
    throw new Error("ingress: raw is not valid base64");
  }
  return Uint8Array.from(decoded, character => character.charCodeAt(0));
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("");
}

function domainOf(address: string): string {
  const separator = address.lastIndexOf("@");
  return separator < 0 ? "" : address.slice(separator + 1).toLowerCase();
}

function header(raw: string, name: string): string {
  const headerBlock = raw.split(/\r?\n\r?\n/, 1)[0] ?? "";
  const unfolded = headerBlock.replace(/\r?\n[ \t]+/g, " ");
  for (const line of unfolded.split(/\r?\n/)) {
    const separator = line.indexOf(":");
    if (separator > 0 && line.slice(0, separator).toLowerCase() === name) {
      return line.slice(separator + 1).trim();
    }
  }
  return "";
}
