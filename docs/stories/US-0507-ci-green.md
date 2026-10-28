# User Story: Get CI Green Before Requesting Review

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want CI to run automatically on my branch so that I can fix
issues before requesting maintainer review, saving everyone's time.

---

## Context

CI runs on every push. Contributor pushes NATS adapter branch; CI runs build + lint +
unit + integration tests. Red CI means no review requested until fixed.

---

## Acceptance Criteria

- [ ] CI runs on: push to any branch, pull_request targeting main.
- [ ] CI pipeline: build → lint → unit tests → integration tests.
- [ ] Build step: `go build ./...` must pass.
- [ ] Lint step: `golangci-lint run` with project `.golangci.yml` config.
- [ ] Integration tests run with docker-compose services (Postgres at minimum).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
.github/workflows/build.yml:
  on: [push, pull_request]
  jobs:
    build:
      go build ./...
    lint:
      golangci-lint run
    test-unit:
      go test -count=1 $(non-integration packages)
    test-integration:
      docker compose up -d postgres
      go test -tags integration ./...
```

### Key Files/Modules

- `.github/workflows/build.yml`: build + test CI
- `.github/workflows/lint.yml`: lint CI
- `.golangci.yml`: linter rules
- `.github/workflows/coverage.yml`: coverage tracking

---

## E2E / Verification Checklist

- [ ] Push new branch; verify CI triggers within 1 min.
- [ ] Introduce lint error; verify lint job fails with clear message.
- [ ] Break integration test; verify test job fails; unit job still passes.
- [ ] Fix all issues; verify all CI jobs green.

---

## Related Stories

- [[US-0502]](./US-0502-run-tests.md) — Run full test suite locally
- [[US-0506]](./US-0506-open-pr.md) — Open a PR with tests and docs
