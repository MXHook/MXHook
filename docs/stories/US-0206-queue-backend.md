# User Story: Switch Queue Backend to Redis or NATS

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to configure MXHook to use Redis or NATS as the message
queue so that I can integrate with my existing infrastructure without running extra Postgres.

---

## Context

Platform team already runs Redis/NATS for other services. Postgres queue works but adds
DB load. Engineer needs to swap backend via config with no code changes.

---

## Acceptance Criteria

- [ ] Queue backend selectable via config: `MXHOOK_QUEUE_BACKEND=redis|nats|postgres|cloudflare`.
- [ ] Consumer-capable backends satisfy `queue.Enqueuer` and the receipt-fenced
  `queue.Dequeuer` contract.
- [ ] Switching backend requires only config change + restart (no data migration for new deploys).
- [ ] Connection failure at startup logs clear error identifying the backend + reason.
- [ ] Backend-specific config (URL, credentials) loaded from env vars.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
queue.Enqueuer interface:
  Enqueue(msg Message) error

queue.Dequeuer interface:
  Claim() (Delivery, error)
  Ack(id, receipt string) error
  Nack(id, receipt, reason string) error
  Extend(id, receipt string, duration) (deadline, error)

factory(config):
  switch config.QueueBackend:
    case "redis":    return redis.NewAdapter(config.RedisURL)
    case "nats":     return nats.NewAdapter(config.NatsURL)
    case "postgres": return postgres.NewAdapter(config.DatabaseURL)
    default: return error "unknown backend"
```

### Key Files/Modules

- `queue/queue.go`: Queue interface
- `queue/postgres/adapter.go`: Postgres implementation
- `queue/cloudflare/cloudflare.go`: Cloudflare Queues implementation
- `main.go`: backend factory + wiring

---

## E2E / Verification Checklist

- [ ] Set `MXHOOK_QUEUE_BACKEND=redis`; start; verify Redis connection in startup log.
- [ ] Send email; verify message enqueued + delivered via Redis.
- [ ] Set invalid backend; verify clear error at startup.
- [ ] Switch back to postgres; verify identical behavior.

---

## Related Stories

- [[US-0503]](./US-0503-new-queue-adapter.md) — Implement a new queue adapter (contributor)
