# User Story: Replay a Failed Delivery

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to replay a failed webhook delivery so that I can recover
missed messages after my endpoint was down, without re-sending the original email.

---

## Context

Webhook target may be temporarily unavailable. After exhausting retries, message lands
in DLQ. Solo dev needs to replay once target recovers — CLI or API.

---

## Acceptance Criteria

- [ ] `mxhook replay --message-id <id>` re-enqueues message for delivery.
- [ ] `POST /api/replay { message_id: "<id>" }` equivalent API endpoint.
- [ ] Replay uses current route config (target URL at time of replay, not original delivery).
- [ ] Successful replay removes message from DLQ.
- [ ] Replay of unknown message ID returns 404 with clear error.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
cmd replay --message-id <id>:
  POST /api/replay { message_id: <id> }
  on 200: print "Replayed <id>; new delivery attempt enqueued"
  on 404: print "Message <id> not found in DLQ"

api replay handler:
  lookup message in DLQ by id
  if not found: return 404
  re-enqueue into delivery queue with current route config
  remove from DLQ
  return 200
```

### Key Files/Modules

- `cli/replay.go`: replay CLI command
- `api/server.go`: POST /replay handler
- `webhook/dlq.go`: DLQ read + remove
- `webhook/replay.go`: re-enqueue logic

---

## E2E / Verification Checklist

- [ ] Force webhook failure (kill target); verify message in DLQ after retry exhaustion.
- [ ] Restore target; run `mxhook replay --message-id <id>`; verify delivery succeeds.
- [ ] Verify message removed from DLQ after successful replay.
- [ ] Run replay on non-existent ID; verify 404.

---

## Related Stories

- [[US-0107]](./US-0107-view-logs.md) — View message processing logs
- [[US-0204]](./US-0204-dlq-inspect.md) — Inspect and replay DLQ entries
