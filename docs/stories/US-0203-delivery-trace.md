# User Story: Trace a Message End-to-End

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to retrieve the full processing trace for a single message
via the API so that I can debug delivery issues during on-call without SSH access.

---

## Context

On-call engineer gets paged: "user says their support email was ignored." Engineer needs
full lifecycle trace: was SMTP accepted? Parsed? Queued? Delivered? Failed at which stage?

---

## Acceptance Criteria

- [ ] `GET /api/messages/:id` returns full message record with lifecycle stages array.
- [ ] Lifecycle stages include timestamps, duration_ms, stage name, result, error (if any).
- [ ] `GET /api/messages?domain=support.acme.com&since=1h` filters by domain + time window.
- [ ] Failed delivery stage includes: HTTP status, response body (truncated), next retry time.
- [ ] Message record available for minimum 30 days after receipt.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
MessageRecord {
  id, envelope, subject, domain,
  received_at, size_bytes,
  lifecycle: [{
    stage: "smtp_accepted" | "parsed" | "queued" | "delivered" | "failed" | "retried",
    timestamp, duration_ms,
    result: "ok" | "error",
    error?: string,
    http_status?: int,
    next_retry?: RFC3339
  }]
}
```

### Key Files/Modules

- `api/server.go`: GET /messages, GET /messages/:id
- `api/store.go`: message + lifecycle DB queries
- `internal/lifecycle/lifecycle.go`: lifecycle event persistence

---

## E2E / Verification Checklist

- [ ] Send email; GET /messages/:id; verify all lifecycle stages present with timestamps.
- [ ] Force delivery failure; verify `failed` stage with HTTP status + error in response.
- [ ] Query GET /messages?domain=support.acme.com&since=30m; verify filtered results.
- [ ] Verify message persists and queryable 24h after receipt.

---

## Related Stories

- [[US-0107]](./US-0107-view-logs.md) — View message processing logs (solo dev)
- [[US-0204]](./US-0204-dlq-inspect.md) — Inspect and replay DLQ entries
