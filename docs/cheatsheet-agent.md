# MXHook Cheatsheet — Agent

Quick reference for autonomous agents, scripts, and LLMs consuming the API.
Scannable in 30 seconds.

---

## Prerequisites

```bash
export MXHOOK_STATE_ENCRYPTION_KEY="$(openssl rand -base64 32)"   # required
export MXHOOK_API_KEY="$(openssl rand -hex 32)"                   # required with API

mxhook start --recipient-domain example.com --db-url ./data/mxhook.db
mxhook start --smtp-addr :2525 --recipient-domain example.com --db-url ./data/mxhook.db
mxhook start --smtp-addr "" --maildir "$HOME/Maildir"
```

`start` runs the whole pipeline: SMTP and/or Maildir ingress, durable enqueue,
intake worker, parser, route matcher, webhook delivery worker, retention
janitor, control API, health endpoints, and Prometheus metrics. The API below
is a runnable agent loop, not a target contract. SQLite is the default;
Postgres and Cloudflare Queue are explicit alternatives.

`--recipient-domain` is required when SMTP is enabled, and the default `all`
role refuses to start until at least one route exists, durable or static.
`--runtime-role` splits the process — `all` (default) runs everything, `edge`
runs ingress only (set `--api-addr ""`), `worker` runs the consumer, delivery
worker, and API with ingress disabled.

```bash
export MXHOOK_API_KEY=<key>
curl -H "X-API-Key: $MXHOOK_API_KEY" http://localhost:8080/domains
```

Auth: `/healthz`, `/readyz`, `/health`, `/metrics`, and `/openapi.yaml` need no
`X-API-Key`; every other endpoint requires it. With no API key configured the
control plane fails closed and rejects every caller.

`/healthz` is liveness. `/health` and `/readyz` are readiness — both ping the
database and return `503` when it is down. Never point a liveness probe at a
readiness endpoint.

OpenAPI spec: `GET /openapi.yaml` (unauthenticated)

## Local Commands

```bash
make help        # machine-readable list of every target; the default goal
make check       # fmt-check, vet, test, race
make check-all   # the above plus worker typecheck/lint and schema apply
```

Parse `make help` to discover available commands rather than guessing; targets
are self-documenting and the list is authoritative.

---

## Agent Loop Contract

```
1. Configure  →  POST /domains + POST /routes
2. Ingest     →  email arrives via SMTP; auto-processed
3. Observe    →  GET /messages (poll state field)
4. Inspect    →  GET /messages/{id}
5. Remediate  →  GET /dlq → POST /replay/{id} → DELETE /dlq/{id}
```

**DO:** poll `GET /messages/{id}` and gate on `state=="processed"` before assuming success.
**DON'T:** replay immediately after failure — check DLQ reason first.

---

## Domains

```bash
# Create
curl -X POST http://localhost:8080/domains \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"name":"example.com"}'
# → {"id":"<id>","name":"example.com","created_at":"..."}

# List
curl http://localhost:8080/domains -H "X-API-Key: $KEY"

# Delete
curl -X DELETE http://localhost:8080/domains/<id> -H "X-API-Key: $KEY"
# → 204 No Content
```

A domain registered here is accepted by SMTP without a restart: recipient
acceptance consults the durable registry, cached for 5s. Domains from
`--recipient-domain` / `MXHOOK_SMTP_ALLOWED_DOMAINS` stay accepted too — the
two sources are unioned. In a split `edge`/`worker` deployment the change
reaches the SMTP process within the cache TTL rather than instantly.

---

## Routes

```bash
# Create — recipient patterns: exact, wildcard (*@domain.com), plus-stripped
curl -X POST http://localhost:8080/routes \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"domain":"example.com","recipient_pattern":"*@example.com",
       "webhook_url":"https://myapp.com/hook","webhook_secret":"opt-hmac"}'
# → {"id":"<id>","domain":"...","recipient_pattern":"...","webhook_url":"...","created_at":"..."}

# List
curl http://localhost:8080/routes -H "X-API-Key: $KEY"

# Delete
curl -X DELETE http://localhost:8080/routes/<id> -H "X-API-Key: $KEY"
```

---

## Messages

```bash
# List (paginated)
GET /messages?limit=50&offset=0

# Get single
GET /messages/{id}
# → {id, message_id, from, to[], subject, received_at, state}
```

### Message State Machine

`state` is derived from the message's delivery rows, not stored:

