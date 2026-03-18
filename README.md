# MXHook

[![Build](https://github.com/mxhook/mxhook/actions/workflows/build.yml/badge.svg)](https://github.com/mxhook/mxhook/actions/workflows/build.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/mxhook/mxhook)](go.mod)

Open-source inbound email webhook platform. Point your MX records at MXHook, and get structured JSON webhooks when emails arrive.

## How It Works

```
Incoming Email ──▶ SMTP Server ──▶ Parse ──▶ Route ──▶ Webhook POST
                       │                                    │
                       ▼                                    ▼
                  PostgreSQL                         Your Application
               (queue + metadata)
```

1. MXHook runs an SMTP server that accepts inbound email
2. Emails are parsed (headers, body, attachments) using [enmime](https://github.com/jhillyerd/enmime)
3. Messages are routed based on rules you configure (recipient patterns, domains)
4. Structured JSON is delivered to your webhook endpoints with retries

## Quick Start

```bash
docker compose up
```

This starts the SMTP server on port `2525` and PostgreSQL on port `5432`.

### Send a test email

```bash
# Using Python (or any SMTP client)
python3 -c "
import smtplib
from email.mime.text import MIMEText
msg = MIMEText('Hello from MXHook!')
msg['Subject'] = 'Test'
msg['From'] = 'sender@example.com'
msg['To'] = 'test@localhost'
with smtplib.SMTP('localhost', 2525) as s:
    s.sendmail('sender@example.com', ['test@localhost'], msg.as_string())
    print('Sent!')
"
```

### Production deployment

1. Deploy MXHook on your server
2. Add an MX record pointing to your server: `yourdomain.com MX 10 mail.yourdomain.com`
3. Create a domain via the API: `POST /domains`
4. Create a route with your webhook URL: `POST /routes`
5. Emails to `*@yourdomain.com` will be delivered as JSON webhooks

## Configuration

MXHook is configured via environment variables, CLI flags, or a config file (`mxhook.yaml`).

| Environment Variable | Flag | Default | Description |
|---|---|---|---|
| `MXHOOK_SMTP_ADDR` | `--smtp-addr` | `0.0.0.0:25` | SMTP listen address |
| `MXHOOK_SMTP_DOMAIN` | `--smtp-domain` | `localhost` | SMTP server domain |
| `MXHOOK_API_ADDR` | `--api-addr` | `0.0.0.0:8080` | REST API listen address |
| `MXHOOK_POSTGRES_DSN` | `--db-url` | | PostgreSQL connection string |

See [`.env.example`](.env.example) for a complete reference.

## API

MXHook exposes a REST API for managing domains, routes, and messages. Full spec: [`api/openapi.yaml`](api/openapi.yaml).

All API requests require an `X-API-Key` header.

### Key endpoints

- `POST /domains` — Register a domain for inbound email
- `POST /routes` — Create a routing rule with a webhook URL
- `GET /messages` — List processed messages
- `GET /messages/:id` — Message details with delivery status

## Building from Source

```bash
# Prerequisites: Go 1.26+
make build    # builds ./mxhook binary
make test     # runs tests
make lint     # runs linters
make dev      # docker compose up --build
```

## Project Structure

```
cli/        CLI commands (start, domain, route, logs, replay, status)
smtp/       SMTP server implementation
api/        REST API server
parser/     Email parsing (headers, body, attachments)
router/     Message routing logic
queue/      Queue backends (PostgreSQL, Cloudflare)
webhook/    Webhook delivery with retries
storage/    Email storage abstractions
ee/         Enterprise features (BSL 1.1 licensed)
```

## Documentation

- [`docs/load-testing.md`](docs/load-testing.md) — Load test plan and benchmarks
- [`docs/security-review.md`](docs/security-review.md) — Security checklist
- [`api/openapi.yaml`](api/openapi.yaml) — OpenAPI 3.1 spec

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

Apache 2.0 — see [`LICENSE`](LICENSE).

The `ee/` directory is licensed under the [Business Source License 1.1](ee/LICENSE).
