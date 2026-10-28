# User Story: Implement a New Queue Adapter

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to implement a NATS queue adapter that satisfies the
the queue contracts so that platform engineers using NATS can use MXHook without
running Postgres.

---

## Context

`queue.Enqueuer` and receipt-fenced `queue.Dequeuer` are the contracts. Contributor implements them for NATS, adds
integration tests, registers it in the factory, and documents it in the config guide.

---

## Acceptance Criteria

- [ ] `queue/nats/adapter.go` implements `queue.Enqueuer` and `queue.Dequeuer`.
- [ ] All methods implemented: `Enqueue`, `Claim`, `Ack`, `Nack`, `Extend`.
- [ ] Integration test in `queue/nats/adapter_test.go` uses real NATS server (docker).
- [ ] Factory in `main.go` accepts `MXHOOK_QUEUE_BACKEND=nats`.
- [ ] Config docs updated: `MXHOOK_NATS_URL` env var documented.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
type NATSAdapter struct {
  conn *nats.Conn
  sub  *nats.Subscription
}

Enqueue(msg EmailMessage):
  data = JSON.marshal(msg)
  conn.Publish("mxhook.queue", data)

Claim() Delivery:
  msg = sub.NextMsg(timeout)
  return Delivery{Message: JSON.unmarshal(msg.Data), Receipt: native_ack_handle}

Ack(id, receipt):
  native_message(receipt).Ack()

Nack(id, receipt, reason):
  native_message(receipt).Term()

Extend(id, receipt, duration):
  native_message(receipt).InProgress()

factory:
  case "nats": return nats.NewAdapter(os.Getenv("MXHOOK_NATS_URL"))
```

### Key Files/Modules

- `queue/queue.go`: interface to implement
- `queue/postgres/adapter.go`: reference implementation
- `queue/nats/adapter.go`: new file (contributor creates)
- `main.go`: factory switch case

---

## E2E / Verification Checklist

- [ ] `MXHOOK_QUEUE_BACKEND=nats go test ./queue/nats/...` passes with NATS running.
- [ ] Full system test: send email; verify delivered via NATS queue.
- [ ] `go vet ./queue/nats/...` passes; no linter warnings.
- [ ] PR description includes: interface compliance, test coverage, config docs.

---

## Related Stories

- [[US-0206]](./US-0206-queue-backend.md) — Switch queue backend (platform eng)
- [[US-0504]](./US-0504-new-storage-backend.md) — Implement a new storage backend
