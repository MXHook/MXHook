# User Story: Configure Wildcard Catch-All Route

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want a catch-all route for my domain so that any email to an
unregistered address is still captured and delivered to a default endpoint.

---

## Context

Without a catch-all, emails to addresses with no matching route are silently dropped
(or rejected). A catch-all ensures nothing is lost while specific routes take priority.

---

## Acceptance Criteria

- [ ] `mxhook route add *@myapp.io --target <url>` registers a domain-level catch-all.
- [ ] Catch-all only fires if no more-specific route matches (lowest priority).
- [ ] Multiple domains can each have their own catch-all.
- [ ] `mxhook route add * --target <url>` registers a global catch-all (all domains).
- [ ] Catch-all match pattern visible in `GET /api/routes` listing.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
route match priority:
  1. exact address (user+tag@domain)
  2. wildcard tag (user+*@domain)
  3. base address (user@domain)
  4. domain wildcard (*@domain)   ← catch-all per domain
  5. global wildcard (*)          ← global catch-all

on no match found → drop message + log "no_route" event
```

### Key Files/Modules

- `router/matcher.go`: priority-ordered matching
- `router/engine.go`: route table management

---

## E2E / Verification Checklist

- [ ] Add catch-all `*@myapp.io`; send to `unknown@myapp.io`; verify delivery to catch-all target.
- [ ] Add specific route for `support@myapp.io`; verify it takes priority over catch-all.
- [ ] Remove all routes; send email; verify `no_route` log event and no delivery attempt.
- [ ] Verify `GET /api/routes` shows catch-all pattern correctly.

---

## Related Stories

- [[US-0108]](./US-0108-plus-addressing.md) — Route plus-addressed email
- [[US-0202]](./US-0202-wildcard-routing.md) — Create wildcard routing rules (platform eng)
