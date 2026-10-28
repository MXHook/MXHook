# User Story: Run Full Test Suite Locally

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to run the full test suite locally on a clean clone so
that I can verify my changes don't break anything before opening a PR.

---

## Context

Contributor clones repo, makes changes, runs tests. SQLite tests need no external
service; optional Postgres integration tests use the Compose profile. `just test`
runs targeted suites cleanly.

---

## Acceptance Criteria

- [ ] `docker compose --profile postgres up -d postgres` starts optional Postgres test
  dependencies in < 2 min.
- [ ] `just test` or `go test ./...` passes on clean clone with deps running.
- [ ] Integration tests tagged with `//go:build integration`; unit tests runnable without Docker.
- [ ] `just test-unit` runs only unit tests (no Docker required).
- [ ] CI runs same test suite; green on `next` and `main` at all times.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
justfile:
  test-unit:
    go test -count=1 $(go list ./... | grep -v integration) ./...

  test-integration:
    docker compose up -d
    go test -tags integration -count=1 ./...

  test:  test-unit + test-integration

test file tags:
  //go:build integration
  // requires TEST_DATABASE_URL env var
```

### Key Files/Modules

- `docker-compose.yml`: test dependency services
- `justfile`: test targets (to be created/verified)
- `queue/postgres/adapter_test.go`: example integration test

---

## E2E / Verification Checklist

- [ ] Clean clone + `docker compose up -d`; `just test-unit` passes without Docker.
- [ ] `just test-integration` passes with Docker running.
- [ ] Kill Postgres; `just test-unit` still passes.
- [ ] CI badge on README is green.

---

## Related Stories

- [[US-0501]](./US-0501-explore-codebase.md) — Explore codebase and find interfaces
- [[US-0507]](./US-0507-ci-green.md) — Get CI green before requesting review
