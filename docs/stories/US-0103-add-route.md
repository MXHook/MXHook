# User Story: Add a Webhook Route

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to map an email address to a webhook URL so that emails to
that address trigger an HTTP POST to my application.

---

## Context

Core routing configuration. Solo dev maps `support@myapp.io` to their app's webhook
endpoint. Route takes effect immediately; no restart required.

---

## Acceptance Criteria

- [ ] `mxhook route add support@myapp.io --target https://myapp.io/hooks/email` creates route.
- [ ] Route takes effect immediately without server restart.
- [ ] `GET /api/routes` lists all routes with match pattern and target URL.
- [ ] Duplicate route (same match) returns 409 with clear message.
- [ ] Invalid target URL (non-HTTPS in prod mode) rejected with clear error.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
cmd route add <match> --target <url>:
  POST /api/routes { match: <match>, target: <url> }
  on 201: print "Route added: <match> → <url>"
  on 409: print "Route already exists for <match>"

router engine:
  on new route registered via API:
    hot-reload routing table (no restart)
```

### Key Files/Modules

- `cli/route.go`: CLI route commands
- `api/server.go`: POST /routes handler
- `router/engine.go`: routing table, hot-reload

---

## E2E / Verification Checklist

- [ ] Add route; verify in `GET /api/routes`.
- [ ] Send email to `support@myapp.io`; verify webhook POST received at target.
- [ ] Add duplicate route; verify 409.
- [ ] Update target URL; verify new target receives subsequent emails.

---

## Related Stories

- [[US-0102]](./US-0102-add-domain.md) — Add a domain
- [[US-0104]](./US-0104-receive-webhook.md) — Receive first webhook payload
- [[US-0108]](./US-0108-plus-addressing.md) — Route plus-addressed email
