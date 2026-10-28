# User Story: Route Plus-Addressed Email to Workflow

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want to route `orders+new@mystore.com` and
`orders+returns@mystore.com` to separate n8n webhook endpoints so that my workflows
stay separated without managing multiple email addresses.

---

## Context

Builder has one domain but multiple workflows. Plus-addressing is the namespace mechanism.
No coding required — pure route configuration via dashboard or API.

---

## Acceptance Criteria

- [ ] Route `orders+new@mystore.com` → n8n new-order webhook.
- [ ] Route `orders+returns@mystore.com` → n8n returns webhook.
- [ ] Route `orders+*@mystore.com` (wildcard) → default orders webhook for unmatched tags.
- [ ] Each route gets its own signing secret (optional).
- [ ] Tag value available in payload as `envelope.to` so workflow can branch on it.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
// Route table for automation builder
routes:
  { match: "orders+new@mystore.com",     target: "https://n8n.../webhook/new-order" }
  { match: "orders+returns@mystore.com", target: "https://n8n.../webhook/returns" }
  { match: "orders+*@mystore.com",       target: "https://n8n.../webhook/orders-default" }

// n8n workflow: branch on tag
tag = envelope.to.split("+")[1].split("@")[0]
switch tag:
  "new" → create order
  "returns" → create return ticket
  default → log and alert
```

### Key Files/Modules

- `router/matcher.go`: plus-tag matching
- `api/server.go`: POST /routes

---

## E2E / Verification Checklist

- [ ] Send to `orders+new@mystore.com`; verify only new-order webhook fires.
- [ ] Send to `orders+returns@mystore.com`; verify only returns webhook fires.
- [ ] Send to `orders+unknown@mystore.com`; verify wildcard route fires.
- [ ] Verify `envelope.to` in each payload contains the full plus-addressed recipient.

---

## Related Stories

- [[US-0108]](./US-0108-plus-addressing.md) — Route plus-addressed email (solo dev)
- [[US-0304]](./US-0304-payload-schema.md) — Consume stable webhook payload schema
