# User Story: Fix a Bug with a Failing Test First

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to fix a bug by writing a failing test first so that the
fix is verifiable and the bug cannot regress.

---

## Context

Contributor finds issue: plus-addressed emails with Unicode tags are misrouted. No
existing test covers this. Standard TDD approach: failing test → minimal fix → green.

---

## Acceptance Criteria

- [ ] Bug reproduced by a new failing test before any code change.
- [ ] Fix is minimal — only changes code necessary to make test pass.
- [ ] All pre-existing tests still pass after fix.
- [ ] Test added to the relevant package test file (not a new file unless justified).
- [ ] Commit message: `fix: <description>` per Conventional Commits.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
// Step 1: Write failing test
func TestPlusAddressUnicodeTag(t *testing.T):
  route = Route{ match: "support+*@myapp.io", target: "http://test" }
  email = EmailEvent{ envelope: { to: "support+héllo@myapp.io" } }
  matched = matcher.Match(email, [route])
  assert matched != nil  // FAILS before fix

// Step 2: Fix matcher to normalize Unicode in plus tags
func parseLocalPart(addr string):
  local, tag = split on "+"
  tag = unicode.ToLower(tag)  // normalize
  return local, tag

// Step 3: Test passes
```

### Key Files/Modules

- `router/matcher.go`: fix location (example)
- `router/matcher_test.go`: add failing test here

---

## E2E / Verification Checklist

- [ ] New test exists and FAILS before code change (commit test first).
- [ ] After fix: new test passes; `go test ./router/...` all green.
- [ ] `go test ./...` all green (no regression).
- [ ] PR description references the issue number.

---

## Related Stories

- [[US-0502]](./US-0502-run-tests.md) — Run full test suite locally
- [[US-0506]](./US-0506-open-pr.md) — Open a PR with tests and docs
