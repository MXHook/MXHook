# User Story: Evaluate MXHook OSS on Staging

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead at a Series A SaaS company, I want to deploy MXHook OSS on our
staging environment within one sprint so that I can validate it replaces our current
Mailgun inbound integration.

---

## Context

Platform team has 1 sprint to prove MXHook works before proposing it to leadership.
Needs: Docker Compose deploy, payload parity check against current Mailgun payloads,
and basic observability confirmation.

---

## Acceptance Criteria

- [ ] `docker compose up` starts full stack with the embedded SQLite queue in < 5 min;
  the optional `postgres` profile provides Postgres when that backend is selected.
- [ ] Existing webhook consumer works with MXHook payload (same field mapping or migration guide).
- [ ] All staging emails delivered within SLA: < 1s p95 webhook latency.
- [ ] DLQ visible and replayable from CLI.
- [ ] No data loss during 48-hour staging soak test.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
staging deploy:
  docker compose up → SMTP :25, API :8080, SQLite volume
  mxhook domain add staging.acme.com
  mxhook route add support@staging.acme.com --target http://staging-app/inbound
  run test email suite
  compare payload fields to Mailgun inbound format
  run 48h soak: send 100 emails/hour, verify 0 dropped
```

### Key Files/Modules

- `docker-compose.yml`: reference staging config
- `docs/`: migration guide from Mailgun/Sendgrid

---

## E2E / Verification Checklist

- [ ] Deploy via docker compose; verify all services healthy at /readyz.
- [ ] Send 100 test emails; verify 100 deliveries in delivery log.
- [ ] Measure p95 webhook latency; verify < 1s.
- [ ] Force 5 DLQ entries; replay all; verify 0 remaining in DLQ.

---

## Related Stories

- [[US-0402]](./US-0402-ee-license.md) — Evaluate Enterprise Edition license
- [[US-0101]](./US-0101-start-server.md) — Start MXHook on a VPS
