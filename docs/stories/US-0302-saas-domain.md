# User Story: Register a Domain in Hosted SaaS

**System:** MXHook
**Personas:** Automation Builder

> **Partially implemented.** The Worker checks the `_mxhook-verify.<domain>` TXT
> record on an hourly cron and marks matching domains verified. Domain
> registration (`POST /api/domains`) and the on-demand `GET
> /api/domains/:id/verify` endpoint do not exist; the cron is the only trigger.
> See [the Worker README](../../worker/README.md#domain-verification).

---

## User Goal

As an Automation Builder, I want to register my domain in the MXHook SaaS portal so that
inbound emails to my domain are processed and delivered as webhooks.

---

## Context

After signup, builder needs to point their domain's MX record at MXHook's hosted SMTP
servers and verify ownership. Multi-tenant: each account's domains are isolated.

---

## Acceptance Criteria

- [ ] `POST /api/domains { domain: "mystore.com" }` registers domain under authenticated account.
- [ ] Response includes MX record values to configure at registrar.
- [ ] Domain verification via DNS TXT record check: `GET /api/domains/:id/verify`.
- [ ] Unverified domain accepts email but shows `verified: false` status.
- [ ] Domains are tenant-isolated: other accounts cannot see or route for this domain.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
POST /api/domains:
  create domain record with account_id
  generate verification token
  return {
    domain, id,
    mx_records: ["mx1.mxhook.dev", "mx2.mxhook.dev"],
    verification_txt: "_mxhook-verify.<domain> TXT <token>"
  }

GET /api/domains/:id/verify:
  lookup DNS TXT record for _mxhook-verify.<domain>
  compare to stored token
  if match: mark verified = true
  return { verified: bool }
```

### Key Files/Modules

- `api/server.go`: domain registration + verification endpoints
- `api/store.go`: tenant-scoped domain queries

---

## E2E / Verification Checklist

- [ ] Register domain; verify MX record values returned.
- [ ] Set DNS TXT record; call verify endpoint; verify `verified: true`.
- [ ] Account B cannot GET domains belonging to account A.
- [ ] SMTP accepts email for domain after registration (verified or not).

---

## Related Stories

- [[US-0301]](./US-0301-saas-signup.md) — Sign up for MXHook hosted SaaS
- [[US-0303]](./US-0303-plus-route.md) — Route plus-addressed email to workflow
