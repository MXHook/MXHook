# User Story: Monitor Service Health and Metrics

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want MXHook to expose health endpoints and Prometheus metrics
so that I can integrate it into my existing monitoring stack and get paged on anomalies.

---

## Context

Platform team runs Prometheus + Grafana. MXHook must expose `/healthz`, `/readyz`, and
`/metrics` (Prometheus format) so standard K8s probes and scrape configs work out of the box.

---

## Acceptance Criteria

- [ ] `GET /healthz` returns 200 if service is running (liveness).
- [ ] `GET /readyz` returns 200 only if queue + DB connections are healthy (readiness).
- [ ] `GET /metrics` returns Prometheus-format metrics.
- [ ] Key metrics exposed: `mxhook_smtp_accepted_total`, `mxhook_messages_parsed_total`,
  `mxhook_deliveries_total{status}`, `mxhook_dlq_depth`, `mxhook_queue_depth`.
- [ ] Grafana dashboard config provided in `deploy/grafana/`.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
GET /healthz:
  return 200 { status: "ok" }

GET /readyz:
  check queue.Ping()
  check db.Ping()
  if any fail: return 503 { status: "unhealthy", checks: {...} }
  return 200

GET /metrics:
  return prometheus.DefaultGatherer output

counters:
  smtp_accepted_total (counter)
  messages_parsed_total (counter)
  deliveries_total{status="ok"|"failed"} (counter)
  dlq_depth (gauge)
  queue_depth (gauge)
  webhook_latency_seconds (histogram)
```

### Key Files/Modules

- `api/health_test.go`: health endpoint tests
- `internal/metrics/metrics.go`: Prometheus metric definitions
- `deploy/grafana/mxhook-dashboard.json`: Grafana dashboard

---

## E2E / Verification Checklist

- [ ] GET /healthz; verify 200.
- [ ] Kill DB; GET /readyz; verify 503 + which check failed.
- [ ] GET /metrics; verify counter names present + incrementing after sending emails.
- [ ] Import `mxhook-dashboard.json` into Grafana; verify panels populate.

---

## Related Stories

- [[US-0107]](./US-0107-view-logs.md) — View message processing logs
- [[US-0207]](./US-0207-rate-limits.md) — Apply per-domain rate limits
