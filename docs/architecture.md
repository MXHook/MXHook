# Architecture — MXHook

Author: $USER
Date: 2026-03-25

---

## Overview

Open-source inbound email webhook platform.
Receives SMTP email → parses → routes → delivers structured JSON webhooks.

---

## Stack

| Layer | Technology |
|---|---|
| Language | Go 1.26 (single binary, vendored deps) |
| SMTP | `github.com/emersion/go-smtp` |
| Email parsing | `github.com/jhillyerd/enmime` |
| CLI | cobra + viper |
| HTTP API | stdlib `net/http` (Go 1.22+ pattern routing) |
| Queue (default) | Postgres (`queue/postgres`) |
| Queue (SaaS) | Cloudflare Queue (`queue/cloudflare`) |
| Metrics | Prometheus (`internal/metrics`) |
| Deployment | Docker multi-stage (golang:1.26-alpine → alpine:3.23) |

---

## Pipeline

```
SMTP connection
  → session.Data() [smtp/server.go]
  → queue.Enqueuer.Enqueue()
  → parser.Worker [parser/worker.go]
  → types.EmailEvent [internal/types/email.go]
  → router.Engine [router/engine.go]
  → webhook.Deliverer [webhook/delivery.go]
  → POST endpoint with X-MXHook-Signature (HMAC-SHA256)
```

Failed deliveries land in `webhook.DLQ`.

---

## Package Map

| Package | Responsibility |
|---|---|
| `smtp/` | SMTP server; recipient validation; enqueue raw messages |
| `parser/` | RFC 5322 → `EmailEvent`; worker loop; MIME/attachment parsing |
| `router/` | Matcher (wildcard + plus-addressing); domain config; engine |
| `webhook/` | Deliverer (retry + backoff); HMAC signer; DLQ; replay |
| `queue/` | `Enqueuer`/`Dequeuer` interfaces; postgres + cloudflare adapters |
| `storage/` | Raw `.eml` + attachment storage interface |
| `api/` | REST API server; API-key auth middleware |
| `cli/` | Commands: `start`, `domain`, `route`, `logs`, `replay`, `status` |
| `ee/` | Enterprise Edition license gating |
| `internal/types/` | `EmailEvent`, `Attachment` structs |
| `internal/metrics/` | Prometheus counters |
| `internal/lifecycle/` | Graceful shutdown helpers |

---

## Core Types

See `internal/types/email.go`:

```
EmailEvent
  ID, MessageID    string
  ReceivedAt       time.Time
  EnvelopeFrom     string
  EnvelopeTo       []string
  Headers          map[string][]string
  Subject          string
  TextBody         string
  HTMLBody         string
  RawPath          string       // path/key to .eml in storage
  Attachments      []Attachment

Attachment
  Filename, ContentType  string
  Size                   int64
  StoragePath            string
```

---

## Routing

Rules defined in `router/rule.go`. Matching in `router/matcher.go`.

### Match precedence (all after plus-address strip)

1. Exact: `support@domain.com`
2. Plus-stripped: `task+label@domain.com` → matches rule for `task@domain.com`
3. Wildcard: `*@domain.com`
4. Catch-all flag (`IsCatchAll: true`)

### Targets

- `webhook` — HTTP POST to `WebhookURL`
- `queue` — forward to queue backend
- `storage` — store only, no delivery

---

## Webhook Delivery

- **Signature header:** `X-MXHook-Signature: sha256=<hmac-hex>`
- **Algorithm:** HMAC-SHA256 (`webhook/signer.go`)
- **Retry:** 3 attempts, quadratic backoff (1s, 4s, 9s)
- **DLQ:** `webhook/dlq.go` — inspect via `GET /dlq`, replay via `POST /replay/{id}`

Receiver verification (pseudocode):

```
ok = VerifySignature(requestBody, sharedSecret, header["X-MXHook-Signature"])
```

See `webhook.VerifySignature()` for constant-time comparison implementation.

---

## REST API

Auth: `X-API-Key` header. Set via `MXHOOK_API_KEY` env or `api.key` config.
OpenAPI spec: `GET /openapi.yaml` (embedded at build time).

| Method | Path | Auth |
|---|---|---|
| GET | `/health` | — |
| POST | `/domains` | yes |
| GET | `/domains` | yes |
| DELETE | `/domains/{id}` | yes |
| POST | `/routes` | yes |
| GET | `/routes` | yes |
| DELETE | `/routes/{id}` | yes |
| GET | `/messages` | yes |
| GET | `/messages/{id}` | yes |
| POST | `/replay/{id}` | yes |
| GET | `/dlq` | yes |
| DELETE | `/dlq/{id}` | yes |

---

## Configuration

Config resolution order: `mxhook.yaml` (cwd) → `/etc/mxhook/mxhook.yaml` → `MXHOOK_*` env vars.

Key SMTP config fields:

```
Addr            string    # ":25" or ":2525"
Domain          string    # MX hostname
AllowedDomains  []string  # accepted recipient domains; empty = accept all
MaxMessageBytes int64     # default 25 MB
TLSCertFile, TLSKeyFile string
```

---

## Queue Backends

| Backend | Import path | Notes |
|---|---|---|
| Postgres | `queue/postgres` | Default; durable; retry-friendly |
| Cloudflare | `queue/cloudflare` | Used in hosted SaaS |
| Noop | `queue.NoopEnqueuer` | Tests / dry-run |

---

## Known Pitfalls

