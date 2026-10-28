# User Story: Add a New Routing Rule Type

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to add a content-body routing rule type so that messages
can be routed based on keywords in the email body, not just the recipient address.

---

## Context

Advanced routing (EE). Contributor adds a new rule type to the router engine. Must
integrate with priority-based matching and be gated behind EE feature flag.

---

## Acceptance Criteria

- [ ] New rule type: `BODY_CONTAINS` with `keyword` field.
- [ ] Evaluated after recipient-based rules in EE mode only.
- [ ] Route config accepts: `match_body: { contains: "invoice" }`.
- [ ] Existing routing tests still pass (no regression).
- [ ] New rule type covered by unit + integration tests.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
Rule interface:
  Matches(email EmailEvent) bool

BodyContainsRule struct { keyword string }
  Matches(email):
    return strings.Contains(strings.ToLower(email.Text), strings.ToLower(keyword))
    OR strings.Contains(strings.ToLower(email.HTML_stripped), ...)

routing eval (EE):
  for route in sorted_routes:
    if route.recipient_matches(email) AND route.body_rule.Matches(email):
      return route
```

### Key Files/Modules

- `router/rule.go`: Rule interface + new BodyContainsRule type
- `router/engine.go`: evaluation order update
- `router/advanced.go`: EE rule types
- `router/advanced_test.go`: test coverage

---

## E2E / Verification Checklist

- [ ] Add route with `match_body: { contains: "invoice" }`; send email with "invoice" in body; verify routed.
- [ ] Send email without keyword; verify falls through to next matching route.
- [ ] Existing wildcard + exact match tests still pass.
- [ ] Feature returns 402 in OSS mode.

---

## Related Stories

- [[US-0408]](./US-0408-advanced-routing.md) — Header-based advanced routing (business)
- [[US-0503]](./US-0503-new-queue-adapter.md) — Implement a new queue adapter
