# User Story: Evaluate Enterprise Edition License

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead, I want to evaluate MXHook Enterprise Edition features so that I can
justify the license cost to our CTO before migrating from the vendor.

---

## Context

OSS covers the happy path. EE adds analytics, RBAC, monitoring dashboards, and advanced
routing — features the platform team needs at scale. License evaluation is part of the
buying decision.

---

## Acceptance Criteria

- [ ] EE trial activatable via license key in config: `MXHOOK_LICENSE_KEY=<key>`.
- [ ] EE features clearly gated: unlocked only with valid license.
- [ ] Invalid/expired license logs warning but degrades gracefully to OSS features.
- [ ] `mxhook status` shows license type (OSS / EE Trial / EE Licensed) + expiry.
- [ ] EE feature list documented; easy to compare OSS vs EE.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
license check:
  load MXHOOK_LICENSE_KEY from env
  validate: signature, expiry, feature_set
  if invalid: log warning "EE features unavailable; running in OSS mode"
  if valid: unlock EE feature flags

mxhook status output:
  License: EE Trial (expires 2026-04-25)
  Features: analytics, rbac, advanced_routing, monitoring
```

### Key Files/Modules

- `ee/license.go`: license validation
- `ee/license_test.go`: license test coverage
- `cli/status.go`: status command

---

## E2E / Verification Checklist

- [ ] Set valid EE license key; verify `mxhook status` shows EE mode + features.
- [ ] Set expired license; verify graceful fallback to OSS + warning log.
- [ ] Unset license key; verify OSS mode, no EE endpoints accessible.
- [ ] Verify EE-only endpoint returns 402/403 in OSS mode.

---

## Related Stories

- [[US-0401]](./US-0401-evaluate-oss.md) — Evaluate MXHook OSS on staging
- [[US-0403]](./US-0403-rbac.md) — Configure RBAC roles
