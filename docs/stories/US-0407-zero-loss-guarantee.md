# User Story: Zero Message Loss Guarantee

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead, I want assurance that MXHook will not silently drop messages so that
I can commit to our product team that all inbound emails will be processed.

---

## Context

Current Mailgun inbound has had silent drops (no webhook, no log). MXHook must guarantee:
every SMTP-accepted message either gets delivered or lands in DLQ — never silently dropped.

---

## Acceptance Criteria

- [ ] Every SMTP DATA accepted generates a persisted queue entry before returning 250.
- [ ] Queue entry persisted durably (Postgres WAL or equivalent) before ACK.
- [ ] If parse/delivery fails, message moves to DLQ (never deleted).
- [ ] DLQ entries never automatically purged without explicit operator action.
- [ ] `GET /api/dlq/count` available to monitor DLQ depth.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
smtp DATA handler:
  receive full message
  BEGIN TRANSACTION
    write raw message to storage (or queue)
    enqueue message_id in queue
  COMMIT
  return 250 OK  // only after durable write

worker:
  dequeue message_id
  parse + attempt delivery
  if delivery fails after N retries:
    move to DLQ (durable write)
    // never delete original message
  ACK queue entry
```

### Key Files/Modules

- `smtp/server.go`: DATA handler with transactional enqueue
- `queue/postgres/adapter.go`: durable queue write
- `webhook/dlq.go`: DLQ as final state, not ephemeral

---

## E2E / Verification Checklist

- [ ] Send 1000 emails; verify 1000 in delivery log (delivered + DLQ = 1000).
- [ ] Kill worker mid-processing; restart; verify in-flight message reprocessed, not lost.
- [ ] Force delivery failure; verify message in DLQ, not silently dropped.
- [ ] GET /api/dlq/count after 0 DLQ entries; verify 0.

---

## Related Stories

- [[US-0204]](./US-0204-dlq-inspect.md) — Inspect and replay DLQ entries
- [[US-0406]](./US-0406-payload-parity.md) — Validate payload parity
