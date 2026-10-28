# User Story: View Message Processing Logs

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to view structured logs for each message's lifecycle so that
I can debug delivery problems without external tooling.

---

## Context

Each message should emit structured log events at each stage: SMTP accepted, parsed,
queued, delivered, failed, retried. Solo dev uses `mxhook logs` to tail or filter these.

---

## Acceptance Criteria

- [ ] `mxhook logs` tails live structured log output.
- [ ] `mxhook logs --message-id <id>` filters to a specific message.
- [ ] `mxhook logs --since 1h` filters to last hour.
- [ ] Log events include: stage, message_id, timestamp, domain, result, error (if any).
- [ ] Stages emitted: `smtp_accepted`, `parsed`, `queued`, `delivered`, `failed`, `retried`.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
log event structure:
  { stage, message_id, domain, envelope_to, timestamp, duration_ms, error? }

stages emitted:
  smtp_accepted  → SMTP server after DATA accepted
  parsed         → parser worker after EmailEvent built
  queued         → queue.Enqueue success
  delivered      → webhook POST 2xx received
  failed         → webhook POST non-2xx or timeout
  retried        → retry attempt N of M

cmd logs [--message-id] [--since]:
  query log store / stream
  format: timestamp stage message_id domain result
```

### Key Files/Modules

- `cli/logs.go`: logs CLI command
- `internal/metrics/metrics.go`: stage counters + log emission
- `api/server.go`: GET /messages/:id for per-message trace

---

## E2E / Verification Checklist

- [ ] Send email; run `mxhook logs --message-id <id>`; verify all 5 stages present.
- [ ] Force delivery failure; verify `failed` + `retried` stages logged.
- [ ] Run `mxhook logs --since 5m`; verify only recent entries shown.
- [ ] Verify log output is machine-parseable JSON (or structured key=value).

---

## Related Stories

- [[US-0106]](./US-0106-replay-failed.md) — Replay a failed delivery
- [[US-0203]](./US-0203-delivery-trace.md) — Trace a message end-to-end
