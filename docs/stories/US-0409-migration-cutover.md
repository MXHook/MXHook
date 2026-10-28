# User Story: Migrate Production Traffic from Existing Vendor

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead, I want a safe cutover path from our current inbound email vendor to
MXHook in production so that we migrate with zero customer-visible downtime or message loss.

---

## Context

Company currently uses Mailgun inbound. Cutover requires: parallel-run period (both
receive), then MX record flip to MXHook-only. Must be reversible within minutes.

---

## Acceptance Criteria

- [ ] Parallel-run mode: MXHook accepts email + delivers webhooks while Mailgun MX still active.
- [ ] Cutover: MX record updated to MXHook; Mailgun disabled. Rollback: revert MX.
- [ ] Cutover playbook documented in `docs/migration/`.
- [ ] No message loss during MX propagation window (TTL-based).
- [ ] Post-cutover: delivery success rate ≥ 99.9% within 1 hour of flip.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
cutover phases:
  1. parallel run (1–2 weeks):
     both MX records active → dual delivery
     compare payload parity between Mailgun + MXHook
     validate DLQ depth stays near 0

  2. cutover (TTL window):
     lower MX TTL to 5 min (do 24h before)
     update MX to MXHook-only
     monitor: smtp_accepted_total rising, delivery_success_rate ≥ 0.999

  3. rollback (if needed):
     revert MX record → Mailgun
     MXHook DLQ accumulates → replay after re-flip
```

### Key Files/Modules

- `docs/migration/mailgun.md`: cutover playbook (to be created)
- `api/server.go`: readyz + metrics for cutover monitoring

---

## E2E / Verification Checklist

- [ ] Deploy parallel run; verify both Mailgun + MXHook receive same test email.
- [ ] Flip MX to MXHook-only; verify smtp_accepted_total climbs; no drops.
- [ ] Check delivery_success_rate via /analytics/summary; verify ≥ 99.9%.
- [ ] Practice rollback in staging; verify MX revert takes effect in < 10 min.

---

## Related Stories

- [[US-0406]](./US-0406-payload-parity.md) — Validate payload parity
- [[US-0407]](./US-0407-zero-loss-guarantee.md) — Zero message loss guarantee
