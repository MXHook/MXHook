# User Story: Open a PR with Tests and Docs

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want clear PR contribution guidelines so that my PR is
accepted without multiple "please add tests" or "please update docs" review cycles.

---

## Context

Contributor opens PR for NATS adapter. Maintainer reviews. Without clear guidelines,
back-and-forth on test coverage, code style, and docs wastes both parties' time.

---

## Acceptance Criteria

- [ ] `docs/contributing.md` defines PR checklist: tests, docs, lint, commit format.
- [ ] PR template (`.github/pull_request_template.md`) includes self-review checklist.
- [ ] CI gate: lint (`golangci-lint`) + unit tests must pass before review requested.
- [ ] CI gate: test coverage for changed packages must not decrease.
- [ ] Commit format: Conventional Commits (`feat:`, `fix:`, `docs:` etc.).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
PR checklist (from template):
  [ ] go test ./... passes
  [ ] golangci-lint passes
  [ ] New public interface has Go doc comment
  [ ] CHANGELOG updated (for user-visible changes)
  [ ] Config env vars documented
  [ ] Integration test for new adapter/backend

.github/pull_request_template.md:
  ## What
  ## Why
  ## Checklist
    - [ ] Tests added
    - [ ] Lint passes
    - [ ] Docs updated
```

### Key Files/Modules

- `docs/contributing.md`: contribution guide
- `.github/pull_request_template.md`: PR template (to be created)
- `.github/workflows/lint.yml`: lint CI gate
- `.golangci.yml`: linter config

---

## E2E / Verification Checklist

- [ ] Open draft PR; verify CI runs lint + tests automatically.
- [ ] Omit test; verify CI coverage gate fails.
- [ ] Follow checklist; verify PR approved in ≤ 2 review cycles.
- [ ] Merged PR updates CHANGELOG correctly.

---

## Related Stories

- [[US-0507]](./US-0507-ci-green.md) — Get CI green before requesting review
- [[US-0509]](./US-0509-release-notes.md) — Understand release and changelog process
