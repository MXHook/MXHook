# User Story: View Usage Metering

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to view my usage metrics (emails received, webhook
deliveries, storage) so that I can understand my consumption and plan credit top-ups.

---

## Context

Builder runs 3 workflows. Wants to know which route is most active, how much storage
attachments consume, and whether they're on track to exceed their budget.

---

## Acceptance Criteria

- [ ] `GET /api/account/usage?period=30d` returns aggregate usage by type.
- [ ] Usage broken down by: route, domain, period (daily/monthly).
- [ ] Metrics: messages_received, deliveries_sent, deliveries_failed, storage_bytes.
- [ ] Usage data available with < 5 min latency.
- [ ] Export to CSV: `GET /api/account/usage?format=csv&period=30d`.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
UsageSummary {
  period: { start, end }
  totals: { messages, deliveries, deliveries_failed, storage_bytes }
  by_route: [{
    route_id, match,
    messages, deliveries, deliveries_failed, storage_bytes
  }]
  by_day: [{ date, messages, deliveries }]
}

GET /api/account/usage:
  aggregate events from metering table
  group by route / day
  return UsageSummary
```

### Key Files/Modules

- `api/server.go`: GET /account/usage
- `api/store.go`: metering aggregate queries
- `internal/metrics/metrics.go`: usage event emission

---

## E2E / Verification Checklist

- [ ] Send 10 emails across 2 routes; GET usage; verify per-route breakdown.
- [ ] Verify storage_bytes reflects attachment sizes.
- [ ] Request CSV format; verify parseable CSV with correct headers.
- [ ] Verify usage data reflects events within 5 minutes.

---

## Related Stories

- [[US-0308]](./US-0308-billing-credits.md) — Recharge account credits
- [[US-0209]](./US-0209-health-check.md) — Monitor service health and metrics
