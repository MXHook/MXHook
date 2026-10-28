# User Story: Add a Domain

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to register my domain with MXHook so that inbound emails to
that domain are accepted and processed.

---

## Context

Before routing rules can be created, the domain must be registered. MXHook must reject
SMTP for unregistered domains (anti-relay). Solo dev runs this once per domain.

---

## Acceptance Criteria

- [ ] `mxhook domain add myapp.io` registers the domain via REST API.
- [ ] SMTP server accepts email to `*@myapp.io` after domain registration.
- [ ] SMTP server rejects (550) email to unregistered domains.
- [ ] `mxhook domain add` is idempotent — re-running does not error.
- [ ] `GET /api/domains` lists all registered domains with created-at timestamp.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
cmd domain add <domain>:
  POST /api/domains { domain: <domain> }
  on 201: print "Domain <domain> added"
  on 409: print "Domain <domain> already exists"

smtp rcpt handler:
  extract domain from recipient
  lookup domain in DB
  if not found: reject 550 "relay not permitted"
```

### Key Files/Modules

- `cli/domain.go`: CLI domain commands
- `api/server.go`: POST /domains handler
- `router/domain_config.go`: domain lookup for SMTP validation

---

## E2E / Verification Checklist

- [ ] Run `mxhook domain add myapp.io`; verify 201 response + CLI success message.
- [ ] Send test email to `test@myapp.io`; verify accepted in SMTP log.
- [ ] Send test email to `test@unknown.io`; verify 550 rejection in SMTP log.
- [ ] Run `mxhook domain add myapp.io` again; verify idempotent (no error).

---

## Related Stories

- [[US-0101]](./US-0101-start-server.md) — Start MXHook on a VPS
- [[US-0103]](./US-0103-add-route.md) — Add a webhook route
