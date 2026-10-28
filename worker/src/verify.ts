// verify.ts — DNS TXT domain-ownership verification.
//
// Convention:
//
//   _mxhook-verify.<domain>  IN TXT  "<token>"
//
// `<token>` is the `domains.txt_record` value stored at registration. A domain
// is verified when any TXT record at that name equals the stored token exactly
// (after unquoting and trimming). Multiple TXT records are allowed — any one
// match verifies. The apex is not consulted.
//
// Failure policy: verification only ever transitions 0 → 1. Any DoH transport
// error, non-200 response, malformed JSON, NXDOMAIN, or absent/mismatched TXT
// record leaves the domain unverified for the next cron run. A single bad
// domain never aborts the batch and never marks anything verified.

import type { DomainRow } from './db.js';
import { listUnverifiedDomains, markDomainVerified } from './db.js';

/** DNS label prefixed to the domain to locate the ownership token. */
export const VERIFY_PREFIX = '_mxhook-verify';

/** Cloudflare DNS-over-HTTPS JSON endpoint. Workers have no `dns` module. */
export const DOH_ENDPOINT = 'https://cloudflare-dns.com/dns-query';

/** Max domains examined per cron run — bounds D1 rows and DoH subrequests. */
export const MAX_DOMAINS_PER_RUN = 50;

/** DNS RCODE 3 — NXDOMAIN. */
const RCODE_NXDOMAIN = 3;

/** TXT record type. */
const TYPE_TXT = 16;

/** Minimal shape of the `fetch` we need; injectable for tests. */
export type FetchLike = (
  input: string,
  init?: { headers?: Record<string, string> },
) => Promise<{ ok: boolean; status: number; json(): Promise<unknown> }>;

/** Outcome of resolving TXT records for one name. */
export interface TxtLookup {
  /** TXT strings found; empty on NXDOMAIN or no TXT records. */
  records: string[];
  /** Set when the lookup could not be completed (transport/protocol failure). */
  error?: string;
}

/** The verification name for a domain, e.g. `_mxhook-verify.example.com`. */
export function verifyName(domain: string): string {
  return `${VERIFY_PREFIX}.${domain}`;
}

/**
 * Resolve TXT records over DNS-over-HTTPS.
 *
 * Never throws. A completed lookup with no matching name (NXDOMAIN) or no TXT
 * records returns an empty `records` list with no `error`; anything that
 * prevented an answer returns `error` so the caller can leave the domain alone.
 */
export async function resolveTxt(
  name: string,
  fetchImpl: FetchLike,
): Promise<TxtLookup> {
  let res: Awaited<ReturnType<FetchLike>>;
  try {
    res = await fetchImpl(
      `${DOH_ENDPOINT}?name=${encodeURIComponent(name)}&type=TXT`,
      { headers: { Accept: 'application/dns-json' } },
    );
  } catch (err) {
    return { records: [], error: `doh request failed: ${String(err)}` };
  }

  if (!res.ok) {
    return { records: [], error: `doh status ${res.status}` };
  }

  let body: unknown;
  try {
    body = await res.json();
  } catch (err) {
    return { records: [], error: `doh body not json: ${String(err)}` };
  }

  if (typeof body !== 'object' || body === null) {
    return { records: [], error: 'doh body not an object' };
  }

  const status = (body as { Status?: unknown }).Status;
  if (typeof status !== 'number') {
    return { records: [], error: 'doh body missing Status' };
  }
  // NXDOMAIN is a definitive "no such name" — not an error, just unverified.
  if (status === RCODE_NXDOMAIN) return { records: [] };
  if (status !== 0) return { records: [], error: `doh rcode ${status}` };

  const answer = (body as { Answer?: unknown }).Answer;
  if (!Array.isArray(answer)) return { records: [] };

  const records: string[] = [];
  for (const entry of answer) {
    if (typeof entry !== 'object' || entry === null) continue;
    const { type, data } = entry as { type?: unknown; data?: unknown };
    // Ignore CNAME hops and any other type in the answer chain.
    if (type !== TYPE_TXT) continue;
    if (typeof data !== 'string') continue;
    records.push(unquoteTxt(data));
  }
  return { records };
}

/**
 * Strip the wire quoting DoH applies to TXT rdata.
 *
 * Long TXT values arrive as multiple quoted strings ("abc" "def") which
 * concatenate per RFC 1035; short ones as a single quoted string. Unquoted
 * input is returned trimmed.
 */
export function unquoteTxt(data: string): string {
  const chunks = data.match(/"(?:[^"\\]|\\.)*"/g);
  if (!chunks) return data.trim();
  return chunks
    .map(c => c.slice(1, -1).replace(/\\(.)/g, '$1'))
    .join('')
    .trim();
}

/** True when any TXT record equals the expected token exactly. */
export function tokenMatches(records: string[], token: string): boolean {
  const expected = token.trim();
  if (expected === '') return false;
  return records.some(r => r === expected);
}

/** Per-domain verification outcome. */
export type VerifyOutcome = 'verified' | 'unverified' | 'error';

export interface VerifyResult {
  domain: string;
  outcome: VerifyOutcome;
  reason?: string;
}

/**
 * Check one domain's TXT record against its stored token.
 *
 * Pure with respect to storage — it reads DNS only and reports what it found.
 * Returns `error` (never `unverified`) when the lookup itself failed, so the
 * caller can distinguish "token absent" from "we could not tell".
 */
export async function checkDomain(
  row: Pick<DomainRow, 'domain' | 'txt_record'>,
  fetchImpl: FetchLike,
): Promise<VerifyResult> {
  const lookup = await resolveTxt(verifyName(row.domain), fetchImpl);

  if (lookup.error !== undefined) {
    return { domain: row.domain, outcome: 'error', reason: lookup.error };
  }
  if (tokenMatches(lookup.records, row.txt_record)) {
    return { domain: row.domain, outcome: 'verified' };
  }
  return {
    domain: row.domain,
    outcome: 'unverified',
    reason: lookup.records.length === 0 ? 'no txt records' : 'token mismatch',
  };
}

export interface RunSummary {
  examined: number;
  verified: number;
  unverified: number;
  errors: number;
  results: VerifyResult[];
}

/**
 * Verify a batch of domains, writing `verified = 1` only on a confirmed match.
 *
 * One domain's failure — DNS or D1 — is recorded and the batch continues.
 */
export async function verifyDomains(
  db: D1Database,
  fetchImpl: FetchLike,
  limit: number = MAX_DOMAINS_PER_RUN,
): Promise<RunSummary> {
  const rows = await listUnverifiedDomains(db, limit);
  const summary: RunSummary = {
    examined: rows.length,
    verified: 0,
    unverified: 0,
    errors: 0,
    results: [],
  };

  for (const row of rows) {
    let result: VerifyResult;
    try {
      result = await checkDomain(row, fetchImpl);
      if (result.outcome === 'verified') {
        await markDomainVerified(db, row.id);
      }
    } catch (err) {
      result = { domain: row.domain, outcome: 'error', reason: String(err) };
    }

    summary.results.push(result);
    if (result.outcome === 'verified') summary.verified++;
    else if (result.outcome === 'unverified') summary.unverified++;
    else summary.errors++;
  }

  return summary;
}
