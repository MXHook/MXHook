# User Story: Receive First Webhook Payload

**System:** MXHook
**Personas:** Solo Developer

---

## User Goal

As a Solo Developer, I want to receive a structured JSON webhook payload when an email
arrives so that my app can process the message without writing an email parser.

---

## Context

The payload contract is the core product value. It must be stable, fully documented,
and cover all message parts: envelope, headers, subject, text, HTML, attachments.

---

## Acceptance Criteria

- [ ] Webhook POST contains: `id`, `timestamp`, `envelope.from`, `envelope.to`,
  `headers`, `subject`, `text`, `html`, `attachments[]`.
- [ ] `Content-Type: application/json` header on POST.
- [ ] `X-MXHook-Signature` HMAC header present (even if route has no secret configured,
  a default key is used or header is omitted — documented behavior).
- [ ] Attachment objects include: `filename`, `content_type`, `size`, `url` (storage ref).
- [ ] Payload schema is stable across patch versions; breaking changes require major version.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
EmailEvent {
  id:          string   // msg_<uuid>
  timestamp:   RFC3339
  envelope: {
    from:      string
    to:        string
  }
  headers:     map[string][]string
  subject:     string
  text:        string
  html:        string
  attachments: []{
    filename:     string
    content_type: string
    size:         int
    url:          string  // storage URL
  }
}
```

### Key Files/Modules

- `internal/types/email.go`: EmailEvent type definition
- `parser/parser.go`: RFC 5322 → EmailEvent
- `webhook/delivery.go`: serialize + POST

---

## E2E / Verification Checklist

- [ ] Send multipart email with text + HTML + attachment; verify all fields populated.
- [ ] Send plain-text-only email; verify `html` is empty string, not null.
- [ ] Verify `Content-Type: application/json` on webhook POST.
- [ ] Verify `X-MXHook-Signature` header present and correct (see US-0105).

---

## Related Stories

- [[US-0105]](./US-0105-verify-signature.md) — Verify HMAC webhook signature
- [[US-0103]](./US-0103-add-route.md) — Add a webhook route
