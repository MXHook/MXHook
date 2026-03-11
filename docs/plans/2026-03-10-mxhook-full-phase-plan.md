# MXHook — Full Phase Plan
*2026-03-10*

## Strategy

Dual-track: open-source core that powers a hosted SaaS. OSS builds trust and adoption; SaaS generates revenue. Same binary, EE features gated by license key.

Hosting: Cloudflare-hybrid. SMTP edge nodes (VMs) for raw TCP ingestion → Cloudflare Queues → Workers for all downstream processing.

Billing: Hybrid — base plan tiers (Free / Pro / Enterprise) + usage overages (emails, webhooks, storage).

---

## Phase 1 — SMTP Ingestion Core

**Goal:** Reliable SMTP receiver that accepts email and enqueues it.

- Go binary with `go-smtp` listener
- TLS support (STARTTLS + direct TLS)
- Recipient validation (domain + address allowlist)
- Size limits and rate limiting per connection
- Raw `.eml` written to filesystem or S3-compatible store
- Queue interface with Postgres adapter (default), Redis and NATS adapters
- Message enqueued with envelope metadata (from, to, received timestamp, message-id)
- Structured JSON logging
- Docker Compose: smtp + postgres + optional redis/nats
- `mxhook start` CLI command

**Done when:** Email received end-to-end, stored raw, confirmed in queue.

---

## Phase 2 — Parser Service

**Goal:** Convert raw `.eml` into structured `EmailEvent`.

- Worker pool pulls from queue
- `enmime` / `go-message` for full MIME parsing
- Extract: envelope from/to, headers, subject, text body, HTML body, attachments, message-id, thread headers
- Attachments stored to filesystem or S3-compatible (Minio default)
- `EmailEvent` metadata written to Postgres
- Parse errors go to dead-letter queue with reason

---

## Phase 3 — Event Router

**Goal:** Route parsed events to configured targets based on rules.

- Rule engine: recipient exact match, wildcard, plus-addressing, header conditions
- Multi-domain support: per-domain routing config
- Rules stored in Postgres, hot-reloaded
- Catch-all routes per domain
- Route targets: webhook, queue (NATS/Redis), storage-only

---

## Phase 4 — Webhook Delivery

**Goal:** Reliable, signed webhook delivery.

- HMAC-SHA256 signed payloads
- Exponential backoff retry (configurable max attempts)
- Per-webhook timeout enforcement
- Dead-letter queue for permanently failed deliveries
- Delivery log per attempt (status code, latency, truncated response body)
- `POST /replay/:message_id` for re-delivery

**Done when:** Full happy path working — email arrives → parsed → routed → webhook delivered with signature + retry loop.

---

## Phase 5 — Observability

**Goal:** Every email traceable end-to-end.

- Structured log pipeline states: `SMTP accepted → Queued → Parsed → Routed → Delivered / Failed / Retried`
- Message lifecycle table in Postgres (one row per state transition)
- Prometheus metrics: ingestion rate, parse errors, delivery success rate, retry counts, latency histograms
- Dead-letter queue browser (CLI + API)
- Grafana dashboard JSON shipped in repo

---

## Phase 6 — CLI

**Goal:** First-class operator experience.

```
mxhook start          # start all services
mxhook domain add     # register a domain
mxhook route add      # configure routing rule
mxhook logs           # tail message lifecycle log
mxhook replay <id>    # re-deliver a message
mxhook status         # health of all components
```

- Config via YAML file + env var overrides
- `cobra` for CLI structure, `viper` for config

---

## Phase 7 — REST API

**Goal:** Programmatic management.

```
POST   /domains
DELETE /domains/:id
POST   /routes
GET    /messages
GET    /messages/:id
GET    /deliveries
POST   /replay/:id
GET    /health
```

- JSON REST, OpenAPI spec generated from code
- API key auth (`X-API-Key` header)

**Done when:** Operators can fully manage MXHook via CLI and API with full message visibility.

---

## Phase 8 — Enterprise Edition (EE)

**Goal:** Features that justify commercial licensing for large orgs.

### Routing & Transformations
- Advanced rule engine: regex, header matching, content-based routing, script hooks (embedded JS or Lua)
- Payload transformations: field mapping, redaction, enrichment before delivery

### RBAC
- Roles: admin / operator / developer
- API key scoped permissions

