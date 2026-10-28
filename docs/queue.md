# Durable Queue Contract

MXHook uses SQLite by default and supports Postgres as an explicit alternative.
Both adapters implement the same ingress and production-consumption semantics.
A third backend, Cloudflare Queues, is publish-only and honors a weaker
contract; see "Cloudflare backend" below.

## Ingress

`queue.Enqueuer` persists raw messages by stable ID. Re-enqueuing the same ID,
body, and metadata succeeds without creating another record. Reusing an ID for
different content returns `queue.ErrIDCollision`.

Duplicate detection is canonical rather than byte-exact: the stored payload is
decoded and compared on raw digest, size, and envelope metadata. A change to
the encoder or to the storage representation therefore never turns a legitimate
redelivery into a collision.

## Production consumption

Workers use the pull-style `queue.Dequeuer` interface:

1. `Claim` blocks until work is available or the context ends.
2. A claim returns the message, opaque receipt, lease deadline, and attempt
   number.
3. `Ack`, `Nack`, and `Extend` require both message ID and receipt.
4. An expired claim becomes eligible for redelivery with a new receipt and an
   incremented attempt number.
5. A stale worker cannot acknowledge, reject, or extend a newer claim;
   `queue.ErrLeaseLost` is returned.
6. Repeating a successful acknowledgement or terminal nack with the same
   receipt is safe.
7. A successful acknowledgement deletes the queue row: the accepted message
   already lives in the raw store and event tables, so the queue retains only
   pending, processing, and nacked work. Nacked rows are kept for the control
   API.

The default visibility timeout is 30 seconds and the empty-queue poll interval
is 500 milliseconds. Library users can set both through each backend's
`NewWithConfig` constructor. Long-running handlers must extend their lease before
the deadline.

`Nack` is terminal for the ingress queue and records the cause in the row's
`failure_reason` column. The intake worker first persists terminal failure
details for the control API. Successful parsing transactionally creates event
and delivery-outbox rows before the queue item is acknowledged.

The lease deadline returned by `Claim` and `Extend` comes from the process
clock, not the database clock, even though the database enforces expiry with
its own clock. Under skew between the two the returned deadline is the
conservative one, so a renewal scheduled from it never fires after the
database-side lease has already lapsed.

Webhook delivery has a separate durable lease and retry schedule. Network
failures, `408`, `429`, and `5xx` responses retry with jittered backoff. A
terminal response or exhausted attempt budget moves the delivery to dead state;
replay advances its generation and schedules it again.

## Cloudflare backend

`queue.backend: cloudflare` publishes to a Cloudflare Queue over the HTTP API.
It is an `Enqueuer` only: it implements no `Dequeuer`, so a process configured
with it cannot run the `worker` or `all` role, and consumption belongs to a
Cloudflare Worker outside this codebase.

It does not honor the idempotency contract above. The HTTP API offers no
publish-time deduplication by message ID, so MXHook cannot detect that an ID
was already published:

- A retried enqueue — after a timeout, a network error, or an ambiguous
  response — publishes the message a second time.
- Reusing an ID for different content is accepted silently;
  `queue.ErrIDCollision` is never returned.

Consumers of a Cloudflare Queue must therefore deduplicate themselves on the
message `id` field, and must be idempotent with respect to redelivery. Choose
SQLite or Postgres where exactly-once ingress semantics matter.

Publishing is also capped at the Cloudflare message-size limit; oversized
encoded messages are rejected locally before the request is sent. A non-2xx
response is reported with the API's own error body included.

## Schema lifecycle

Both adapters apply idempotent queue-schema DDL at startup only when migrations
are enabled (the `database.migrate` setting, on by default); with migrations
disabled they verify the schema exists and fail fast otherwise. Operational
state uses a schema version gate and refuses unknown/newer versions. Migrations
are forward-only; there is no compatibility mode. Back up the database and raw
store before upgrading. Rollback restores that pre-upgrade recovery set.

Startup executes schema DDL only when migrations are enabled, so the migration
identity needs schema privileges. After initialization, operators may run
steady-state roles with migrations disabled and a reduced data-access identity.
