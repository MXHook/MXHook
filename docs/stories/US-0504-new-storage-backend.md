# User Story: Implement a New Storage Backend

**System:** MXHook
**Personas:** OSS Go Developer

---

## User Goal

As an OSS Go Developer, I want to implement a GCS (Google Cloud Storage) storage backend
so that users on GCP can store email attachments without running S3 or Minio.

---

## Context

storage.Storage interface is the contract. Contributor implements it for GCS, adds
integration tests with a GCS emulator, and registers it in the storage factory.

---

## Acceptance Criteria

- [ ] `storage/gcs/adapter.go` implements `storage.Storage` interface.
- [ ] Methods implemented: `Put(key, data)`, `PresignURL(key, ttl)`.
- [ ] Integration test uses GCS emulator or `fake-gcs-server` in docker-compose.
- [ ] Factory accepts `MXHOOK_STORAGE_BACKEND=gcs`.
- [ ] `MXHOOK_GCS_BUCKET` and auth config documented.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
type GCSAdapter struct {
  client *storage.Client
  bucket string
}

Put(key string, data []byte):
  obj = client.Bucket(bucket).Object(key)
  w = obj.NewWriter(ctx)
  w.Write(data)
  w.Close()

PresignURL(key string, ttl duration):
  opts = SignedURLOptions{ Expires: time.Now().Add(ttl), Method: "GET" }
  return storage.SignedURL(bucket, key, opts)

factory:
  case "gcs": return gcs.NewAdapter(os.Getenv("MXHOOK_GCS_BUCKET"))
```

### Key Files/Modules

- `storage/storage.go`: interface to implement
- `storage/gcs/adapter.go`: new file
- `docker-compose.yml`: add fake-gcs-server service for tests

---

## E2E / Verification Checklist

- [ ] `MXHOOK_STORAGE_BACKEND=gcs go test ./storage/gcs/...` passes with emulator.
- [ ] Send email with attachment; verify stored in GCS bucket + URL in payload.
- [ ] PresignURL expires at correct time.
- [ ] `go vet` passes; no new linter warnings.

---

## Related Stories

- [[US-0503]](./US-0503-new-queue-adapter.md) — Implement a new queue adapter
- [[US-0208]](./US-0208-attachment-filter.md) — Filter and store attachments
