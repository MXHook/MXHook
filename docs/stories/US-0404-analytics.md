# User Story: View Email Volume Analytics

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead, I want to view email volume, delivery success rate, and processing
latency analytics so that I can report on MXHook's reliability to our CTO.

---

## Context

EE feature. Leadership wants a monthly reliability report. Platform lead needs volume
trends, p95 latency, and delivery success rate without querying raw metrics.

---

## Acceptance Criteria

- [ ] `GET /api/analytics/summary?period=30d` returns key metrics (EE only).
- [ ] Metrics: messages_received, delivery_success_rate, p50/p95/p99 webhook latency,
  dlq_rate, attachment_volume_bytes.
- [ ] Breakdown by domain and route.
- [ ] Data refreshed every 5 minutes.
- [ ] Grafana dashboard in `deploy/grafana/` covers all above metrics.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
AnalyticsSummary {
  period: { start, end }
  messages_received: int
  delivery_success_rate: float  // 0.0–1.0
  latency: { p50, p95, p99 } // milliseconds
  dlq_rate: float
  attachment_volume_bytes: int
  by_domain: [{ domain, messages, success_rate, p95_latency }]
  by_route: [{ route, messages, success_rate }]
}
```

### Key Files/Modules

- `api/server.go`: GET /analytics/summary (EE gated)
- `internal/metrics/metrics.go`: histogram + counter collection
- `deploy/grafana/mxhook-dashboard.json`: Grafana panels

---

## E2E / Verification Checklist

- [ ] Send 100 emails; GET /analytics/summary; verify all metric fields populated.
- [ ] Verify latency percentiles are plausible (p95 < 1000ms for local test).
- [ ] Verify endpoint returns 402/403 in OSS mode.
- [ ] Verify domain/route breakdown matches delivery log counts.

---

## Related Stories

- [[US-0403]](./US-0403-rbac.md) — Configure RBAC roles
- [[US-0209]](./US-0209-health-check.md) — Monitor service health and metrics