- `AllowedDomains` empty → open relay; restrict in production
- Plus-addressing stripped **before** rule matching — design intent, not a bug
- Webhook secret optional; signature still sent even if empty
- DLQ must be wired via `api.WithDLQ(dlq)` — nil DLQ returns empty list silently
- EE packages gated by `ee/license.go`; check license before extending EE
- No auth on SMTP inbound — correct for a public MX server

---

## Build & Test

```bash
# Build
docker build -t mxhook .

# Test per-package (no justfile present)
go test ./smtp/...
go test ./parser/...
go test ./router/...
go test ./webhook/...
go test ./api/...
```

---

## Log States

```
SMTP accepted → Parsed → Queued → Delivered
                                → Failed → Retried → DLQ
```

---

# 2026-04-26 update — SaaS deployment stack

> **Scope.** The body above documents the self-hosted Go
> binary architecture. This section adds the SaaS deployment
> stack (Cloudflare Workers, D1, R2, Fly.io edge, abuse
> mitigation, admin panel) and the commercialisation gap
> against the 90-day plan.

## Cross-references

- [hop](https://github.com/hop-top/hop/blob/main/docs/architecture.md) — orchestrator (deployment seam)
- [kit](https://github.com/hop-top/kit/blob/main/docs/architecture.md) — adoption tracked as `mxhook/mxhook#T-0058`

## SaaS architecture

```
                 ┌──────────────────────────────────┐
                 │  Inbound mail (MX → Fly.io edge) │
                 └─────────────────┬────────────────┘
                                   │
                                   ▼ Cloudflare Queue publish
                       ┌───────────────────────┐
                       │   Cloudflare Worker   │
                       │  (queue consumer)     │
                       └─────────┬─────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
      ┌──────────────┐    ┌──────────────┐   ┌───────────────┐
      │  D1 (tenant │    │  R2 (.eml +  │   │  Webhook      │
      │  + delivery │    │  attachments)│   │  delivery     │
      │  logs)      │    │              │   │  (HMAC signed)│
      └──────────────┘    └──────────────┘   └───────────────┘
```

| Component | Tech | Role |
|---|---|---|
| SMTP edge | Fly.io node | Inbound mail relay |
| Async queue | Cloudflare Queue | Replaces Postgres queue in SaaS |
| Control plane | Cloudflare Worker (`worker/src/`) | Parse, route, deliver per tenant |
| Tenant + billing store | Cloudflare D1 (`worker/src/db.ts`) | Accounts, routes, delivery logs, usage metering |
| Object storage | Cloudflare R2 | Raw `.eml` + attachments |
| Reverse proxy / TLS | Cloudflare | Default platform |

## SaaS dashboards (TS, in `worker/src/`)

| Surface | Auth | Purpose |
|---|---|---|
| Customer dashboard | Session + cookie | Tenant: domains, routes, messages, billing |
| Admin panel | Admin API key | Ops: tenant mgmt, abuse logs, system health |
| Status page | — | Public uptime + incidents |

Implemented under tasks T-0046 (dashboard), T-0047 (admin
panel), T-0048 (abuse mitigation), T-0049 (status page).

## Commercialisation gap (vs 90-day plan)

The 90-day plan (Month 1, May 2026) calls for:

- [ ] Stripe integration live — **not coded**
- [ ] Pricing page published — **not live** (README has tiers Free / $9 / $49 / Contact)
- [ ] SaaS signup flow — **partial** (dashboard exists; account creation pipeline unclear)
- [ ] Usage metering + billing — **not built** (no Stripe webhook receiver; no meter event collection)
- [ ] HN / r/selfhosted / dev.to launch — **not posted**

Self-hosted is fully functional; SaaS dashboard + admin panels
live; **revenue loop is incomplete**.

## kit adoption

Task: `mxhook/mxhook#T-0058` (filed 2026-04-26). UI components
not yet integrated. Worker dashboard uses vanilla TS / HTML;
opportunity to unify with kit on adoption.

## Recent activity

- `9514aa0` add human + agent cheatsheets
- `ab5a0ba` rename mxhook.io → mxhook.dev
- `e1269a0` Go 1.24 → 1.26
- `e66b330` MIT + BSL 1.1 licenses
- `490ca9d` comprehensive test coverage
- `986c8d9` load tests + security review + launch checklist (T-0050)
- `92b1e0a` worker status page (T-0049)
- `9c87870` worker abuse mitigation (T-0048)
- `7bbda69` worker admin panel (T-0047)
- `9ebc322` worker dashboard API + UI (T-0046)
- `4b273be` wire parser / router / delivery into queue consumer
- `5ddfd88` D1 schema + helpers
- `86b657a` scaffold Worker + queue + D1 + R2 bindings
- `28d11a5` Fly.io SMTP edge + CF Queue publisher
- `0d1dc4f` EE advanced rule engine (regex + header matching)
- `bed4142` API: domain / route / message handlers
- `7e242dc` API: OpenAPI 3.1 spec + GET /openapi.yaml endpoint
- `fe5b1ce` EE license gate (`Gate()` and `Require()`)

## Open questions

1. **Stripe webhook consumer.** `POST /webhooks/stripe` lives in self-hosted API or Worker?
2. **Metering collection.** Worker middleware or dashboard API?
3. **Signup flow.** Worker (`worker/src/`) or external (Auth0 / Clerk)?
4. **Kit integration.** Stripe checkout UI uses kit components or external Stripe Checkout?
5. **Self-hosted billing.** Stripe usable in self-hosted or only SaaS?
6. **DLQ in SaaS.** D1 table or separate? `worker/src/dlq.ts` not found.
7. **Email forwarding.** Routes can forward to Gmail / other MX?
8. **Rate limiting.** Worker abuse (`worker/src/abuse.ts`) thresholds not in config — exposed to tenants?
