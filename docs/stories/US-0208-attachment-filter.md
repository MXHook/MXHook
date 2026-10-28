# User Story: Filter and Store Attachments to S3

**System:** MXHook
**Personas:** Platform Engineer

---

## User Goal

As a Platform Engineer, I want attachments above a size threshold to be stored in S3 and
referenced by URL in the webhook payload so that large files don't bloat webhook bodies.

---

## Context

Customers email invoices (PDFs, 5–10MB). Including raw base64 in webhook payload breaks
downstream consumers. Attachments should be stored in S3; payload carries a presigned URL.

---

## Acceptance Criteria

- [ ] Attachments stored in configured S3-compatible bucket (S3, Minio).
- [ ] Webhook payload `attachments[].url` contains a presigned URL valid for configurable TTL.
- [ ] Size threshold configurable: attachments below threshold can be inlined (base64).
- [ ] Storage backend configurable: `MXHOOK_STORAGE_BACKEND=s3|minio|filesystem`.
- [ ] Storage failure is non-fatal: message still delivered with `url: null` + error logged.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
parser worker post-parse:
  for each attachment in email:
    if attachment.size > config.inline_threshold OR config.always_store:
      key = "attachments/<msg_id>/<filename>"
      storage.Put(key, attachment.content)
      url = storage.PresignURL(key, ttl=config.attachment_url_ttl)
      attachment.url = url
      attachment.content = nil  // strip from payload

storage.Storage interface:
  Put(key string, data []byte) error
  PresignURL(key string, ttl duration) (string, error)
```

### Key Files/Modules

- `storage/storage.go`: Storage interface + backends
- `parser/worker.go`: attachment storage post-parse
- `internal/types/email.go`: Attachment.URL field

---

## E2E / Verification Checklist

- [ ] Send email with 5MB PDF attachment; verify payload has `url` not raw bytes.
- [ ] Fetch URL; verify file content matches original attachment.
- [ ] Set `always_store=false` + small attachment; verify inlined in payload.
- [ ] Kill storage backend; send email; verify delivery succeeds with `url: null` + warning log.

---

## Related Stories

- [[US-0104]](./US-0104-receive-webhook.md) — Receive first webhook payload
- [[US-0305]](./US-0305-attachment-url.md) — Access attachment via URL (automation builder)
