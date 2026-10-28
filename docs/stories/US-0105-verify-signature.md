# User Story: Verify HMAC Webhook Signature

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to verify that webhook POSTs originate from my MXHook instance
so that my endpoint is protected from spoofed requests.

---

## Context

HMAC-SHA256 signature over the raw request body. Signing secret configured per route.
Standard pattern matching Stripe/GitHub webhook security.

---

## Acceptance Criteria

- [ ] Route can be configured with a `--secret` signing key.
- [ ] `X-MXHook-Signature: sha256=<hex>` header present on all signed deliveries.
- [ ] Signature computed as HMAC-SHA256(secret, raw_body).
- [ ] Receiver can verify: `expected = HMAC(secret, body); compare constant-time`.
- [ ] Requests with missing/invalid signature rejected by example receiver code (docs).

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
delivery:
  raw_body = JSON.serialize(event)
  if route.secret != "":
    sig = HMAC-SHA256(route.secret, raw_body)
    headers["X-MXHook-Signature"] = "sha256=" + hex(sig)
  POST target_url, headers, raw_body

receiver verification:
  expected = "sha256=" + hex(HMAC-SHA256(secret, request.body))
  if not constant_time_equal(expected, request.header["X-MXHook-Signature"]):
    return 401
```

### Key Files/Modules

- `webhook/signer.go`: HMAC signing logic
- `cli/route.go`: `--secret` flag on route add
- `api/types.go`: Route struct with secret field

---

## E2E / Verification Checklist

- [ ] Add route with `--secret mysecret`; send email; verify header present + valid.
- [ ] Tamper with body; verify signature mismatch detectable by receiver.
- [ ] Route without secret; verify header absent (or document default behavior).
- [ ] Verify constant-time comparison used (no timing oracle).

---

## Related Stories

- [[US-0104]](./US-0104-receive-webhook.md) — Receive first webhook payload
- [[US-0205]](./US-0205-hmac-verify.md) — Enforce HMAC signature validation (platform eng)
