# MXHook architecture

MXHook is a Go 1.27 single binary. The self-hosted runtime uses SQLite by
default and Postgres optionally. SMTP and Maildir share one versioned ingress
contract and one durable processing pipeline.

## Production pipeline

```text
SMTP / Maildir
  -> versioned ingress envelope (mxhook.message.v1)
  -> durable ingress queue (SQLite or Postgres)
  -> lease-fenced intake worker
  -> owner-only raw message storage
  -> MIME parser and route matcher
  -> transactional event + delivery outbox
  -> lease-fenced delivery worker
  -> HTTPS webhook with HMAC and stable delivery ID
```

An ingress source reports success only after durable queue insertion. The intake
worker acknowledges the queue item only after event and delivery rows commit.
A crash before acknowledgement causes safe, idempotent redelivery. Webhook
attempts and retry schedules live in the database, so restarts do not reset
them.

Delivery is at-least-once. Receivers must deduplicate on
`X-MXHook-Delivery-ID`.

## Choosing an ingestion path

Three ingress paths exist. Only the first two are part of the self-hosted
production release.

| | Own MX (SMTP) | Maildir | Cloudflare worker |
|---|---|---|---|
| Maturity | production | production | **experimental**, hosted-service work |
| Enabled by | `--smtp-addr` (default `0.0.0.0:25`) | `--maildir <root>` | `--queue-backend cloudflare` plus a separately deployed consumer |
| Inbound port 25 | required, open to the internet | none | none on your host |
| Static IP and PTR | needed for deliverability; many hosts block outbound and inbound 25 | not needed | not needed |
| TLS certificate | needed for STARTTLS (`--smtp-tls-cert`, `--smtp-tls-key`, `--smtp-require-tls`); MXHook does not do implicit TLS | not needed | terminated at the edge |
| DNS | MX record for each accepted domain | whatever the upstream mail host requires | edge-node dependent |
| Envelope recipient | authoritative, from the SMTP `RCPT TO` command | reconstructed from a header (`--maildir-recipient-header`, default `Delivered-To`) or `--maildir-default-recipient` | carried in the ingress envelope |
| Trust boundary | you run the MTA | the local MTA writing the Maildir must be the one injecting the recipient header | edge publisher must be authenticated |
| Setup guide | [DNS setup](dns-setup.md) | [Maildir ingestion](maildir.md) | none; see the caveats below |

Pick **own MX** when you control a host that can accept inbound port 25, want
the envelope recipient straight from `RCPT TO` with no header trust question,
and are willing to own an MX record, a certificate, and reverse DNS.

Pick **Maildir** when port 25 is blocked, unavailable, or not worth operating:
an existing mail server, a provider such as exe.dev, or a forwarding rule from
Google Workspace or Fastmail drops messages onto the filesystem and MXHook
drains them. This is the lowest-friction on-ramp, and it sidesteps every port
25, PTR, and certificate concern. Its one hard requirement is a recipient
header MXHook can trust, because Maildir does not standardize the envelope
recipient. See [Maildir ingestion](maildir.md).

Both paths can run in one process; `all` and `edge` roles accept SMTP, Maildir,
or both, and refuse to start with neither.

### Hosted Cloudflare target

The `worker/` and Cloudflare queue adapter are experimental. They share the
versioned ingress envelope but are not part of the self-hosted production
release: inline Queue payload limits, resource provisioning, tenant isolation,
and D1/R2 contract tests remain hosted-service work. Selecting Cloudflare for
an edge publisher requires explicit credentials and a separately operated
consumer.

The real topology is **fly.io SMTP edge nodes -> Cloudflare Queue -> Worker
consumer**. `worker/src/index.ts` exports exactly two handlers, `fetch` (the
dashboard, admin, and status HTTP surface) and `queue` (the batch consumer for
`mxhook-email-events`). `worker/wrangler.toml` declares a queue consumer, a D1
database, and an R2 bucket.

This is **not** Cloudflare Email Routing. There is no `email()` handler, no
`send_email` binding, and no `EmailMessage` import anywhere in `worker/`;
mail never reaches the Worker from Cloudflare's mail infrastructure. The SMTP
conversation still happens on an edge node you operate, which publishes the
ingress envelope onto the queue.