| `state` | Meaning |
|---------|---------|
| `received` | No deliveries exist — nothing was routed |
| `pending` | At least one delivery is still pending, processing, or retrying |
| `processed` | At least one delivery succeeded |
| `dead` | All deliveries ended terminally and none succeeded |

### Query patterns

```bash
# Recent messages
curl "http://localhost:8080/messages?limit=20" -H "X-API-Key: $KEY"

# Poll until processed
until curl -s http://localhost:8080/messages/$MSG_ID \
  -H "X-API-Key: $KEY" | grep -q '"processed"'; do sleep 2; done
```

---

## Replay

```bash
# Re-deliver a message: resets its dead deliveries to pending
curl -X POST http://localhost:8080/replay/<message_id> -H "X-API-Key: $KEY"
# → 202 {"status":"queued","deliveries":<n>} | 404 not found
```

---

## Dead-Letter Queue

```bash
# List failures — dead deliveries plus ingress failures, newest first
curl "http://localhost:8080/dlq?limit=50&offset=0" -H "X-API-Key: $KEY"
# → [{id, message_id, route_id, webhook_url, attempt_count,
#     replay_generation, reason, failed_at}, ...]

# Remove an ingress failure (its id is prefixed "ingress:")
curl -X DELETE "http://localhost:8080/dlq/ingress:<message_id>" -H "X-API-Key: $KEY"
# → 204 | 404
```

Dead *deliveries* are retained and cannot be deleted: `DELETE /dlq/{id}` on a
non-`ingress:` id returns `405` with
`{"error":"dead deliveries are retained; schedule a replay instead"}`. Use
`POST /replay/{message_id}` to retry one.

---

## Webhook Delivery

Webhooks delivered as `POST` to configured URL with:

| Header | Value |
|--------|-------|
| `Content-Type` | `application/json` |
| `X-MXHook-Signature` | `sha256=<hmac>` (if `webhook_secret` set) |
| `X-MXHook-Timestamp` | Unix seconds at signing time |

Signature: `HMAC-SHA256(webhook_secret, "{X-MXHook-Timestamp}.{request_body}")`
— verify with `webhook.VerifyTimestampedSignature(body, secret, sig, ts,
tolerance)`, which enforces the replay window (default 5m). Legacy
payload-only deliveries (no timestamp header, pre-timestamp senders) verify
only via the explicit `webhook.VerifySignature(body, secret, sig)` path.

### EmailEvent payload fields

| Field | Type | Notes |
|-------|------|-------|
| `schema_version` | string | `mxhook.email.v1`; switch on it, alert on unknown values |
| `id` | string | stable UUID |
| `trace_id` | string | correlation ID across the pipeline |
| `message_id` | string | RFC 5322 Message-ID; omitted when empty |
| `received_at` | ISO 8601 | SMTP receipt time |
| `envelope_from` | string | SMTP MAIL FROM |
| `envelope_to` | []string | SMTP RCPT TO |
| `headers` | map[string][]string | all MIME headers |
| `subject` | string | decoded subject |
| `text_body` | string | plain-text part; omitted when empty |
| `html_body` | string | HTML part; omitted when empty |
| `attachments` | []Attachment | `filename`, `content_type`, `size` only |

Attachments carry metadata, not bytes: there is no `data` field, and the
server-side `RawPath`/`StoragePath` locations are deliberately never
serialized.

### Delivery retries

The runtime delivery worker makes up to 8 attempts (`--delivery-max-attempts`),
starting at 5s (`--delivery-base-retry`) and doubling to a 1h ceiling
(`--delivery-max-retry`), with ±20% jitter on each delay. A delivery goes
`dead` once attempts are exhausted or the error is not retryable. This is the
runtime policy — not the webhook library's own single-attempt configuration.

---

## Error Handling

| Condition | Handling |
|-----------|----------|
| 401 Unauthorized | Set `X-API-Key` header; verify key matches config |
| 403 Forbidden | Key authenticated but lacks the permission for that route |
| 404 on message | ID may not exist yet — poll with backoff |
| 405 on DLQ delete | Dead deliveries are retained; `POST /replay/{id}` instead |
| 409 Conflict | Duplicate domain or route; list first, reuse the existing id |
| Webhook not delivered | Check `GET /dlq` for `reason`; fix endpoint; replay |
| `state=="dead"` persists | Inspect DLQ; `POST /replay/{id}` once fixed |
| `state=="received"` persists | No route matched — check `GET /routes` and the domain |
| Routes not matching | Verify `recipient_pattern` + `domain` match envelope `To:` |
