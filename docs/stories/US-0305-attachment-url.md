# User Story: Access Attachment via URL in Payload

**System:** MXHook
**Personas:** Automation Builder

---

## User Goal

As an Automation Builder, I want attachments referenced by a presigned URL in the webhook
payload so that my n8n workflow can download files without handling raw binary data.

---

## Context

Customer emails an invoice PDF. Workflow needs to download it, run OCR, and store in
Google Drive. Raw base64 in webhook body breaks n8n's JSON node size limits.

---

## Acceptance Criteria

- [ ] Attachments stored in hosted SaaS object storage; URL in payload.
- [ ] Presigned URL valid for configurable TTL (default: 24 hours).
- [ ] URL accessible without auth (presigned); no API key required to fetch.
- [ ] `content_type`, `filename`, `size` always present alongside `url`.
- [ ] URL format stable; domain won't change across patch versions.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
parser post-process:
  for attachment in email.attachments:
    key = accounts/<account_id>/attachments/<msg_id>/<filename>
    storage.Put(key, attachment.data)
    url = storage.PresignURL(key, ttl=24h)
    attachment.url = url
    attachment.data = nil  // not included in webhook payload

n8n workflow:
  url = payload.attachments[0].url
  file_bytes = HTTP.GET(url)
  google_drive.upload(file_bytes, filename)
```

### Key Files/Modules

- `storage/storage.go`: S3/Minio presign
- `parser/worker.go`: attachment upload post-parse

---

## E2E / Verification Checklist

- [ ] Send email with PDF attachment; verify payload has URL + correct metadata.
- [ ] Download URL; verify file content matches original.
- [ ] Wait past TTL; verify URL returns 403/410.
- [ ] Verify URL does not contain API key or account credentials in query params.

---

## Related Stories

- [[US-0208]](./US-0208-attachment-filter.md) — Filter and store attachments (platform eng)
- [[US-0304]](./US-0304-payload-schema.md) — Consume stable webhook payload schema
