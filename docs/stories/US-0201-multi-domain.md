# User Story: Configure Multiple Domains via API

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to register and manage multiple domains via the REST API
so that different product lines share one MXHook deployment.

---

## Context

Platform team manages email for `support.acme.com`, `billing.acme.com`, `replies.acme.com`.
All share one MXHook instance. Domains provisioned via API from Terraform or CI pipeline.

---

## Acceptance Criteria

- [ ] `POST /api/domains` accepts `{ domain: "support.acme.com" }` and returns 201 + domain record.
- [ ] `GET /api/domains` lists all registered domains with id, domain, created_at.
- [ ] `DELETE /api/domains/:id` removes domain and all associated routes atomically.
- [ ] Deleting a domain with active in-flight messages logs a warning but proceeds.
- [ ] Domain registration is idempotent (POST existing domain → 200, not 409 nor duplicate).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
POST /api/domains:
  validate domain format (RFC 1035)
  upsert domain record
  return 201 (new) or 200 (existing)

DELETE /api/domains/:id:
  begin transaction
    delete all routes for domain
    delete domain record
  commit
  log warning if any in-flight messages exist for domain
```

### Key Files/Modules

- `api/server.go`: domain CRUD handlers
- `api/store.go`: domain DB operations
- `router/domain_config.go`: domain config cache invalidation

---

## E2E / Verification Checklist

- [ ] POST 3 domains; verify GET lists all 3.
- [ ] DELETE one domain; verify its routes also removed; GET confirms.
- [ ] POST same domain twice; verify idempotent (no duplicate in DB).
- [ ] SMTP accepts mail for registered domain; rejects for deleted domain.

---

## Related Stories

- [[US-0202]](./US-0202-wildcard-routing.md) — Create wildcard routing rules
- [[US-0102]](./US-0102-add-domain.md) — Add a domain (solo dev)
