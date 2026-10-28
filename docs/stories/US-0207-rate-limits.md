# User Story: Apply Per-Domain Rate Limits

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to configure per-domain rate limits on inbound SMTP
so that a single noisy domain cannot starve other tenants' message processing.

---

## Context

Multi-domain deployment. One domain receives a spam burst (10k emails/min). Without
rate limiting, it saturates the queue and delays delivery for all other domains.

---

## Acceptance Criteria

- [ ] Domain config accepts `max_messages_per_minute` field.
- [ ] SMTP server enforces limit: excess messages rejected with 421 (try again later).
- [ ] Rate limit configurable via API: `PATCH /api/domains/:id { rate_limit: 100 }`.
- [ ] Rate limit state tracked in-memory (per node) or in Redis for multi-node.
- [ ] `mxhook logs` shows `rate_limited` event with domain + count when triggered.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
smtp rcpt/data handler:
  domain = extract_domain(recipient)
  cfg = load_domain_config(domain)
  if cfg.rate_limit > 0:
    count = increment_counter(domain, window=1min)
    if count > cfg.rate_limit:
      reject 421 "rate limit exceeded, try again later"
      emit log event: { stage: "rate_limited", domain, count }
      return

token bucket or sliding window counter per domain per minute
```

### Key Files/Modules

- `smtp/server.go`: rate limit enforcement in SMTP handler
- `router/domain_config.go`: domain config with rate_limit field
- `api/server.go`: PATCH /domains/:id

---

## E2E / Verification Checklist

- [ ] Set `rate_limit: 10` on domain; send 15 emails rapidly; verify 5 rejected with 421.
- [ ] Verify `rate_limited` log events for rejected messages.
- [ ] Verify messages within limit are processed normally.
- [ ] Set rate_limit to 0 (unlimited); verify no rejections.

---

## Related Stories

- [[US-0201]](./US-0201-multi-domain.md) — Configure multiple domains
- [[US-0209]](./US-0209-health-check.md) — Monitor service health and metrics