The Cloudflare queue backend also does not deduplicate by delivery ID, so a
crash-retry re-enqueues the same message and consumers see duplicates. Prefer
the SQLite or Postgres queue whenever consumers cannot dedupe themselves.

## Runtime roles

| Role | Ingress | Processing | API/metrics |
|---|---|---|---|
| `all` | SMTP and/or Maildir | yes | yes unless disabled |
| `edge` | SMTP and/or Maildir | no | disabled |
| `worker` | disabled | yes | yes unless disabled |

`all` is the production default. Split roles require a shared Postgres queue;
embedded SQLite is intended for one process.

## Storage and state

The selected database contains both the ingress queue and operational state.
SQLite enables WAL, foreign keys, a busy timeout, bounded connections, and
owner-only database/WAL/SHM files. Postgres uses the same receipt-fenced queue
and outbox contracts.

Raw messages are stored before parsing under opaque SHA-256-derived names. File
creation is atomic and no-clobber. Directories use mode `0700`; files use
`0600`. Symlinks, traversal, and non-regular Maildir entries are rejected.

Webhook secrets are encrypted with AES-256-GCM using
`MXHOOK_STATE_ENCRYPTION_KEY`. The key is never stored in the database. Losing
it makes route secrets unrecoverable; back it up separately.

## Message contract

`mxhook.message.v1` includes stable message and trace IDs, receipt time, source,
source ID, envelope sender, ordered recipients, raw content hash and size, and
the raw RFC 5322 bytes. All queue adapters validate it before persistence.

## Routing

Rule precedence is priority, then exact address, plus-address normalization,
domain wildcard (`*@example.com`), and global wildcard (`*`). A configured
static webhook route has low priority; API-created rules override it. Routes
snapshot destination and encrypted secret into each delivery row so later
configuration changes cannot mutate an existing delivery.

## Webhook delivery

Requests use HTTPS and include:

- `X-MXHook-Signature: sha256=<HMAC-SHA256 of "{unix_ts}.{payload}">`
- `X-MXHook-Timestamp: <unix seconds>` (signed; bounds the replay window)
- `X-MXHook-Delivery-ID`
- message, trace, and attempt identifiers

Receivers verify with `webhook.VerifyTimestampedSignature`, which rejects
timestamps outside the tolerance window (default five minutes). The legacy
payload-only scheme (no timestamp header) is verifiable only through the
explicit `webhook.VerifySignature` path and carries no replay protection.

Targets with credentials, private/loopback/link-local/multicast addresses, or
non-HTTPS schemes are rejected. DNS is resolved again at dial time. Redirects
are bounded and revalidated. Network failures, `408`, `429`, and `5xx` retry
with durable jittered backoff; other `4xx` responses become terminal.

## Control plane

The HTTP server uses bounded bodies, responses, pagination, and timeouts.
`X-API-Key` protects all control-plane endpoints. Responses carry a generated
`X-Request-ID`; errors use a JSON `{"error":"..."}` envelope. Public endpoints:

| Path | Meaning |
|---|---|
| `/healthz` | process liveness |
| `/readyz` and `/health` | database readiness |
| `/metrics` | Prometheus metrics |

Domains, routes, messages, delivery state, ingress failures, and replay state
are durable. Route secrets are accepted on creation but never returned.
Terminal history is retained; replay schedules a new delivery generation.

See [the OpenAPI contract](../api/openapi.yaml).

## Package map

| Package | Responsibility |
|---|---|
| `smtp/` | restricted-domain SMTP, STARTTLS, durable enqueue |
| `maildir/` | exclusive claims, recovery, quarantine, durable enqueue |
| `queue/sqlite/`, `queue/postgres/` | ingress queue and fenced leases |
| `parser/` | RFC 5322 and MIME parsing |
| `router/` | rule matching |
| `storage/` | hardened raw message storage |
| `internal/state/` | schema, events, encrypted routes, delivery outbox |
| `internal/runtime/` | intake and delivery workers |
| `webhook/` | endpoint validation, signing, HTTP delivery |
| `api/` | persistent control API, health, metrics |
| `cli/` | composition root and operator commands |