### Analytics
- Email volume over time (per domain, per route)
- Processing latency percentiles
- Delivery success rate trends
- Attachment statistics
- Domain traffic breakdown

### Monitoring
- Pre-built Grafana dashboards
- Configurable alert thresholds
- SLA metrics: p50/p95/p99 ingestion-to-delivery latency

### Compliance
- Audit log: config change history
- DKIM/SPF result ingestion (parse headers, store, expose in event)

### Licensing
- EE features gated behind license key check (open core model)

---

## Open-Source Release Prep

- README, CONTRIBUTING, LICENSE (Apache 2.0)
- Docker Compose quickstart (one command to working demo)
- Helm chart for Kubernetes deployment
- Single binary build (all services, selectable via flags)
- GitHub Actions: test, lint, release binary + Docker image
- Docs site (markdown, GitHub Pages)

**Done when:** OSS v1.0 published. EE builds on same binary with license key activation.

---

## Phase 9 — SaaS Infrastructure (Cloudflare-hybrid)

**Architecture:**

```
Internet SMTP
      │
SMTP Edge Nodes (VMs — raw TCP)
      │
Cloudflare Queues
      │
Cloudflare Workers (parser, router, delivery)
      │
├── D1       (metadata, routing config, delivery logs)
├── R2       (raw .eml, attachments)
└── Workers  (webhook delivery, retry logic)
```

SMTP requires raw TCP — Cloudflare Workers cannot handle it. SMTP edge nodes (e.g. fly.io VMs or Cloudflare Metal) handle ingestion and forward to Cloudflare Queues. All downstream processing runs on Workers.

---

## Phase 10 — Multi-Tenancy

- Tenant isolation: domain → tenant mapping in D1
- Per-tenant: routing config, webhook credentials, R2 storage namespace
- API keys scoped to tenant

---

## Phase 11 — Accounts & Billing

### Auth
- User accounts via Cloudflare Access or Better Auth

### Domain Onboarding
- Domain registration flow
- DNS ownership verification via TXT record

### Billing (Hybrid)
- Base plan tiers: Free / Pro / Enterprise (feature + limit gates)
- Usage overages: emails received, webhook deliveries, storage GB
- Credits system for prepaid usage
- Stripe integration: subscriptions + usage metering
- Usage metering via Cloudflare Workers Analytics Engine

---

## Phase 12 — SaaS Operations

- Customer dashboard: message logs, delivery status, domain management, billing
- Admin panel: tenant management, usage overview, support tools
- Abuse/spam mitigation hooks
- Status page

**Done when:** Public SaaS live — customers sign up, add a domain, receive emails, get webhooks, get billed.

---

## Package Structure

```
mxhook/
 ├── smtp/        # SMTP receiver
 ├── parser/      # MIME parsing, EmailEvent
 ├── router/      # routing rules engine
 ├── queue/       # queue interface + adapters (postgres, redis, nats)
 ├── storage/     # raw eml + attachment storage (fs, s3, minio, r2)
 ├── webhook/     # delivery, retry, signing
 ├── api/         # REST API
 ├── cli/         # cobra CLI
 ├── config/      # viper config
 ├── ee/          # enterprise edition (license-gated)
 └── internal/    # shared types, EmailEvent, lifecycle
```

---

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Language | Go |
| SMTP | `go-smtp` |
| Parsing | `enmime`, `go-message` |
| CLI | `cobra`, `viper` |
| Database | Postgres (self-hosted) / D1 (SaaS) |
| Queue | Interface: Postgres / Redis / NATS adapters |
| Object storage | Filesystem / Minio / S3 / R2 |
| SaaS processing | Cloudflare Workers |
| SaaS queuing | Cloudflare Queues |
| Billing | Stripe |
| Metrics | Prometheus + Grafana |

---

## Phase Summary

| Phase | Deliverable |
|-------|-------------|
| 1 | SMTP ingestion + queue |
| 2 | Parser service + EmailEvent |
| 3 | Event router + rules engine |
| 4 | Webhook delivery + retry + DLQ |
| 5 | Observability + Prometheus + Grafana |
| 6 | CLI (cobra/viper) |
| 7 | REST API + OpenAPI spec |
| 8 | Enterprise Edition (EE) + OSS v1.0 release |
| 9 | Cloudflare SaaS infrastructure |
| 10 | Multi-tenancy |
| 11 | Accounts + hybrid billing (Stripe) |
| 12 | Customer dashboard + admin panel + SaaS launch |
