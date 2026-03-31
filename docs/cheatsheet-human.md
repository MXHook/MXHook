# MXHook Cheatsheet — Human

Quick reference for daily ops. Scannable in 30 seconds.

---

## Start

```bash
mxhook start                                     # SMTP :25, API :8080 (defaults)
mxhook start --smtp-addr 0.0.0.0:2525 \
             --smtp-domain mail.example.com \
             --api-addr 0.0.0.0:8080 \
             --db-url postgres://...
curl http://localhost:8080/health                 # → {"status":"ok"}
```

Config file: `mxhook.yaml` (CWD) or `/etc/mxhook/mxhook.yaml`
Env prefix: `MXHOOK_` (e.g. `MXHOOK_SMTP_ADDR`)

### Cloudflare Queue (SaaS mode)

```bash
export CF_QUEUE_ACCOUNT_ID=<id>
export CF_QUEUE_ID=<id>
export CF_QUEUE_TOKEN=<token>
mxhook start     # uses CF Queue; falls back to noop if vars unset
```

---

## Domains

```bash
mxhook domain add example.com                    # register domain for ingestion
mxhook domain add example.com --api-url http://localhost:8080 --api-key <key>

# REST equivalents
curl -X POST http://localhost:8080/domains \
  -H "X-API-Key: <key>" \
  -H "Content-Type: application/json" \
  -d '{"name":"example.com"}'
curl http://localhost:8080/domains -H "X-API-Key: <key>"
curl -X DELETE http://localhost:8080/domains/<id> -H "X-API-Key: <key>"
```

---

## Routes

Route = recipient pattern → webhook URL mapping.

```bash
mxhook route add --recipient "*@example.com" --webhook https://myapp.com/hook
mxhook route add --recipient "support@example.com" --webhook https://myapp.com/support

# REST equivalents
curl -X POST http://localhost:8080/routes \
  -H "X-API-Key: <key>" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","recipient_pattern":"*@example.com",
       "webhook_url":"https://myapp.com/hook","webhook_secret":"opt-hmac-secret"}'
curl http://localhost:8080/routes -H "X-API-Key: <key>"
curl -X DELETE http://localhost:8080/routes/<id> -H "X-API-Key: <key>"
```

### Recipient pattern types

| Pattern | Matches |
|---------|---------|
| `user@example.com` | Exact address only |
| `*@example.com` | All addresses on domain |
| `user@example.com` | Plus-stripped (`user+tag@example.com` → `user@example.com`) |

---

## Messages

```bash
mxhook logs                                      # list recent messages
mxhook logs --api-key <key> --message-id <id>    # filter by ID

# REST
curl http://localhost:8080/messages -H "X-API-Key: <key>"
curl "http://localhost:8080/messages?limit=20&offset=0" -H "X-API-Key: <key>"
curl http://localhost:8080/messages/<id> -H "X-API-Key: <key>"
```

### Message states

`accepted` → `queued` → `parsed` → `routed` → `delivered` | `failed` | `retried` | `dlq`

---

## Replay

Re-deliver a failed or previously processed message.

```bash
mxhook replay <message-id>
mxhook replay <message-id> --api-key <key>

# REST
curl -X POST http://localhost:8080/replay/<id> -H "X-API-Key: <key>"
```

---

## Dead-Letter Queue (DLQ)

```bash
# REST (no CLI command yet)
curl http://localhost:8080/dlq -H "X-API-Key: <key>"           # list failures
curl -X DELETE http://localhost:8080/dlq/<id> -H "X-API-Key: <key>"  # remove entry
```

---

## Status

```bash
mxhook status                                    # probe API health
mxhook status --api-url http://localhost:8080    # custom URL
curl http://localhost:8080/health                # → {"status":"ok"} (no auth)
```

---

## Config Reference

```yaml
# mxhook.yaml
smtp:
  addr: "0.0.0.0:25"
  domain: "mail.example.com"

api:
  addr: "0.0.0.0:8080"
  key: "your-api-key"

database:
  url: "postgres://user:pass@host/db"
```

---

## Common Tips & Failure Modes

| Symptom | Fix |
|---------|-----|
| SMTP not receiving | Confirm MX record points to server; check port 25 not blocked |
| Webhook not delivered | Check DLQ: `GET /dlq`; verify webhook URL is reachable |
| Auth 401 | Pass `X-API-Key` header or `--api-key` flag |
| Messages missing | Check `mxhook status`; verify `mxhook start` is running |
| CF Queue not used | Confirm all 3 `CF_QUEUE_*` env vars are set |
| Replay returns 404 | Message ID not found; check `GET /messages` for valid IDs |
