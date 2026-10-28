# User Story: Sign Up for MXHook Hosted SaaS

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to sign up for MXHook hosted SaaS and get an API key
so that I can start receiving email webhooks without running any infrastructure.

---

## Context

Automation builder is non-ops; expects SaaS onboarding similar to Mailgun or Resend.
Create account → get API key → add domain → done.

---

## Acceptance Criteria

- [ ] Account creation via email + password or OAuth (GitHub/Google).
- [ ] API key generated on account creation; visible in dashboard and via API.
- [ ] API key scoped to account; used as Bearer token for all API calls.
- [ ] New account starts with a credit balance (free tier or trial credits).
- [ ] Welcome email sent with quickstart link.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
POST /api/auth/register { email, password }:
  create account record
  generate API key: "mxh_<random_32_bytes_hex>"
  assign free tier credits
  send welcome email
  return { account_id, api_key, credits }

auth middleware:
  Authorization: Bearer <api_key>
  lookup api_key → account_id
  inject account_id into request context
```

### Key Files/Modules

- `api/auth_test.go`: auth flow tests
- `api/server.go`: /auth/register, /auth/login
- `api/types.go`: Account, APIKey types

---

## E2E / Verification Checklist

- [ ] Register account; verify API key returned.
- [ ] Use API key to call GET /api/domains; verify 200.
- [ ] Use invalid API key; verify 401.
- [ ] Verify free tier credits assigned to new account.

---

## Related Stories

- [[US-0302]](./US-0302-saas-domain.md) — Register a domain in hosted SaaS
- [[US-0308]](./US-0308-billing-credits.md) — Recharge account credits
