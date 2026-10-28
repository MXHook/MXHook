# MXHook Cheatsheet — Human

Quick reference for daily ops. Scannable in 30 seconds. Unfamiliar term? See the
[glossary](glossary.md).

---

## Start

```bash
export MXHOOK_STATE_ENCRYPTION_KEY="$(openssl rand -base64 32)"   # required
export MXHOOK_API_KEY="$(openssl rand -hex 32)"                   # required with API

mxhook start --recipient-domain example.com      # SMTP :25 → SQLite → webhook
mxhook start --smtp-addr 0.0.0.0:2525 \
             --smtp-domain mail.example.com \
             --recipient-domain example.com \
             --db-url ./data/mxhook.db
mxhook start --smtp-addr "" --maildir "$HOME/Maildir"
```

Config file: `mxhook.yaml` (CWD) or `/etc/mxhook/mxhook.yaml`
Env prefix: `MXHOOK_` (e.g. `MXHOOK_SMTP_ADDR`)

`start` runs the whole pipeline: SMTP and/or Maildir ingress, durable enqueue,
queue consumer, parser, route matcher, webhook outbox worker, control API,
health endpoints, and Prometheus metrics. `--runtime-role` splits it — `all`
(default) runs everything, `edge` runs ingress only (set `--api-addr ""`), and
`worker` runs the consumer, delivery worker, and API with ingress disabled. See
[the CLI reference](cli.md) for the full flag set.

`--recipient-domain` is required when SMTP is enabled; startup fails without at
least one. `all` also refuses to start until at least one route exists, durable
or static.

### Cloudflare Queue (SaaS mode)

```bash
export CF_QUEUE_ACCOUNT_ID=<id>
export CF_QUEUE_ID=<id>
export CF_QUEUE_TOKEN=<token>
MXHOOK_QUEUE_BACKEND=cloudflare mxhook start
```

All three credentials are required. Missing credentials fail startup; messages are
never acknowledged into a no-op queue.

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

A domain added here is accepted by SMTP without a restart. Domains from
`MXHOOK_SMTP_ALLOWED_DOMAINS` / `--recipient-domain` stay accepted as well;
the two sources are unioned. In a split `edge`/`worker` deployment the change
reaches the SMTP process within a few seconds rather than instantly.

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

Derived from the message's delivery rows, not stored:

`received` (nothing routed) · `pending` (a delivery is still in flight) ·
`processed` (a delivery succeeded) · `dead` (all deliveries ended, none
succeeded)

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
curl -X DELETE "http://localhost:8080/dlq/ingress:<message-id>" \
  -H "X-API-Key: <key>"                                        # clear an ingress failure
```

Dead deliveries are retained and cannot be deleted — `DELETE` on one returns
`405`. Retry with `mxhook replay <message-id>` once the receiver is fixed.

---

## Status

```bash
mxhook status                                    # probe API health
mxhook status --api-url http://localhost:8080    # custom URL
curl http://localhost:8080/healthz               # liveness  → {"status":"ok"}
curl http://localhost:8080/readyz                # readiness → 503 if DB is down
curl http://localhost:8080/metrics               # Prometheus metrics
```

`/healthz`, `/readyz`, `/health`, `/metrics`, and `/openapi.yaml` need no
`X-API-Key`; every other endpoint does.

Point a container liveness probe at `/healthz`, never at a readiness endpoint.
`/readyz` (and its alias `/health`) pings the database and returns `503` when
that dependency is down, so a liveness probe aimed there restarts a healthy
process on every database blip. See
[operations](operations.md#health-and-monitoring).

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
  url: "./data/mxhook.db"

queue:
  backend: "sqlite"
```

Postgres remains available with `queue.backend: postgres` and a Postgres connection
string in `database.url`.

---

## Dev Workflow

```bash
make help          # every target; also the default goal
make check         # fmt-check, vet, test, race — run before pushing
make check-all     # the above plus worker-check and schema-check
make test-postgres # Postgres-gated tests; refuses without TEST_DATABASE_URL
make schema-check  # apply worker migrations to scratch SQLite, list tables
```

Toolchain versions are pinned in `mise.toml` to match CI. Set up locally with
`mise trust && mise install`, or open the repository in the devcontainer, which
installs the toolchain and reuses the Compose `postgres` service so
`TEST_DATABASE_URL` is preset and the Postgres-gated tests run instead of
skipping. See [DEVELOPING](../DEVELOPING.md).

---

## Common Tips & Failure Modes

| Symptom | Fix |
|---------|-----|
| SMTP not receiving | Confirm MX record points to server; check port 25 not blocked — see [DNS setup](dns-setup.md) |
| Webhook not delivered | Check DLQ: `GET /dlq`; verify webhook URL is reachable |
| Auth 401 | Pass `X-API-Key` header or `--api-key` flag |
| Messages missing | Check `mxhook status`; verify `mxhook start` is running |
| CF Queue not used | Confirm all 3 `CF_QUEUE_*` env vars are set |
| Replay returns 404 | Message ID not found; check `GET /messages` for valid IDs |
