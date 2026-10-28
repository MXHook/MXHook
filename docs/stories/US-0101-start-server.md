# User Story: Start MXHook on a VPS

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to start MXHook on a cheap VPS with a single command so that
I can begin receiving email within minutes.

---

## Context

Solo dev has a VPS, a domain, and MX records pointing at the server. Needs to start the
SMTP server and REST API with minimal config. SQLite is the default queue backend;
Postgres is optional for scale and high availability.

---

## Acceptance Criteria

- [ ] `mxhook start` boots SMTP server (port 25) and REST API (port 8080) with defaults.
- [ ] Config loaded from env vars or `mxhook.yaml`; sensible defaults for all optional fields.
- [ ] Startup logs show: SMTP listening, API listening, queue backend connected.
- [ ] Graceful shutdown on SIGTERM; in-flight messages not dropped.
- [ ] Exit with non-zero code and clear error if SQLite is unwritable or an explicitly
  configured Postgres backend is unreachable at startup.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
cmd start:
  load config (env > file > defaults)
  connect queue backend (sqlite default)
  start SMTP server goroutine on :25
  start REST API goroutine on :8080
  wait for SIGTERM
  graceful shutdown: drain in-flight, close listeners
```

### Key Files/Modules

- `cli/start.go`: start command entrypoint
- `smtp/server.go`: SMTP server lifecycle
- `api/server.go`: REST API server lifecycle
- `internal/lifecycle/lifecycle.go`: shutdown coordination

---

## E2E / Verification Checklist

- [ ] Run `mxhook start`; verify startup log lines for SMTP + API + queue.
- [ ] Send `QUIT` signal; verify graceful shutdown log.
- [ ] Set bad DB URL; verify non-zero exit and clear error message.
- [ ] Verify port 25 and 8080 are listening via `ss -tlnp`.

---

## Related Stories

- [[US-0102]](./US-0102-add-domain.md) — Add a domain
- [[US-0103]](./US-0103-add-route.md) — Add a webhook route
