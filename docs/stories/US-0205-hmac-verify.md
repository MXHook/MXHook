# User Story: Enforce HMAC Signature Validation

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want to enforce HMAC signature validation on all webhook
endpoints so that spoofed requests are rejected at the receiving service level.

---

## Context

Platform eng deploys MXHook alongside internal microservices. Each service validates
`X-MXHook-Signature`. Secrets managed via Vault/env vars. Rotation must be zero-downtime.

---

## Acceptance Criteria

- [ ] Route secret configurable via API: `PATCH /api/routes/:id { secret: "<new>" }`.
- [ ] Old + new secret both valid during rotation window (dual-secret support).
- [ ] Signing uses HMAC-SHA256 over raw JSON body (not re-serialized).
- [ ] Signature header format: `sha256=<hex>` (GitHub-compatible).
- [ ] `GET /api/routes/:id` does NOT return secret value (write-only field).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
route secret rotation:
  PATCH /api/routes/:id { secret: "newkey", secret_prev: "oldkey" }
  delivery: try newkey first; if receiver rejects (401), try oldkey
  after rotation_window expires: drop oldkey

signature format:
  "sha256=" + lower_hex(HMAC-SHA256(secret, raw_body_bytes))

GET /api/routes/:id:
  return route without secret field (redacted)
```

### Key Files/Modules

- `webhook/signer.go`: HMAC + dual-secret logic
- `api/types.go`: Route type; secret write-only serialization
- `api/server.go`: PATCH /routes/:id

---

## E2E / Verification Checklist

- [ ] Set secret on route; verify header present + correct on delivery.
- [ ] PATCH to rotate secret; verify both old and new accepted during window.
- [ ] GET /api/routes/:id; verify secret field absent from response.
- [ ] Consumer rejects request with invalid signature; verify 401 in consumer logs.

---

## Related Stories

- [[US-0105]](./US-0105-verify-signature.md) — Verify HMAC webhook signature (solo dev)
