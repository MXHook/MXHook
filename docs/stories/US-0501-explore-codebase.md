# User Story: Explore Codebase and Find Interfaces

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to understand MXHook's package structure and key interfaces
within 30 minutes so that I know exactly what to implement for my contribution.

---

## Context

First-time contributor clones the repo. Needs to find: which interfaces exist, which
files define them, and where adapters are registered — without asking maintainers.

---

## Acceptance Criteria

- [ ] `docs/contributing.md` exists with: package map, interface list, how to add adapter.
- [ ] Each interface has a Go doc comment explaining contract + invariants.
- [ ] `queue/queue.go` defines the `Queue` interface with clear method docs.
- [ ] `storage/storage.go` defines the `Storage` interface with clear method docs.
- [ ] Example adapter (e.g., `queue/postgres/`) serves as reference implementation.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
// Package map (from contributing.md)
smtp/     → SMTP server; calls queue.Enqueue on DATA accept
queue/    → Queue interface + adapters (postgres, cloudflare)
parser/   → RFC 5322 → EmailEvent; calls storage for attachments
router/   → route matching engine
webhook/  → delivery, retry, DLQ, replay
storage/  → Storage interface + adapters
api/      → REST API
cli/      → CLI commands
ee/       → Enterprise Edition feature gates
internal/ → metrics, lifecycle, types
```

### Key Files/Modules

- `queue/queue.go`: Queue interface
- `storage/storage.go`: Storage interface
- `docs/contributing.md`: contributor guide (to be created)

---

## E2E / Verification Checklist

- [ ] Read queue/queue.go; verify interface + doc comments are clear.
- [ ] Find existing adapter (postgres); understand pattern to replicate.
- [ ] Read contributing.md; verify package map + "how to add adapter" section present.
- [ ] `go doc ./queue` outputs useful interface documentation.

---

## Related Stories

- [[US-0502]](./US-0502-run-tests.md) — Run full test suite locally
- [[US-0503]](./US-0503-new-queue-adapter.md) — Implement a new queue adapter
