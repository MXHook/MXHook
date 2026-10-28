---
name: mxhook
description: |
  Develop MXHook — open-source inbound email webhook platform in Go. SMTP
  ingestion → parse → route → deliver structured JSON webhooks. Covers
  EmailEvent types, router rules (wildcard/*@domain, plus-addressing),
  HMAC-SHA256 signing, DLQ, queue backends (Postgres, Cloudflare), REST API,
  and CLI (cobra/viper).


  Enterprise Features (EE): RBAC (admin/operator/developer roles), 
  Payload Transformations (field mapping, redaction, enrichment).

  Use when: adding routes/domains, extending webhook delivery, debugging
  "recipient not accepted" or SMTP errors, adding queue backends, writing
  parser/router/webhook tests, or working on CLI commands.
---

# MXHook Developer Skill

## Project

Open-source inbound email processing platform.
Emails arrive via SMTP → parsed → routed → delivered as structured JSON webhooks.

**Repo:** `github.com/mxhook/mxhook`
**Language:** Go 1.27
**License:** AGPL-3.0-only (core) / BSL 1.1 (`ee/`)

## Architecture

```
SMTP / Maildir edge → queue.Enqueuer → runtime.Worker (IntakeProcessor: parse,
store raw, match routes) → state delivery outbox → runtime.DeliveryWorker →
transform.Pipeline (EE) → webhook.Deliverer
        ↓                                              ↓
 queue/sqlite (default)                    internal/state (dead deliveries)
 queue/postgres, queue/cloudflare
```

## Package Map

| Package | Responsibility |
|---|---|
| `smtp/` | SMTP server (go-smtp); recipient validation; enqueue raw messages |
| `maildir/` | Maildir ingestion: exclusive claims, recovery, quarantine, enqueue |
| `parser/` | RFC 5322 → `EmailEvent`; `enmime` for MIME/attachments |
| `router/` | Matcher (wildcard + plus-addressing); domain config; engine |
| `webhook/` | Deliverer; HMAC signer; endpoint validation; in-memory DLQ type |
| `queue/` | `Enqueuer`/`Dequeuer` interfaces; sqlite, postgres, cloudflare adapters |
| `storage/` | Raw `.eml` + attachment storage interface |
| `api/` | REST API server (stdlib `net/http`); API-key auth; health; metrics; RBAC (EE) |
| `cli/` | cobra commands: `start`, `domain`, `route`, `logs`, `replay`, `status`, `config` |
| `ee/rbac/` | **EE:** Role-Based Access Control; role-aware API keys |
| `ee/transform/`| **EE:** Payload transformation pipeline (redaction, enrichment) |
| `ee/` | Enterprise Edition: license gating |
| `types/` | `EmailEvent`, `Attachment` structs (public; moved out of `internal/`) |
| `internal/state/` | Schema, events, encrypted routes, delivery outbox, DLQ reads |
| `internal/runtime/` | Intake and delivery workers |
| `internal/metrics/` | Prometheus counters |
| `internal/lifecycle/` | Graceful shutdown helpers |
| `worker/` | **Experimental:** Cloudflare Queue consumer (TypeScript) |

## Core Types

```go
// types/email.go — import "github.com/mxhook/mxhook/types"
const EmailEventSchemaV1 = "mxhook.email.v1"

type EmailEvent struct {
    SchemaVersion    string      // pinned to EmailEventSchemaV1
    ID, TraceID      string
    MessageID        string
    ReceivedAt       time.Time
    EnvelopeFrom     string
    EnvelopeTo       []string
    Headers          map[string][]string
    Subject          string
    TextBody, HTMLBody string
    RawPath          string      // path/key to .eml; not serialized
    Attachments      []Attachment
}

type Attachment struct {
    Filename, ContentType string
    Size        int64
    StoragePath string      // not serialized
}
```

## Router Rules

```go
// router/rule.go
type Rule struct {
    Domain, RecipientPattern string  // "*@domain.com" | "exact@domain.com"
    Target        Target             // "webhook" | "queue" | "storage"
    WebhookURL    string
    WebhookSecret string
    IsCatchAll    bool
    Priority      int
}
```

Matching precedence (router/matcher.go):
1. Exact match: `support@domain.com`
2. Plus-address stripped: `task+label@domain.com` → matches `task@domain.com`
3. Wildcard: `*@domain.com`

## Webhook Delivery

- **Signature:** `X-MXHook-Signature: sha256=<hmac-hex>` over
  `"{unix_ts}.{payload}"`, with `X-MXHook-Timestamp: <unix>` (`webhook/signer.go`)
- **Retry:** durable. `cli/start.go` builds the deliverer with `MaxAttempts: 1`
  and leaves retries to the delivery worker: 8 attempts, exponential backoff
  from `5s` doubling to a `1h` cap, jittered ±20%
  (`internal/runtime/delivery_worker.go`). The in-process `webhook.Deliverer`
  defaults (3 attempts, quadratic 1s/4s/9s) apply only to direct library use.
- **Transform (EE):** Pipeline (field mapping, redaction, enrichment) runs before delivery.
- **DLQ:** dead deliveries and ingress failures live in durable state
  (`internal/state/api_repository.go`); `webhook/dlq.go` is the in-memory
  library type, not what the running server serves
- **Replay:** `POST /replay/{id}` or `mxhook replay`

Verify inbound signature (rejects timestamps outside tolerance; 0 = 5m default):
```go
ok := webhook.VerifyTimestampedSignature(payload, secret,
	r.Header.Get("X-MXHook-Signature"), r.Header.Get("X-MXHook-Timestamp"), 0)
```
Legacy payload-only deliveries (no `X-MXHook-Timestamp`, sender not yet
upgraded) verify only via the explicit legacy path:
```go
ok := webhook.VerifySignature(payload, secret, r.Header.Get("X-MXHook-Signature"))
```

## REST API

Auth: `X-API-Key` header (static key in OSS; role-aware key store in EE).

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/healthz` | — | Process liveness |
| GET | `/readyz`, `/health` | — | Database readiness (`503` when down) |
| GET | `/metrics` | — | Prometheus metrics |
| POST | `/domains` | domains:write | Add domain |
| GET | `/domains` | domains:read | List domains |
| DELETE | `/domains/{id}` | domains:write | Remove domain |
| POST | `/routes` | routes:write | Add route |
| GET | `/routes` | routes:read | List routes |
| DELETE | `/routes/{id}` | routes:write | Remove route |
| GET | `/messages` | logs:read | List messages |
| GET | `/messages/{id}` | logs:read | Get message |
| POST | `/replay/{id}` | webhooks:write | Replay message |
| GET | `/dlq` | logs:read | List DLQ |
| DELETE | `/dlq/{id}` | webhooks:write | Remove DLQ entry |
| GET | `/openapi.yaml` | — | OpenAPI spec |

## CLI Commands

```bash
mxhook start          # ingress, queue consumer, delivery worker, API
mxhook config validate # check effective config without opening a listener
mxhook domain add     # register domain
mxhook route add      # add routing rule
mxhook logs           # tail delivery logs
mxhook replay         # replay failed delivery
mxhook status         # show server status
```

Config loaded from: `mxhook.yaml` (cwd) → `/etc/mxhook/mxhook.yaml` → env (`MXHOOK_*`).

## Build & Test

```bash
# Build (use -buildvcs=false if in symlinked environment)
go build -buildvcs=false ./... 

# Run all tests
go test ./...
```

## Common Pitfalls

- `AllowedDomains` empty = `smtp.NewServer` refuses construction; at least one
  accepted recipient domain is required (`--recipient-domain`, repeatable)
- Plus-addressing stripped **before** rule matching — `task+foo@d.com` matches `task@d.com`
- Webhook secret empty = signature header still sent but verification skipped by receiver
- `GET /dlq` reads durable state when the server has `WithRepository` (what
  `start` wires); the `WithDLQ` in-memory fallback applies only without it, and
  a nil DLQ then returns an empty list
- EE features gated by `ee/license.go` — check license before extending EE packages
