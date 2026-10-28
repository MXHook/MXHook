# User Story: Consume Stable Webhook Payload Schema

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want the webhook payload schema to be stable and documented
so that my n8n workflows don't break when MXHook is updated.

---

## Context

Automation workflows are fragile to schema changes. Builder needs confidence that
field names, types, and nesting won't change across patch/minor versions. Breaking
changes require a major version bump and migration path.

---

## Acceptance Criteria

- [ ] Payload schema documented in OpenAPI spec (`api/openapi.yaml`).
- [ ] Schema version included in payload: `_schema: "1.0"`.
- [ ] No field renamed or removed in patch/minor versions.
- [ ] New optional fields may be added in minor versions.
- [ ] Breaking changes (rename, remove, type change) require `_schema: "2.0"` + migration guide.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
EmailEvent v1.0:
  _schema:   "1.0"
  id:        "msg_<uuid>"
  timestamp: RFC3339
  envelope: { from: string, to: string }
  headers:   map[string][]string
  subject:   string
  text:      string         // empty string if absent, never null
  html:      string         // empty string if absent, never null
  attachments: [{
    filename:     string
    content_type: string
    size:         int
    url:          string | null
  }]
```

### Key Files/Modules

- `api/openapi.yaml`: EmailEvent schema definition
- `internal/types/email.go`: Go struct (source of truth)
- `webhook/delivery.go`: serialization

---

## E2E / Verification Checklist

- [ ] Send 3 email variants (text-only, HTML-only, multipart+attachments); verify schema consistent.
- [ ] Verify `text` and `html` are empty string not null when absent.
- [ ] Verify `_schema` field present in all payloads.
- [ ] Validate payload against `api/openapi.yaml` schema in integration test.

---

## Related Stories

- [[US-0104]](./US-0104-receive-webhook.md) — Receive first webhook payload
- [[US-0305]](./US-0305-attachment-url.md) — Access attachment via URL
