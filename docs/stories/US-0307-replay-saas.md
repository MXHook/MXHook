# User Story: Replay Failed Delivery from SaaS UI

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to replay a failed webhook delivery from the SaaS
dashboard so that my workflow processes the message after I fix the n8n endpoint.

---

## Context

n8n workflow had wrong webhook URL. 3 emails landed in DLQ. Builder fixes n8n, then
replays from SaaS dashboard — no CLI, no SSH required.

---

## Acceptance Criteria

- [ ] Dashboard shows DLQ entries with "Replay" button per entry.
- [ ] `POST /api/replay { message_id: "<id>" }` replays via API.
- [ ] Replay uses current route config (updated target URL).
- [ ] Replay success removes entry from DLQ view.
- [ ] Bulk replay: "Replay All Failed" button replays all DLQ for account.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
POST /api/replay { message_id }:
  verify message belongs to authenticated account
  re-enqueue message with current route config
  remove from DLQ
  return { status: "replaying", message_id }

POST /api/replay/bulk { route_id?, since? }:
  select DLQ entries for account (+ optional filters)
  for each: re-enqueue
  return { job_id, count }
```

### Key Files/Modules

- `api/server.go`: POST /replay, POST /replay/bulk
- `webhook/replay.go`: re-enqueue logic

---

## E2E / Verification Checklist

- [ ] Force 3 DLQ entries; use API to replay one; verify delivery + removed from DLQ.
- [ ] Replay with wrong account token; verify 403.
- [ ] Bulk replay; verify all 3 entries re-delivered.
- [ ] Replay to updated target URL; verify new URL receives message.

---

## Related Stories

- [[US-0306]](./US-0306-delivery-status.md) — Check delivery status
- [[US-0106]](./US-0106-replay-failed.md) — Replay failed delivery (solo dev)
