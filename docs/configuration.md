# Production configuration

## Required values

For the default `all` role:

| Environment variable | Requirement |
|---|---|
| `MXHOOK_SMTP_ALLOWED_DOMAINS` | Domain accepted by SMTP; comma- or space-separated, or a YAML list, for multiple domains |
| `MXHOOK_STATE_ENCRYPTION_KEY` | Base64 encoding of exactly 32 random bytes |
| `MXHOOK_API_KEY` | Random control-plane credential |

Generate independent state, API, and per-route webhook values:

```bash
openssl rand -hex 32
openssl rand -base64 32
openssl rand -hex 32
```

Never reuse the API key, webhook secret, or state encryption key. Store the
state key in a secret manager and back it up independently of the database.

### Passing secrets

`--api-key`, `--webhook-secret`, and `--state-encryption-key` exist as flags,
but a value passed on the command line is visible to every local user through
the process table (`ps`) and is recorded in shell history. Prefer the
environment or a config file rendered by the deployment system:

```text
MXHOOK_API_KEY, MXHOOK_WEBHOOK_SECRET, MXHOOK_STATE_ENCRYPTION_KEY
```

Reserve the flags for local development and throwaway values. Anything that
protects production data should reach the process through the environment,
with the config file readable only by the service account.

`MXHOOK_WEBHOOK_URL` and `MXHOOK_WEBHOOK_SECRET` are a static catch-all route
and must be configured together. A fresh `all` role needs this route. It may be
omitted only when durable API routes already exist, or while running a zero-
ingress `worker` role to bootstrap those routes. `all` refuses startup when the
effective route set is empty, so it cannot accept and strand mail.

## YAML example

```yaml
runtime:
  role: all
  shutdown_grace_period: 30s
queue:
  backend: sqlite
database:
  url: /data/mxhook.db
  migrate: true
storage:
  raw_dir: /data/raw
smtp:
  addr: :2525
  domain: mx.example.com
  allowed_domains: [example.com]
  max_message_bytes: 10485760
  max_recipients: 50
  max_connections: 1000
  tls_cert: /run/tls/fullchain.pem
  tls_key: /run/tls/privkey.pem
  require_tls: true
api:
  addr: 0.0.0.0:8080
webhook:
  timeout: 10s
retention:
  ttl: 720h
  interval: 1h
```

Supply `MXHOOK_API_KEY` and `MXHOOK_STATE_ENCRYPTION_KEY` through the environment
or secret files rendered by the deployment system. If using the optional static
route, supply its URL and `MXHOOK_WEBHOOK_SECRET` there too. Do not put secrets
in source-controlled YAML.

## SQLite and Postgres

SQLite is the default:

```text
MXHOOK_QUEUE_BACKEND=sqlite
MXHOOK_DATABASE_URL=/data/mxhook.db
```

Use one MXHook process per SQLite database and place `/data` on durable local
storage. Do not use NFS or a shared network filesystem for SQLite.

Postgres is optional:

```text
MXHOOK_QUEUE_BACKEND=postgres
MXHOOK_DATABASE_URL=postgres://mxhook:password@db.example/mxhook?sslmode=verify-full
```

Postgres enables split `edge` and `worker` processes. The edge role must set
`MXHOOK_API_ADDR=` and does not need state/webhook/API secrets. A worker role
must set both ingress addresses empty:

```text
MXHOOK_RUNTIME_ROLE=worker
MXHOOK_SMTP_ADDR=
MXHOOK_MAILDIR_PATH=
```

Two-stage bootstrap:

1. Start `worker` with SMTP and Maildir empty.
2. Create at least one route through the authenticated API.
3. Change the role to `all`, enable SMTP and/or Maildir, and restart.
4. Enable provider forwarding only after readiness is green.

## Retention

Without retention, processed state grows forever. `retention.ttl`
(`MXHOOK_RETENTION_TTL`, default `0` = disabled) enables a janitor on worker
and `all` roles that deletes terminal rows older than the TTL: succeeded
deliveries and their attempts, events no delivery references any more together
with their raw `.eml` files, and ingress-failure records.
`retention.interval` (`MXHOOK_RETENTION_INTERVAL`, default `1h`) sets the
sweep cadence.

Dead-letter deliveries and the events and raw files they reference are always
kept so `/dlq` listing and replay keep working; replaying or clearing dead
deliveries makes their events eligible on a later sweep. Successful queue
acknowledgements delete the queue row immediately regardless of this setting.
Choose a TTL no shorter than your operational replay window.

## API authentication

The control plane fails closed. With no `api.key` configured and no enterprise
authorizer attached, every control route answers `401`; only `/healthz`,
`/readyz`, `/health`, `/metrics`, and `/openapi.yaml` stay reachable so
orchestrator probes and scrapes keep working. Configure `MXHOOK_API_KEY`
before expecting `/messages`, `/domains`, `/routes`, or `/dlq` to respond.

A caller that presents no credential receives `401`; one that authenticates but
lacks the permission for a route receives `403`.

Error bodies are deliberately terse. Validation and conflict responses name the
offending field, `500` responses carry no internal detail, and a `404` for an
unknown message echoes the identifier from the request path — treat API logs
and responses as carrying caller-supplied text, and avoid rendering them
unescaped in an operator dashboard.

