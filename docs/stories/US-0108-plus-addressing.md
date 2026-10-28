# User Story: Route Plus-Addressed Email

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to route emails sent to `support+billing@myapp.io` and
`support+tech@myapp.io` to different webhook endpoints so that I can namespace workflows
without registering multiple email addresses.

---

## Context

Plus-addressing (subaddressing per RFC 5321) lets a single base address fan out to
multiple routes based on the tag. Critical for multi-workflow automation without extra
domains or aliases.

---

## Acceptance Criteria

- [ ] Route with match `support+billing@myapp.io` receives only emails to that exact tag.
- [ ] Route with match `support+*@myapp.io` (wildcard tag) receives all plus-tagged variants.
- [ ] Route with match `support@myapp.io` receives emails without a plus tag.
- [ ] Plus-tag is preserved in `envelope.to` in the webhook payload.
- [ ] Catch-all route `*@myapp.io` receives any unmatched address including plus-tagged.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
route matching priority (most-specific first):
  1. exact match:    support+billing@myapp.io
  2. wildcard tag:   support+*@myapp.io
  3. base address:   support@myapp.io
  4. domain wildcard: *@myapp.io
  5. catch-all:      *

plus-address parsing:
  "support+billing@myapp.io" → local="support", tag="billing", domain="myapp.io"
```

### Key Files/Modules

- `router/matcher.go`: plus-address parsing and priority matching
- `router/rule.go`: rule type definitions (exact, wildcard-tag, base, domain-wildcard)

---

## E2E / Verification Checklist

- [ ] Add routes for `support+billing@myapp.io` and `support+tech@myapp.io`.
- [ ] Send to each; verify each webhook endpoint receives correct message only.
- [ ] Add wildcard `support+*@myapp.io`; send to `support+new@myapp.io`; verify matched.
- [ ] Verify `envelope.to` in payload retains the full plus-addressed recipient.

---

## Related Stories

- [[US-0103]](./US-0103-add-route.md) — Add a webhook route
- [[US-0109]](./US-0109-wildcard-route.md) — Configure wildcard catch-all route
- [[US-0303]](./US-0303-plus-route.md) — Route plus-addressed email (automation builder)
