# User Story: Apply Header-Based Advanced Routing Rules

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Engineer, I want to route emails based on header conditions (not just
recipient address) so that I can implement priority routing for VIP customers.

---

## Context

EE advanced routing feature. VIP customer emails have `X-Customer-Tier: vip` header.
Platform team wants these routed to a high-priority webhook endpoint regardless of
recipient address.

---

## Acceptance Criteria

- [ ] Route can include header condition: `match_header: { name: "X-Customer-Tier", value: "vip" }`.
- [ ] Header-based routes evaluated after recipient match; can override lower-priority routes.
- [ ] Multiple header conditions combinable with AND logic.
- [ ] Header condition on route visible in `GET /api/routes/:id`.
- [ ] EE-only feature; header routing returns 402 in OSS mode.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
Route {
  match:          string   // address pattern
  match_headers:  [{ name, value }]  // EE only
  target:         string
  priority:       int
}

routing evaluation (EE):
  candidates = routes matching recipient
  for candidate in candidates sorted by priority desc:
    if all match_headers satisfied:
      return candidate
  return nil

header condition:
  email.headers[name] contains value (case-insensitive)
```

### Key Files/Modules

- `router/advanced.go`: header-based routing (EE)
- `router/advanced_test.go`: advanced routing tests
- `ee/license.go`: EE feature gate

---

## E2E / Verification Checklist

- [ ] Create header-based route for `X-Customer-Tier: vip`; send email with header; verify routed to VIP endpoint.
- [ ] Send same address without header; verify normal route used.
- [ ] Verify endpoint returns 402 in OSS mode.
- [ ] Test AND condition: two headers both required; verify only both-match triggers route.

---

## Related Stories

- [[US-0202]](./US-0202-wildcard-routing.md) — Create wildcard routing rules
- [[US-0402]](./US-0402-ee-license.md) — Evaluate EE license
