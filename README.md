# MXHook

Self-hosted inbound email processing: SMTP or Maildir in, signed JSON webhooks out.

[![Release](https://img.shields.io/github/v/release/MXHook/MXHook?include_prereleases&sort=semver)](https://github.com/MXHook/MXHook/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/mxhook/mxhook.svg)](https://pkg.go.dev/github.com/mxhook/mxhook)
[![Build](https://github.com/MXHook/MXHook/actions/workflows/build.yml/badge.svg)](https://github.com/MXHook/MXHook/actions/workflows/build.yml)
[![Lint](https://github.com/MXHook/MXHook/actions/workflows/lint.yml/badge.svg)](https://github.com/MXHook/MXHook/actions/workflows/lint.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/MXHook/MXHook)](go.mod)
[![License: AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue.svg)](LICENSE)

## What this is, and is not

MXHook accepts SMTP or Maildir messages, persists the raw message, parses and
routes it, then delivers a signed JSON webhook with durable retries.

SQLite is the default and needs no external service. Postgres is optional for
multi-process deployments. The hosted Cloudflare implementation is a separate,
experimental target.

MXHook receives mail and never sends it. It accepts SMTP, forwards over HTTPS,
and never connects out to another mail server, generates bounces, or relays.
No SPF or DKIM record is required for MXHook, and adding an MX record leaves a
domain's existing outbound records untouched. See
[Pointing a domain at MXHook](docs/dns-setup.md).

## Install

Linux and macOS. Windows is unsupported (Maildir ingestion is POSIX-only).

### Binary

```bash
go install github.com/mxhook/mxhook@latest
```

### Container

Clone the repository for the Compose files, then fill in the environment
template:

```bash
git clone https://github.com/MXHook/MXHook.git mxhook
cd mxhook
cp .env.example .env
# Set every blank value in .env, including the webhook destination and secrets.
```

## Quick start

### Docker Compose

```bash
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:8080/readyz
```

The base Compose file exposes only the control API on localhost. Add one ingress
overlay:

```bash
# Public SMTP with mandatory STARTTLS
docker compose -f docker-compose.yml -f docker-compose.smtp-tls.yml up -d --build

# Exclusive Maildir ingestion
MAILDIR_PATH="$HOME/Maildir" \
  docker compose -f docker-compose.yml -f docker-compose.maildir.yml up -d --build
```

The SMTP overlay requires certificate and key paths configured in `.env`.
Production MX traffic normally uses host port 25; verify that the host and
provider allow inbound TCP/25 before changing DNS. MXHook listens on `:2525`
inside the container so it can run unprivileged, and the overlay publishes host
port 25 to it. See [Pointing a domain at MXHook](docs/dns-setup.md) for the DNS
records, provider port-25 policies, and a cutover checklist.

Postgres must be selected explicitly:

```bash
MXHOOK_QUEUE_BACKEND=postgres \
MXHOOK_DATABASE_URL='postgres://mxhook:secret@postgres:5432/mxhook?sslmode=disable' \
POSTGRES_PASSWORD=secret \
docker compose --profile postgres up -d --build
```

### Binary

```bash
export MXHOOK_STATE_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export MXHOOK_API_KEY="$(openssl rand -hex 32)"
export MXHOOK_WEBHOOK_URL=https://receiver.example.com/inbound-email
export MXHOOK_WEBHOOK_SECRET="$(openssl rand -hex 32)"
mxhook start --smtp-addr :2525 --recipient-domain example.com
```

The static catch-all route can later be replaced or overridden by API routes.
For a two-stage deployment, first start the `worker` role with ingress disabled,
create a route through the authenticated API, then restart with role `all`.
`all` refuses to open ingress while the durable route set is empty.

`mxhook config validate` checks effective configuration without opening a
listener. Flags override environment variables, which override YAML values;
defaults apply last.

## Features

- SMTP and Maildir ingress; MXHook receives mail and never sends it.
- Durable pipeline: the raw message is persisted, parsed, routed, then delivered.
- SQLite by default; Postgres for multi-process deployments.
- Signed JSON webhooks with durable retries, dead-letter inspection, and replay.
- REST API and CLI for domains, routes, messages, and replays.

### Webhook delivery

Webhook receivers must deduplicate on `X-MXHook-Delivery-ID`. Delivery is
at-least-once, so a response can be repeated after a timeout or crash even when
the receiver handled the first request.

Deliveries are signed Stripe-style: `X-MXHook-Signature` is the HMAC-SHA256 of
`"{unix_ts}.{payload}"` and `X-MXHook-Timestamp` carries the signed timestamp.
Verify with `webhook.VerifyTimestampedSignature`, which rejects timestamps
outside a tolerance window (default five minutes) so captured requests cannot
be replayed. Upgrade receivers to the timestamped verification before
upgrading the sender; the legacy payload-only scheme remains verifiable via
the explicit `webhook.VerifySignature` path for senders that predate
timestamped signing.

### Security defaults

- SMTP refuses startup without at least one accepted recipient domain.
- Public SMTP can require STARTTLS; the production overlay enables it.
- Webhook destinations require HTTPS and public IP addresses. DNS is checked
  again at connection time and after redirects.
- Every configured webhook requires an HMAC secret; route secrets are encrypted
  at rest.
- The container runs as numeric non-root, uses a read-only root filesystem, and
  writes only to `/data` and declared Maildir/TLS mounts.
- Raw email and SQLite files use owner-only permissions.

### Documentation

- [Pointing a domain at MXHook](docs/dns-setup.md)
- [Configuration](docs/configuration.md)
- [Operations and recovery](docs/operations.md)
- [Architecture](docs/architecture.md)
- [CLI reference](docs/cli.md)
- [Maildir ingestion](docs/maildir.md)
- [Durable queue contract](docs/queue.md)
- [Glossary](docs/glossary.md)
- [OpenAPI specification](api/openapi.yaml)

## Development

- I want to USE MXHook → [SKILL.md](SKILL.md)
- I want to MODIFY MXHook → [DEVELOPING.md](DEVELOPING.md)

Changes follow [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

[AGPLv3](LICENSE)
