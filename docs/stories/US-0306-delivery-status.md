# User Story: Check Delivery Status via SaaS Dashboard

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to check the delivery status of a webhook via the SaaS
dashboard so that I can confirm my workflow received the email without checking n8n logs.

---

## Context

Builder doesn't have SSH or server access. Needs self-serve visibility into whether a
specific email was received, parsed, and delivered — from the MXHook dashboard or API.

---

## Acceptance Criteria

- [ ] `GET /api/deliveries?since=1h` returns recent delivery attempts with status.
- [ ] Each delivery record shows: message_id, route, target, status, http_status, timestamp.
- [ ] Filter by: route, status (delivered/failed/pending), since.
- [ ] Link from delivery record to full message details (subject, envelope).
- [ ] Dashboard shows delivery list with real-time status (poll or SSE).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
DeliveryRecord {
  id, message_id, route_id,
  target_url, status, http_status,
  attempt_count, last_attempt_at,
  next_retry_at?,
  error?: string
}

GET /api/deliveries:
  filter by account_id (tenant isolation)
  support: since, status, route_id
  return paginated list
```

### Key Files/Modules

- `api/server.go`: GET /deliveries
- `api/store.go`: delivery queries (tenant-scoped)

---

## E2E / Verification Checklist

- [ ] Send email; GET /api/deliveries; verify entry with `delivered` status.
- [ ] Force failure; verify entry with `failed` status + http_status + error.
- [ ] Filter by `status=failed`; verify only failures returned.
- [ ] Account B cannot see account A's deliveries.

---

## Related Stories

- [[US-0307]](./US-0307-replay-saas.md) — Replay failed delivery from SaaS UI
- [[US-0203]](./US-0203-delivery-trace.md) — Trace a message end-to-end (platform eng)