## TLS and network exposure

For public SMTP, set both certificate paths and enable SMTP TLS requirement.
MXHook supports STARTTLS, not implicit TLS. Restrict the API to localhost or a
private network; it has API-key authentication but no built-in TLS terminator.
Terminate API TLS at a trusted reverse proxy when remote administration is
required.

### Getting a certificate for your MX hostname

The certificate must cover the **MX hostname** — the name your MX record points
to, and the name set as `smtp.domain` (`MXHOOK_SMTP_DOMAIN`) — not the
recipient domain in `smtp.allowed_domains`. With an MX record pointing
`example.com` at `mx.example.com`, the certificate's CN/SAN must be
`mx.example.com`. A certificate issued for `example.com` will not match the
name senders connect to. The two are configured independently, so it is easy
to certify the wrong one:

```yaml
smtp:
  domain: mx.example.com        # certificate must cover this name
  allowed_domains: [example.com] # recipient domains; not certified
```

Issue the certificate with certbot on the host the MX record resolves to:

```bash
certbot certonly --standalone -d mx.example.com
```

`--standalone` runs a temporary listener on **port 80** for the HTTP-01
challenge, so port 80 must be free and reachable from the internet for the
duration of the run. Stop anything already bound to it first, or use
`--webroot -w /var/www/html` to serve the challenge from an existing web
server instead. Port 80 is required only for issuance and renewal, not for
SMTP.

Point the SMTP TLS keys at the issued files:

```text
MXHOOK_SMTP_TLS_CERT=/etc/letsencrypt/live/mx.example.com/fullchain.pem
MXHOOK_SMTP_TLS_KEY=/etc/letsencrypt/live/mx.example.com/privkey.pem
```

The equivalents are `smtp.tls_cert` / `smtp.tls_key` in YAML and
`--smtp-tls-cert` / `--smtp-tls-key` on the command line. Both must be set
together; setting only one fails startup. The service account must be able to
read `privkey.pem`.

#### Renewal

**MXHook reloads rotated certificates without a restart.** The SMTP server
resolves its certificate per handshake and reloads the pair when either file's
modification time changes, so a renewal that replaces the files on disk is
picked up by the next connection. A deploy hook therefore does not need to
restart or signal MXHook:

```bash
certbot renew --deploy-hook "systemctl reload nginx"
```

If nothing else consumes the certificate, no deploy hook is needed at all —
`certbot renew` alone is sufficient for MXHook. Renewal still needs port 80
free for the HTTP-01 challenge, on the same terms as issuance.

Two details worth knowing. Certbot replaces the `live/` symlink targets rather
than editing files in place, and MXHook stats the configured paths on each
handshake, so the rotation is seen through the symlink. And if a reload fails —
for example a handshake landing between the certificate and key being
written — MXHook keeps serving the previously loaded pair rather than failing
the connection, and picks up the new pair on a later handshake.

In containers, mount the certificate directory so renewals on the host are
visible inside the container. The `docker-compose.smtp-tls.yml` overlay binds
the host paths given in `MXHOOK_SMTP_CERT_FILE` and `MXHOOK_SMTP_KEY_FILE`
into the container; those two variables are host-side volume sources for
Compose, distinct from the `MXHOOK_SMTP_TLS_CERT` / `MXHOOK_SMTP_TLS_KEY`
values MXHook itself reads.

MXHook offers STARTTLS on the SMTP port, not implicit TLS on port 465, so no
separate submission listener is involved. See `docs/dns-setup.md` for the MX
record itself.

Webhook targets must resolve only to public HTTPS addresses. Prefer a public
authenticated gateway in front of private services; the opt-outs below exist
for self-hosted deployments that cannot provide one.

### Dangerous webhook policy opt-ins

Two keys weaken the built-in SSRF protection. Both default to `false`; leave
them unset unless the deployment genuinely requires them.

| Key | Environment variable | Effect when `true` |
|---|---|---|
| `webhook.allow_http` | `MXHOOK_WEBHOOK_ALLOW_HTTP` | Permits plain `http://` webhook targets. Payloads and signatures travel unencrypted. |
| `webhook.allow_private_network` | `MXHOOK_WEBHOOK_ALLOW_PRIVATE_NETWORK` | Permits targets resolving to private, loopback, and link-local addresses. Disables the SSRF guard. |

```yaml
webhook:
  timeout: 10s
  # DANGEROUS: only for receivers on a trusted internal network,
  # e.g. another container on the same Docker Compose network.
  allow_http: true
  allow_private_network: true
```

Enable them only when the receiver lives on a network segment you fully
control (for example the Compose quickstart delivering to a sibling container
on `172.16.0.0/12`). Never enable either on an instance that accepts
attacker-influenced route configuration.

## Validation

Run before every rollout:

```bash
mxhook --config /etc/mxhook/mxhook.yaml config validate
mxhook --config /etc/mxhook/mxhook.yaml config validate --json
```

Startup also validates configuration, database/schema access, raw storage,
Maildir ownership, SMTP TLS material, and webhook DNS before opening listeners.
