# Operations and recovery

## Health and monitoring

Probe `GET /healthz` for liveness and `GET /readyz` for readiness. Do not restart
a live process solely because readiness is red; investigate its database or
storage dependency first. Scrape `GET /metrics` with Prometheus.

Alert on sustained readiness failure, growing ingress backlog, webhook retries,
dead deliveries, disk usage above 80%, TLS expiry, and Maildir `new/` plus
`cur/` growth. exe.dev disables receipt when `new/` exceeds 1,000 entries.
Starter Prometheus rules live in
[`deploy/prometheus/mxhook-alerts.yml`](../deploy/prometheus/mxhook-alerts.yml).
Tune thresholds to expected traffic before paging.

The default Compose file publishes the API on localhost, but the `--api-addr`
flag itself defaults to `0.0.0.0:8080`. A process started outside Compose
therefore listens on every interface. Bind it to a loopback or private address,
or keep it behind a firewall or reverse proxy; the API has API-key
authentication but terminates no TLS of its own.

Inspect state with:

```bash
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/metrics
mxhook logs --api-key "$MXHOOK_API_KEY"
curl -fsS -H "X-API-Key: $MXHOOK_API_KEY" http://127.0.0.1:8080/dlq
```

Replay a terminal delivery only after the receiver or network fault is fixed:

```bash
mxhook replay MESSAGE_ID --api-key "$MXHOOK_API_KEY"
```

## Graceful shutdown

Send `SIGTERM`. MXHook stops HTTP and SMTP acceptance, waits for in-flight SMTP
durable enqueue, cancels workers, and leaves unacknowledged queue leases and
outbox claims recoverable on restart. Container stop timeout must be at least
`MXHOOK_SHUTDOWN_GRACE_PERIOD`.

## SQLite backup and restore

The raw store and database form one recovery set. Safest backup procedure:

1. Stop MXHook gracefully.
2. Copy `/data/mxhook.db`, any `-wal` and `-shm` siblings, and `/data/raw/` to a
   versioned backup directory.
3. Record file sizes and SHA-256 checksums.
4. Restart MXHook and verify `/readyz`.
5. Replicate the backup off-host with encryption and retention.

Restore drill:

1. Start an isolated host with the same state encryption key.
2. Restore the complete recovery set with owner UID/GID and modes preserved.
3. Start the same MXHook version; verify readiness, list routes/messages, and
   deliver a controlled message.
4. Record recovery time and newest restored message timestamp.

Never copy only the SQLite main file while the process is running. If online
backups are required, use SQLite's backup API through an operational backup
tool and snapshot the raw directory at the same recovery point.

## Postgres backup and restore

Use `pg_dump --format=custom` plus the raw directory backup. Restore with
`pg_restore` into an isolated database, supply the same encryption key, then run
the controlled-message validation. Database and raw-store snapshots must share
one recorded recovery point.

## Upgrade and rollback

1. Back up database, raw storage, configuration, and state encryption key.
2. Validate the new configuration and image in an isolated restore.
3. Stop the old process gracefully.
4. Start the new image with migrations enabled and wait for readiness.
5. Send and verify one controlled message before changing traffic.

Schemas are forward-only and MXHook intentionally provides no compatibility
mode. Rollback means restoring the pre-upgrade database/raw snapshot and running
the prior image. Never run an older binary against a database opened by a newer
schema version.

## Versioned envelope and snake_case payload migration

The queue envelope and the webhook payload are now versioned:

- Queue envelope: `schema_version: "mxhook.message.v1"` (`queue/queue.go`).
- Webhook payload: `schema_version: "mxhook.email.v1"` (`types/email.go`),
  serialized in snake_case.

Receivers and consumers should switch on `schema_version` and alert on values
they do not recognize instead of guessing.

### Webhook payload field renames

Pre-v1 payloads serialized Go field names. The v1 payload renames every key:

| Go field | Legacy JSON key | v1 JSON key |
|---|---|---|
| `SchemaVersion` | — (new) | `schema_version` |
| `ID` | `ID` | `id` |
| `TraceID` | — (new) | `trace_id` |
| `MessageID` | `MessageID` | `message_id` (omitted when empty) |
| `ReceivedAt` | `ReceivedAt` | `received_at` |
| `EnvelopeFrom` | `EnvelopeFrom` | `envelope_from` |
| `EnvelopeTo` | `EnvelopeTo` | `envelope_to` |
| `Headers` | `Headers` | `headers` |
| `Subject` | `Subject` | `subject` |
| `TextBody` | `TextBody` | `text_body` (omitted when empty) |
| `HTMLBody` | `HTMLBody` | `html_body` (omitted when empty) |
| `RawPath` | `RawPath` | — removed: server-side path, never for receivers |
| `Attachments` | `Attachments` | `attachments` |
| `Attachment.Filename` | `Filename` | `filename` |
| `Attachment.ContentType` | `ContentType` | `content_type` |
| `Attachment.Size` | `Size` | `size` |
| `Attachment.StoragePath` | `StoragePath` | — removed: server-side path, never for receivers |

`RawPath` and `StoragePath` exposed internal storage locations; their removal
is deliberate and permanent. Receivers that unmarshal legacy keys read zero
values from a v1 payload — update parsers before upgrading the sender.

### Self-hosted durable queue: drain, then deploy

Pre-v1 durable queue rows used a different SQL shape (`id`, `body`, `meta`
columns) that the current backends cannot read; there is no decode-level
compatibility path. Drain before deploying:

1. Stop ingress (SMTP and Maildir) while leaving workers running, or scale
   `edge` roles to zero.
2. Wait for the queue and outbox to empty: `mxhook_queue` depth on
   `/metrics`, then `/readyz` green with no pending deliveries.
3. Stop the process gracefully and take the standard backup.
4. Deploy the new image with migrations enabled.
5. Re-enable ingress after the controlled-message check.

### Cloudflare worker and edge publishers

The worker queue consumer accepts both the `mxhook.message.v1` envelope and
the legacy (unversioned) edge event for one release, so the in-flight backlog
drains instead of dead-lettering. Upgrade order:

1. Deploy the worker (dual-format consumer).
2. Upgrade edge publishers to emit `mxhook.message.v1`.
3. After the legacy backlog drains and all publishers emit v1, remove the
   legacy acceptance path in the following release.

## Incidents

### Database unavailable

Readiness turns red. SMTP returns temporary failures when durable enqueue is
unavailable; Maildir claims remain in `cur/`. Restore database availability,
then confirm queue and outbox progress before replaying anything.

### Webhook storm or receiver outage

Delivery retries use durable jittered backoff. Keep the process running, repair
the receiver, and monitor retries/dead state. Do not bulk replay until the
receiver can sustain the backlog.

### Disk pressure

Stop new traffic or disable provider forwarding, then back up and expand the
data volume. Do not delete raw messages or database files manually. Retention
purge is not implicit because it would destroy accepted mail.

### Credential compromise

Rotate the API key at the deployment boundary and restart. Create replacement
routes with new webhook secrets, verify delivery, then disable old routes. State
encryption-key rotation requires an export/re-encryption procedure; if the key
is exposed, isolate the database and redeploy from a controlled backup.

### Maildir backlog

Inspect both `new/` and `cur/`. `new/` indicates unclaimed provider delivery;
`cur/` indicates claimed work awaiting durable enqueue. Fix database or disk
faults before changing files. Never move or delete claims by hand while MXHook
holds the Maildir lock.
