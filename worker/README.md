# Cloudflare Worker (experimental)

This directory holds the Cloudflare Worker for the hosted MXHook service. It is
not part of the self-hosted production release and does not build or ship with
the Go binary. Read
[the hosted Cloudflare target](../docs/architecture.md#hosted-cloudflare-target)
before depending on anything here.

## Topology

The Worker is a **queue consumer**, not a mail server:

```
fly.io SMTP edge nodes → Cloudflare Queue → Worker consumer → D1 / R2 → webhook
```

Edge nodes run the same Go binary as a self-hosted deployment with
`--runtime-role edge` and `--queue-backend cloudflare`. They accept SMTP, then
publish a versioned ingress envelope (`mxhook.message.v1`) to the
`mxhook-email-events` queue. The Worker consumes that queue, decodes and
verifies the envelope, and delivers webhooks.

`src/index.ts` exports three handlers:

| Handler | Purpose |
|---|---|
| `fetch` | Dashboard and admin HTTP API, status page, `/health` |
| `queue` | Batch consumer for `mxhook-email-events` |
| `scheduled` | Hourly cron — DNS TXT verification of pending domains |

The consumer verifies `raw_size` and a SHA-256 digest of the raw message before
decoding (`src/ingress.ts`), acks health probes and abuse-blocked messages, and
calls `msg.retry()` on any error. `max_retries = 3`, after which the message
goes to the `mxhook-email-events-dlq` dead-letter queue.

### Cloudflare Email Routing is not used

This is the most common wrong assumption. The Worker has no `email()` handler,
no `send_email` binding, and imports no `EmailMessage` type — mail never reaches
Cloudflare directly. All inbound mail arrives over SMTP at the fly.io edge and
enters the Worker only as a queue message. Changing that would be a topology
change, not a configuration change.

## Bindings

Declared in `wrangler.toml`:

| Binding | Kind | Purpose |
|---|---|---|
| `DB` | D1 | Tenants, domains, routes, messages, delivery logs |
| `EMAIL_STORE` | R2 | Raw `.eml` storage |

Secrets, set with `wrangler secret put <NAME>`:

- `SESSION_SECRET` — HMAC key for dashboard session tokens
- `ADMIN_API_KEY` — admin panel access key

There is no KV namespace. `EMAIL_QUEUE` is a producer binding on the
`mxhook-email-events` queue, used by the admin health probe in
`src/admin/system.ts`; the worker consumes the same queue.

## Local development

```bash
cd worker
npm install
npm run typecheck     # tsc --noEmit
npm run lint          # eslint src
npm run dev           # wrangler dev
npm run db:migrate    # wrangler d1 migrations apply mxhook
npm run deploy        # wrangler deploy
```

Those five are the only scripts defined in `package.json`; there is no test
script and no test suite. Before `wrangler dev` or `deploy` will work, replace
`database_id = "REPLACE_WITH_D1_DATABASE_ID"` in `wrangler.toml` with a real D1
database id.

## Domain verification

Domain ownership is proved with a DNS TXT record:

```
_mxhook-verify.<domain>  IN TXT  "<token>"
```

`<token>` is the `domains.txt_record` value stored when the domain is
registered. The apex is not consulted, so the token never collides with SPF,
DMARC, or another vendor's apex records.

The hourly cron (`crons = ["0 * * * *"]` in `wrangler.toml`) drives
`scheduled()` in `src/index.ts`, which calls `verifyDomains` in `src/verify.ts`:

1. Select up to `MAX_DOMAINS_PER_RUN` (50) domains with `verified = 0`, oldest
   first.
2. Resolve `_mxhook-verify.<domain>` TXT over DNS-over-HTTPS
   (`https://cloudflare-dns.com/dns-query`, `Accept: application/dns-json`).
   Workers have no `dns` module; fetch-based DoH is the only option.
3. `UPDATE domains SET verified = 1` when any TXT record at that name equals
   the stored token exactly. Multiple records are fine — one match verifies.

**Failure policy.** Verification only ever transitions `0 → 1`; nothing
un-verifies a domain. A DoH transport failure, non-200 response, unparseable
body, or SERVFAIL leaves the domain unverified and is retried on the next tick —
an outage never marks a domain verified, and never silently un-verifies one.
NXDOMAIN and "no TXT record" are treated as a definitive "not yet", not an
error. One failing domain — DNS or D1 — is logged and the batch continues; the
run ends with a `[verify] cron=… examined=… verified=… unverified=… errors=…`
summary line.

There is no on-demand verify endpoint yet. A `GET /api/domains/:id/verify`
endpoint would need the registration endpoint (`POST /api/domains`) and
tenant-scoped domain lookup, neither of which exists; the cron is the only
trigger today.

## Known gaps

- `src/dashboard/domains.ts` selects `d.name`, `d.verification_token`,
  `d.spf_ok`, and `d.dkim_ok`. None of those columns exist in any migration —
  the table has `domain` and `txt_record` — so `GET /api/v1/domains` fails
  against a database built from `migrations/`.
- Migrations jump from `0001` to `0006`; `0002`–`0005` are absent from the
  directory, so a fresh D1 database is not reproducible from this tree alone.

## Status

Experimental, carrying the disclaimer from
[the architecture document](../docs/architecture.md#hosted-cloudflare-target).
Unresolved before this could be operated: inline Queue payload limits, resource
provisioning, tenant isolation, and D1/R2 contract tests.

## Relationship to the self-hosted binary

The Go binary is the supported product and needs none of this. It runs the whole
pipeline in one process — ingress, queue consumer, parser, router, webhook
delivery worker, and control API — against SQLite or Postgres. See
[the CLI reference](../docs/cli.md) and
[operations](../docs/operations.md).

The two share only the versioned ingress envelope. Selecting
`--queue-backend cloudflare` turns a Go deployment into an edge publisher and
requires a separately operated consumer; it is not a drop-in backend swap.
