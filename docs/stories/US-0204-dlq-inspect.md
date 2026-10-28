# User Story: Inspect and Replay DLQ Entries

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to inspect dead-letter queue entries and selectively replay
them via API so that I can recover from delivery failures without re-sending original emails.

---

## Context

Webhook target had a 2-hour outage. ~500 messages landed in DLQ. Platform eng needs to
inspect, triage, and bulk replay without touching message sources.

---

## Acceptance Criteria

- [ ] `GET /api/dlq` lists DLQ entries with message_id, envelope, route, failure reason, retry_count.
- [ ] `GET /api/dlq?domain=<d>&limit=100&offset=0` supports pagination and domain filter.
- [ ] `POST /api/replay { message_id: "<id>" }` replays single entry.
- [ ] `POST /api/replay { domain: "<d>", since: "<t>" }` bulk replays all DLQ for domain+window.
- [ ] Bulk replay is async; returns job_id for status polling.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
GET /api/dlq:
  query dlq table where status = "dead"
  support filter: domain, since, limit, offset
  return paginated list

POST /api/replay { domain, since }:
  create replay job
  async: for each dlq entry matching filter:
    re-enqueue with current route config
    mark dlq entry as "replaying"
  return { job_id, count }

GET /api/replay/:job_id:
  return { status, replayed, failed, remaining }
```

### Key Files/Modules

- `api/dlq_test.go`: DLQ API test coverage
- `webhook/dlq.go`: DLQ persistence
- `webhook/replay.go`: replay orchestration

---

## E2E / Verification Checklist

- [ ] Force 5 messages to DLQ; GET /api/dlq; verify all 5 listed with failure reason.
- [ ] POST /api/replay single message; verify delivery + removed from DLQ.
- [ ] POST /api/replay bulk; verify job_id returned; poll until complete.
- [ ] Verify bulk replay doesn't re-attempt already-replaying entries.

---

## Related Stories

- [[US-0106]](./US-0106-replay-failed.md) — Replay a failed delivery (solo dev)
- [[US-0203]](./US-0203-delivery-trace.md) — Trace a message end-to-end
