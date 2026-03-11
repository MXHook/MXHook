# MXHook

Open-source inbound email webhook platform. Receive emails on your own infrastructure;
get structured JSON webhooks.

## Self-hosted

```
docker compose up
```

Configure MX records to point to your instance. Emails arrive → parsed → routed → webhook.

## Hosted SaaS

**Sign up:** https://mxhook.dev/signup *(coming soon)*

### SaaS Tiers

| Plan | Messages/mo | Domains | Price |
|---|---|---|---|
| Free | 1,000 | 1 | $0 |
| Starter | 10,000 | 5 | $9/mo |
| Pro | 100,000 | 20 | $49/mo |
| Enterprise | Unlimited | Unlimited | Contact us |

Features: dashboard, delivery logs, domain verification, API keys, abuse protection.

## Quick Start (self-hosted)

1. Deploy: `docker compose up -d`
2. Add MX record pointing to your server
3. Create a domain: `POST /domains`
4. Create a route: `POST /routes` with `webhook_url`
5. Send an email → receive webhook

## Documentation

- `docs/load-testing.md` — load test plan
- `docs/security-review.md` — security checklist
- `docs/launch-checklist.md` — go-live checklist
- `api/openapi.yaml` — REST API spec

## Architecture

```
SMTP edge (fly.io) → Cloudflare Queue → Worker (parse/route/deliver) → Your webhook
                                              ↓
                                         D1 (metadata, logs)
                                         R2 (raw email storage)
```

## Status

https://mxhook.dev/status.html

## License

Apache 2.0 — see `LICENSE`.
