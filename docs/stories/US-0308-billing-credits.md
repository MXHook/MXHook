# User Story: Recharge Account Credits

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to recharge my MXHook credit balance via the SaaS
dashboard so that my email processing doesn't stop mid-month.

---

## Context

Hosted SaaS uses a credit/balance model. Credits consumed per: email received, webhook
delivery, storage used. Builder tops up via Stripe when balance runs low.

---

## Acceptance Criteria

- [ ] `GET /api/account/balance` returns current credit balance + usage breakdown.
- [ ] `POST /api/account/recharge { amount: 1000 }` initiates Stripe payment.
- [ ] Email alert sent when balance drops below configurable threshold (default: 10% remaining).
- [ ] Processing paused (SMTP reject 452) when balance reaches 0.
- [ ] 452 rejection message includes link to recharge page.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
credit consumption events:
  smtp_accepted:   deduct 1 credit per message
  webhook_delivery: deduct 0.1 credit per delivery
  storage_mb:      deduct 0.01 credit per MB/day

GET /api/account/balance:
  return { balance, used_this_period, breakdown: { messages, deliveries, storage } }

POST /api/account/recharge { amount }:
  create Stripe PaymentIntent
  on success: add credits to account
  send receipt email

smtp rcpt handler:
  if account.balance <= 0: reject 452 "insufficient credits"
```

### Key Files/Modules

- `api/server.go`: /account/balance, /account/recharge
- `api/store.go`: credit ledger queries

---

## E2E / Verification Checklist

- [ ] GET /api/account/balance; verify breakdown fields present.
- [ ] Send email; verify credits decremented.
- [ ] Deplete credits to 0; send email; verify 452 rejection.
- [ ] Recharge; verify credits added; send email succeeds.

---

## Related Stories

- [[US-0301]](./US-0301-saas-signup.md) — Sign up for MXHook hosted SaaS
- [[US-0309]](./US-0309-metering.md) — View usage metering
