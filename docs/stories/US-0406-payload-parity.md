# User Story: Validate Payload Parity with Existing Vendor

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Engineer, I want to verify that MXHook's webhook payload covers all fields
our existing Mailgun/Sendgrid consumers depend on so that migration requires minimal
code changes.

---

## Context

Company's webhook consumers were written against Mailgun inbound payload. Migration
requires either payload parity or a clear mapping guide. Undocumented field differences
cause silent data loss.

---

## Acceptance Criteria

- [ ] MXHook payload covers: from, to, subject, plain text body, HTML body, attachments,
  message-id, received headers, CC, BCC, reply-to.
- [ ] Migration guide documents field mapping: Mailgun → MXHook, Sendgrid → MXHook.
- [ ] Fields present in MXHook but not Mailgun documented as additions.
- [ ] Fields in Mailgun not in MXHook documented with workaround or roadmap note.
- [ ] Sample payloads from both systems available in docs for side-by-side comparison.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
// Mailgun field → MXHook field
"sender"           → envelope.from
"recipient"        → envelope.to
"subject"          → subject
"body-plain"       → text
"body-html"        → html
"Message-Id"       → headers["Message-Id"][0]
"attachment-count" → len(attachments)
"attachment-1"     → attachments[0]
"Cc"               → headers["Cc"]
```

### Key Files/Modules

- `docs/migration/mailgun.md`: field mapping guide (to be created)
- `api/openapi.yaml`: canonical field reference
- `internal/types/email.go`: EmailEvent struct

---

## E2E / Verification Checklist

- [ ] Send email with CC, BCC, reply-to; verify fields present in payload.
- [ ] Compare MXHook payload to saved Mailgun sample; verify no missing critical fields.
- [ ] Existing consumer code updated per migration guide; verify zero 500 errors.
- [ ] Message-Id header preserved in `headers` map.

---

## Related Stories

- [[US-0304]](./US-0304-payload-schema.md) — Consume stable webhook payload schema
- [[US-0401]](./US-0401-evaluate-oss.md) — Evaluate MXHook OSS on staging
