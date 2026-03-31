# MXHook Cheatsheet — Agent

Quick reference for autonomous agents, scripts, and LLMs consuming the API.
Scannable in 30 seconds.

---

## Prerequisites

```bash
mxhook start --api-addr 0.0.0.0:8080   # REST :8080; keep running
curl http://localhost:8080/health        # → {"status":"ok"}

# Auth: all endpoints (except /health, /openapi.yaml) require API key
export MXHOOK_API_KEY=<key>
curl -H "X-API-Key: $MXHOOK_API_KEY" http://localhost:8080/domains
```

OpenAPI spec: `GET /openapi.yaml` (unauthenticated)

---

## Agent Loop Contract

```
1. Configure  →  POST /domains + POST /routes
2. Ingest     →  email arrives via SMTP; auto-processed
3. Observe    →  GET /messages (poll state field)
4. Inspect    →  GET /messages/{id}
5. Remediate  →  GET /dlq → POST /replay/{id} → DELETE /dlq/{id}
```

**DO:** poll `GET /messages/{id}` and gate on `state==delivered` before assuming success.
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

```
accepted → queued → parsed → routed → delivered
                                    ↘ failed → retried → dlq
```

### Query patterns

```bash
# Recent messages
curl "http://localhost:8080/messages?limit=20" -H "X-API-Key: $KEY"

# Poll until delivered
until curl -s http://localhost:8080/messages/$MSG_ID \
  -H "X-API-Key: $KEY" | grep -q '"delivered"'; do sleep 2; done
```

---

## Replay

```bash
# Re-deliver a message
curl -X POST http://localhost:8080/replay/<message_id> -H "X-API-Key: $KEY"
# → 202 {"status":"queued"} | 404 not found
```

---

## Dead-Letter Queue

```bash
# List failures
curl http://localhost:8080/dlq -H "X-API-Key: $KEY"
# → [{message_id, webhook_url, failed_at, reason}, ...]

# Remove entry after manual remediation
curl -X DELETE http://localhost:8080/dlq/<id> -H "X-API-Key: $KEY"
# → 204 | 404 | 503 (DLQ backend unavailable)
```

---

## Webhook Delivery

Webhooks delivered as `POST` to configured URL with:

| Header | Value |
|--------|-------|
| `Content-Type` | `application/json` |
| `X-MXHook-Signature` | `sha256=<hmac>` (if `webhook_secret` set) |

Signature: `HMAC-SHA256(webhook_secret, request_body)` — verify on receipt.

### EmailEvent payload fields

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | stable UUID |
| `message_id` | string | RFC 5322 Message-ID |
| `received_at` | ISO 8601 | SMTP receipt time |
| `envelope_from` | string | SMTP MAIL FROM |
| `envelope_to` | []string | SMTP RCPT TO |
| `headers` | map[string][]string | all MIME headers |
| `subject` | string | decoded subject |
| `text_body` | string | plain-text part |
| `html_body` | string | HTML part |
| `attachments` | []Attachment | filename, content_type, size, data |

---

## Error Handling

| Condition | Handling |
|-----------|----------|
| 401 Unauthorized | Set `X-API-Key` header; verify key matches config |
| 404 on message | ID may not exist yet — poll with backoff |
| 503 on DLQ delete | DLQ backend not configured; check server config |
| Webhook not delivered | Check `GET /dlq` for `reason`; fix endpoint; replay |
| `state==failed` persists | Inspect DLQ; `POST /replay/{id}` once fixed |
| Routes not matching | Verify `recipient_pattern` + `domain` match envelope `To:` |
