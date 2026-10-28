# MXHook CLI reference

## Configuration

All commands accept `--config PATH`. Without it, MXHook reads `mxhook.yaml`
from the current directory or `/etc/mxhook/`. Resolution order is flags,
`MXHOOK_` environment variables, YAML, then defaults. Dotted YAML keys become
uppercase underscore environment variables; `state.encryption_key` becomes
`MXHOOK_STATE_ENCRYPTION_KEY`.

```bash
mxhook config validate
mxhook config validate --json
```

Validation is read-only and redacts secrets. A non-zero exit means the process
would refuse startup.

## `start`

Starts ingress, the durable queue consumer, parser, route matcher, webhook
outbox worker, control API, health endpoints, and Prometheus metrics according
to `--runtime-role`.

Roles:

| Role | Components |
|---|---|
| `all` | SMTP and/or Maildir, queue consumer, delivery worker, API |
| `edge` | SMTP and/or Maildir publisher only; set `--api-addr ""` |
| `worker` | queue consumer, delivery worker, API; ingress must be disabled |

Core flags:

| Flag | Default | Purpose |
|---|---:|---|
| `--runtime-role` | `all` | `all`, `edge`, or `worker` |
| `--queue-backend` | `sqlite` | `sqlite`, `postgres`, or `cloudflare` |
| `--db-url` | `./data/mxhook.db` | SQLite file or Postgres DSN |
| `--raw-dir` | `./data/raw` | Durable raw RFC 5322 storage |
| `--api-addr` | `0.0.0.0:8080` | Empty disables HTTP API |
| `--api-key` | empty | Required when API is enabled |
| `--webhook-url` | empty | Optional static catch-all destination |
| `--webhook-secret` | empty | Required when static URL is set |
| `--state-encryption-key` | empty | Required base64 32-byte key |
| `--migrate` | `true` | Initialize/validate supported schema |
| `--shutdown-grace-period` | `30s` | In-flight shutdown deadline |

SMTP flags:

| Flag | Default |
|---|---:|
| `--smtp-addr` | `0.0.0.0:25` |
| `--smtp-domain` | `localhost` |
| `--recipient-domain` | none; repeatable and required when SMTP is enabled |
| `--smtp-max-message-bytes` | `10485760` |
| `--smtp-max-recipients` | `50` |
| `--smtp-max-connections` | `1000` |
| `--smtp-read-timeout` | `60s` |
| `--smtp-write-timeout` | `60s` |
| `--smtp-tls-cert` | empty |
| `--smtp-tls-key` | empty |
| `--smtp-require-tls` | `false` |

Maildir flags:

| Flag | Default |
|---|---:|
| `--maildir` | disabled |
| `--maildir-poll-interval` | `2s` |
| `--maildir-max-message-bytes` | `10485760` |
| `--maildir-recipient-header` | `Delivered-To` |
| `--maildir-default-recipient` | empty |
| `--maildir-id-namespace` | canonical Maildir path |
| `--maildir-adopt-existing` | `false` |

Worker/delivery flags:

| Flag | Default |
|---|---:|
| `--worker-max-attempts` | `5` |
| `--worker-lease-extension` | `30s` |
| `--webhook-timeout` | `10s` |
| `--delivery-max-attempts` | `8` |
| `--delivery-base-retry` | `5s` |
| `--delivery-max-retry` | `1h` |

Examples:

```bash
mxhook start --smtp-addr :2525 --recipient-domain example.com
mxhook start --smtp-addr "" --maildir "$HOME/Maildir"
mxhook start --queue-backend postgres \
  --db-url 'postgres://user:pass@host/mxhook?sslmode=require'
```

## Control commands

Control commands use `--api-url` (default `http://localhost:8080`) and
`--api-key`. Transport errors and non-success API responses return non-zero.

```bash
mxhook status
mxhook domain add example.com --api-key "$MXHOOK_API_KEY"
mxhook route add --recipient 'support@example.com' \
  --webhook https://receiver.example.com/email \
  --secret "$ROUTE_WEBHOOK_SECRET" \
  --api-key "$MXHOOK_API_KEY"
mxhook logs --message-id MESSAGE_ID --api-key "$MXHOOK_API_KEY"
mxhook replay MESSAGE_ID --api-key "$MXHOOK_API_KEY"
```

`domain add` registers a domain for ingestion; SMTP accepts mail for it without
a restart. Domains configured statically through `--recipient-domain` /
`MXHOOK_SMTP_ALLOWED_DOMAINS` remain accepted, so the two sources are unioned.
A split `edge`/`worker` deployment picks the change up within a few seconds;
`all` applies it on the next recipient check. At least one domain must still be
configured statically for the server to start.

`status` calls readiness. `logs` returns persisted message and delivery state.
`replay` schedules a dead delivery through the durable outbox; it never posts
directly from the CLI.

Replay always targets the delivery's stored route endpoint. An earlier
override-URL replay was removed deliberately: it posted unsigned payloads to
an arbitrary URL, bypassing endpoint validation and the SSRF policy. To
redirect deliveries, update or recreate the route through the authenticated
API — route changes pass full endpoint validation — and then replay.
