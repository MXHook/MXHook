# User Story: Create Wildcard Routing Rules

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to define wildcard routing rules so that any email address
under a domain is routed to the correct service without enumerating every address.

---

## Context

Platform team handles multi-tenant product where each customer gets `<tenant>@tickets.acme.com`.
A single wildcard rule routes all tenant inboxes to the ticketing service, with per-tenant
context extracted from the envelope.

---

## Acceptance Criteria

- [ ] Route pattern `*@tickets.acme.com` matches any local-part for that domain.
- [ ] Route pattern `task+*@acme.com` matches any plus-tagged variant of `task`.
- [ ] Wildcard routes have lower priority than exact-match routes on same domain.
- [ ] Envelope recipient (including wildcard-matched address) preserved in payload.
- [ ] `GET /api/routes` returns wildcard patterns correctly serialized.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
route types:
  EXACT      = "user@domain.com"
  PLUS_EXACT = "user+tag@domain.com"
  PLUS_WILD  = "user+*@domain.com"
  DOMAIN_WC  = "*@domain.com"
  GLOBAL_WC  = "*"

match(recipient, routes):
  for priority in [EXACT, PLUS_EXACT, PLUS_WILD, DOMAIN_WC, GLOBAL_WC]:
    if route = find(routes, type=priority, recipient):
      return route
  return nil
```

### Key Files/Modules

- `router/matcher.go`: multi-tier matching
- `router/rule.go`: route type enum + pattern parsing

---

## E2E / Verification Checklist

- [ ] Add `*@tickets.acme.com`; send to `tenant1@tickets.acme.com`; verify matched.
- [ ] Add exact `support@tickets.acme.com`; verify it takes priority over wildcard.
- [ ] Verify envelope.to in payload contains original addressed recipient.
- [ ] Add 100 wildcard routes; verify matching performance < 1ms per lookup.

---

## Related Stories

- [[US-0201]](./US-0201-multi-domain.md) — Configure multiple domains
- [[US-0109]](./US-0109-wildcard-route.md) — Wildcard catch-all (solo dev)
