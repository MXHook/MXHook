# Glossary

Terms used across MXHook's documentation, configuration, and code. Each entry
names where the term is defined or enforced in source, so a definition here can
be checked rather than trusted.

Entries are grouped by subsystem. Terms whose meaning lives only in product
prose, with no code that enforces it, say so explicitly.

## Index

[AAD binding](#aad-binding) ·
[Accepted recipient domain](#accepted-recipient-domain) ·
[Ack](#ack) ·
[AdoptExisting](#adoptexisting) ·
[Attempt](#attempt) ·
[Attempt budget](#attempt-budget) ·
[At-least-once](#at-least-once) ·
[Backoff](#backoff) ·
[Catch-all](#catch-all) ·
[Claim (queue)](#claim-queue) ·
[Claim (Maildir)](#claim-maildir) ·
[Cloudflare Queue consumer](#cloudflare-queue-consumer) ·
[Control plane](#control-plane) ·
[cur/](#maildir-new-cur-tmp) ·
[D1](#d1) ·
[Dead delivery](#dead-delivery) ·
[Delivery](#delivery) ·
[DLQ](#dlq) ·
[Domain verification TXT record](#domain-verification-txt-record) ·
[Edge role](#runtime-role) ·
[Egress](#ingress-and-egress) ·
[Envelope recipient](#envelope-recipient) ·
[envelope_from / envelope_to](#envelope_from--envelope_to) ·
[ErrIDCollision](#erridcollision) ·
[ErrLeaseLost](#errleaselost) ·
[Event](#event) ·
[Extend / renew](#extend--renew) ·
[flock ownership](#flock-ownership) ·
[Header recipient](#header-recipient) ·
[HMAC signature](#hmac-signature) ·
[Idempotency key](#deterministic-message-id) ·
[Ingestion path](#ingestion-path) ·
[Ingress](#ingress-and-egress) ·
[Ingress failure](#ingress-failure) ·
[Jitter](#backoff) ·
[Lease](#lease) ·
[Lease expiry](#lease-expiry) ·
[Lease token / receipt](#receipt) ·
[Liveness](#liveness) ·
[MaxAttempts](#attempt-budget) ·
[Message](#message) ·
[mxhook.message.v1](#mxhookmessagev1) ·
[MX target hostname](#mx-target-hostname) ·
[Nack](#nack) ·
[new/](#maildir-new-cur-tmp) ·
[No-replace rename](#no-replace-rename) ·
[Outbox](#outbox) ·
[Permanent error](#temporary-vs-permanent-error) ·
[Plus-stripping](#plus-stripping) ·
[Quarantine](#quarantine) ·
[R2](#r2) ·
[RCPT TO](#rcpt-to) ·
[Readiness](#readiness) ·
[Recipient case-folding](#recipient-case-folding) ·
[Recipient header trust boundary](#recipient-header-trust-boundary) ·
[Recipient pattern](#recipient-pattern) ·
[Receipt fencing](#receipt-fencing) ·
[Redelivery](#redelivery) ·
[Redirect re-validation](#redirect-re-validation) ·
[Replay](#replay) ·
[Replay generation](#replay-generation) ·
[require_tls](#require_tls) ·
[Retention (queue)](#queue-retention) ·
[Retention (state)](#state-retention) ·
[Retention janitor](#retention-janitor) ·
[Route](#route) ·
[Runtime role](#runtime-role) ·
[schema_version](#schema_version) ·
[Sealed secret](#sealed-secret) ·
[Signing scheme](#signing-scheme) ·
[SSRF filtering](#ssrf-filtering) ·
[STARTTLS](#starttls-vs-implicit-tls) ·
[State encryption key](#state-encryption-key) ·
[Temporary error](#temporary-vs-permanent-error) ·
[Tenant](#tenant) ·
[Tenant domain](#tenant-domain) ·
[TLS hot-reload](#tls-hot-reload) ·
[tmp/](#maildir-new-cur-tmp) ·
[Tolerance window](#tolerance-window) ·
[Webhook target policy](#webhook-target-policy) ·
[Worker role](#runtime-role)

---

## Naming and addressing

### MX target hostname

The server's own name, configured as `smtp.domain` / `--smtp-domain` /
`MXHOOK_SMTP_DOMAIN` (for example `mx.example.com`). It is announced in the
SMTP greeting and `EHLO` response, and it is the name an MX record points *to*.
Required whenever SMTP is enabled.

Defined at `smtp/server.go:23` (`Config.Domain`), validated at
`smtp/server.go:71`, and passed to the SMTP library at `smtp/server.go:123`.

**Easily confused with the [accepted recipient domain](#accepted-recipient-domain).**
This is the single most common cause of a failed first setup. The two are
configured independently, so nothing stops you from setting one where the other
belongs. Two consequences follow: the TLS certificate must cover the MX target
hostname, not the recipient domain (`docs/configuration.md`, "Getting a
certificate for your MX hostname"), and a recipient whose domain is not in
`allowed_domains` is rejected regardless of what `smtp.domain` says. See
[Pointing a domain at MXHook](dns-setup.md).

### Accepted recipient domain

A domain MXHook will accept mail *for* — the part after the `@` in addresses you
want delivered. Configured as `smtp.allowed_domains` / `--recipient-domain` /
`MXHOOK_SMTP_ALLOWED_DOMAINS` (for example `example.com`). At least one is
mandatory when SMTP is enabled.

Defined at `smtp/server.go:24` (`Config.AllowedDomains`); the "at least one"
rule is enforced at `smtp/server.go:74`. Values are normalized to lowercase with
a trailing dot trimmed at `smtp/server.go:85`, and each is rejected if it is
empty, contains `@`, or contains a comma or whitespace (`smtp/server.go:79`).
Enforcement happens per recipient in `session.isAllowedRecipient`
(`smtp/server.go:320`), which splits at the last `@` and asks the domain
resolver about the part after it (`smtp/server.go:333`).

A recipient on an unlisted domain is answered `550 5.1.1 recipient not
accepted` (`smtp/server.go:312`). When the durable domain registry cannot be
read and nothing else vouches for the recipient, the answer is instead `451
4.3.0` so the sender retries rather than bouncing (`smtp/server.go:309`).

Synonyms: recipient domain, allowed domain.

### RCPT TO

The SMTP command naming one envelope recipient. MXHook's handler for it is
`session.Rcpt` (`smtp/server.go:302`); accepted addresses accumulate on the
session at `smtp/server.go:314` and become the message's `EnvelopeTo` when
`DATA` completes (`smtp/server.go:371`).

### Envelope recipient

The recipient as given by the transport, independent of anything written in the
message headers. On the SMTP path it is authoritative, taken directly from
`RCPT TO`. On the Maildir path there is no envelope, so it is reconstructed from
a header — see [recipient header trust boundary](#recipient-header-trust-boundary).

Carried on the queue message as `EnvelopeTo` (`queue/queue.go:30`) and on the
parsed event as `envelope_to` (`types/email.go:16`).

### envelope_from / envelope_to

The two envelope fields on the wire contracts. `envelope_from` is the sender
(SMTP `MAIL FROM`, or `Return-Path` on the Maildir path); `envelope_to` is the
ordered list of accepted recipients.

On the ingress envelope: `queue/queue.go:29` and `queue/queue.go:30`. On the
webhook payload: `types/email.go:14` and `types/email.go:15`. Validation
requires at least one recipient and rejects an empty one
(`queue/queue.go:65`, `queue/queue.go:76`); `envelope_from` may legitimately be
empty, which is the null sender.

### Header recipient

An address appearing in the message's own headers (`To`, `Cc`,
`Delivered-To`, …). Routing does **not** use these on the SMTP path; it matches
the envelope recipients (`internal/runtime/intake.go:88`). The EE advanced
matcher can additionally condition on arbitrary headers
(`router/advanced.go:14`, matched at `router/advanced.go:75`).

**Easily confused with the [envelope recipient](#envelope-recipient).** A
message can be addressed to one recipient in its headers and delivered to a
different one by the envelope; MXHook routes on the envelope. The exception is
Maildir, where a header *is* the recipient source by necessity.

---

## Pipeline and entities

### Ingress and egress

Ingress is everything up to the durable queue insert: an SMTP conversation or a
Maildir scan producing a validated [`mxhook.message.v1`](#mxhookmessagev1)
envelope. Egress is webhook delivery out to a receiver. The queue is the seam:
an ingress source reports success only after the durable insert
(`smtp/server.go:372`), and delivery is driven separately by the delivery worker
(`internal/runtime/delivery_worker.go:61`).

Terminal ingress failures are recorded separately from delivery failures — see
[ingress failure](#ingress-failure).

### Ingestion path

One of the three ways mail enters MXHook. Only the first two are part of the
self-hosted production release.

| Path | Enabled by | Envelope recipient source |
|---|---|---|
| Own MX (SMTP) | `--smtp-addr` | `RCPT TO`, authoritative (`smtp/server.go:314`) |
| Maildir | `--maildir <root>` | a header, default `Delivered-To` (`maildir/ingester.go:26`) |
| Cloudflare worker | `--queue-backend cloudflare` plus a separately deployed consumer | carried in the ingress envelope |

The third is experimental and belongs to the hosted-service work in `worker/`;
see [architecture](architecture.md) and [tenant](#tenant). SMTP and Maildir can
both run in one process, and the `all` and `edge` roles refuse to start with
neither (`cli/start.go:459`).

### Message

One received email as it enters the pipeline: the versioned ingress envelope
holding the raw RFC 5322 bytes plus transport metadata. Defined as
`queue.Message` at `queue/queue.go:22`, constructed by `queue.NewMessage`
(`queue/queue.go:36`), and validated before any backend persists it
(`queue/queue.go:53`).

Once parsed, a message becomes an [event](#event); each route it matched
becomes a [delivery](#delivery).

### Event

The structured, parsed form of a message: subject, headers, text and HTML
bodies, attachment metadata. Defined as `types.EmailEvent` at
`types/email.go:8`, produced by the parser and committed to
`mxhook_events` at `internal/state/store.go:272`. This is the object serialized
into the webhook payload (`webhook/delivery.go:109`).

### Delivery

One (event, route) pair with its own durable state, attempt counter, lease, and
retry schedule. Rows live in `mxhook_deliveries`, created transactionally
alongside the event at `internal/state/store.go:286` — one per matched
webhook route. Its ID is deterministic: `sha256(messageID + "\x00" + routeID)`,
prefixed `delivery:` (`internal/state/store.go:320`), and it is sent to
receivers as `X-MXHook-Delivery-ID` (`webhook/delivery.go:152`).

Delivery states are `pending`, `processing`, `retry`, `succeeded`, and `dead`
(`internal/state/store.go:355`, `:430`, `:434`, `:438`).

### Attempt

One try at delivering a delivery. `mxhook_deliveries.attempt_count` increments
on each claim (`internal/state/store.go:387`), and every finished attempt is
appended to `mxhook_delivery_attempts` keyed by
`(delivery_id, replay_generation, attempt_no)` (`internal/state/store.go:478`).

**Message vs delivery vs attempt vs event, in one line:** one *message* arrives,
is parsed into one *event*, fans out to one *delivery* per matched route, and
each delivery accumulates many *attempts*. The relationship is 1 → 1 → N → N.

Note that `webhook.Deliverer.Deliver` also loops internally up to
`MaxAttempts` (`webhook/delivery.go:116`) — those in-process tries are distinct
from the durable attempt rows the delivery worker records, and both are bounded
by their own `MaxAttempts` setting.

### Route

A rule mapping a recipient pattern to a destination. Defined as `router.Rule`
(`router/rule.go:13`) with targets `webhook`, `queue`, or `storage`
(`router/rule.go:7`). Persisted encrypted in `mxhook_routes`
(`internal/state/store.go:214`).

Rules are ordered by `priority DESC` then creation time
(`internal/state/store.go:233`), and the first pattern that matches wins
(`router/matcher.go:18`). A statically configured webhook route is given
priority `-1000` so API-created routes override it (`cli/start.go:487`).

A route snapshots its URL and encrypted secret into each delivery row at commit
time (`internal/state/store.go:286`), so later configuration changes cannot
mutate a delivery already in flight.

### Recipient pattern

The matching expression on a route. Three forms are supported
(`router/matcher.go:46`):

| Pattern | Matches |
|---|---|
| `user@example.com` | that exact address, after normalization |
| `*@example.com` | any address on that domain |
| `*` | any address at all — a [catch-all](#catch-all) |

Matching is done against a *normalized* recipient, so exact patterns also catch
plus-addressed and differently-cased variants. See
[plus-stripping](#plus-stripping) and [recipient case-folding](#recipient-case-folding).

### Catch-all

A route matching every recipient. In matching terms this is the `*` pattern
(`router/matcher.go:46`). `router.Rule` also carries an `IsCatchAll` flag
(`router/rule.go:20`) that is persisted and returned by the API
(`internal/state/store.go:215`), but the matcher does not read it — matching
depends only on the pattern string. The configured static webhook route is
created with pattern `*` at `cli/start.go:487`.

### Plus-stripping

Discarding everything from the first `+` in the local part before matching, so
`task+label@domain.com` matches a route for `task@domain.com`. Implemented in
`normalizeRecipient` at `router/matcher.go:33`, stripping at `router/matcher.go:40`.

Only the *match* is normalized. Normalization happens inside the matcher and is
not written back, so the recipient carried into the payload is the original,
unmodified one (`internal/runtime/intake.go:89` passes the raw recipient to
`Match`; `parser/parser.go:32` copies the envelope list through untouched).

### Recipient case-folding

Lowercasing the whole address before matching (`router/matcher.go:34`). This
folds the local part too, which RFC 5321 permits to be case-sensitive; the
code documents this as a deliberate policy, since stored patterns are
lowercased and both sides must agree (`router/matcher.go:29`).

Separately, recipient *domains* are lowercased and stripped of a trailing dot
when `allowed_domains` is normalized at `smtp/server.go:85`.

---

## Queue and delivery mechanics

### Outbox

The transactional pattern by which a parsed event and its delivery rows are
written in one database transaction, before the queue item is acknowledged
(`internal/state/store.go:267`–`:295`). It guarantees no event is committed
without its deliveries, and a crash before the ack simply replays the message.
The interface is `runtime.EventOutbox` (`internal/runtime/intake.go:17`).

### Claim (queue)

Taking a message off the durable queue for processing. `Dequeuer.Claim`
(`queue/queue.go:158`) blocks until work exists and returns a
`queue.Delivery` — the message, an opaque [receipt](#receipt), the lease
deadline, and the attempt number (`queue/queue.go:148`). The claim is pull-based
so a lease does not start before a worker is ready.

**Note the name collision:** `queue.Delivery` is a *leased claim on an ingress
message*, unrelated to the webhook [delivery](#delivery) in `internal/state`.
Two different types, two different lifecycles.

### Lease

The time-bounded exclusive right to work on a claimed item. Both leases default
to 30 seconds — the ingress queue's at `queue/sqlite/adapter.go:24`, the state
store's delivery lease at `internal/state/store.go:73`. A lease is recorded as
`locked_until` on the row (`internal/state/store.go:388`).

The lease deadline returned to the caller comes from the process clock even
though the database enforces expiry with its own clock — under skew the
returned deadline is the conservative one, so a renewal scheduled from it never
fires late (see [queue contract](queue.md)).

### Receipt

The opaque 128-bit handle identifying one specific claim, minted by
`queue.NewReceiptHandle` (`queue/queue.go:165`). Stored on the delivery row as
`lease_token` (`internal/state/store.go:388`) and required by `Ack`, `Nack`, and
`Extend` (`queue/queue.go:159`–`161`).

Synonyms: lease token, receipt handle.

**Easily confused with the [lease](#lease) itself.** The lease is the *deadline*
— how long you hold the item. The receipt is the *identity* of your particular
hold. A new claim of the same row after expiry gets a fresh receipt while the
row's identity is unchanged; that is precisely what makes
[fencing](#receipt-fencing) work.

### Receipt fencing

Requiring the receipt to match on every state transition, so a worker whose
lease already expired and was reclaimed elsewhere cannot ack, nack, or extend
the newer claim. Enforced in SQL by including `lease_token = ?` in the `WHERE`
clause (`internal/state/store.go:419`, `:490`); a zero-row update becomes
[`ErrLeaseLost`](#errleaselost) (`internal/state/store.go:515`).

### Lease expiry

The moment `locked_until` passes. An expired `processing` row becomes claimable
again by the same query that picks up `pending` and `retry` rows
(`internal/state/store.go:356`), which is what produces
[redelivery](#redelivery).

### Extend / renew

Pushing a lease deadline out so long-running work is not reclaimed mid-flight.
`Dequeuer.Extend` (`queue/queue.go:161`) and `Store.ExtendDelivery`
(`internal/state/store.go:411`). Both workers renew on a background goroutine
that wakes at half the remaining lease (`internal/runtime/worker.go:182`,
`internal/runtime/delivery_worker.go:145`); a failed renewal cancels the
in-flight work rather than letting two workers proceed
(`internal/runtime/worker.go:202`).

### Ack

Acknowledging successful processing. `Dequeuer.Ack` (`queue/queue.go:159`),
called by the intake worker only after the outbox transaction commits
(`internal/runtime/worker.go:144`). A successful ack deletes the queue row —
see [queue retention](#queue-retention).

### Nack

Rejecting a message terminally. `Dequeuer.Nack` (`queue/queue.go:160`) records
the cause in the row's `failure_reason` column. The intake worker nacks when the
error is permanent or the attempt budget is spent, and persists an
[ingress failure](#ingress-failure) first so the control API can report it
(`internal/runtime/worker.go:159`–`167`).

Nack is **terminal for the ingress queue** — it is not a "retry later" signal.
Ingress retries happen by letting the lease expire instead
(`internal/runtime/worker.go:170`).

### Redelivery

A previously claimed item becoming claimable again after its lease expires,
with a new receipt and an incremented attempt count. This is the ingress retry
mechanism: on a non-permanent failure within budget, the intake worker
deliberately does nothing and logs that the delivery will retry after lease
expiry (`internal/runtime/worker.go:170`).

### At-least-once

The delivery guarantee: a message may be processed, and a webhook may be
delivered, more than once. Stated in [architecture](architecture.md); receivers
must deduplicate on `X-MXHook-Delivery-ID` (`webhook/delivery.go:152`). The
Maildir ingester documents its own at-least-once window at
`maildir/ingester.go:369`.

### Deterministic message ID

An ID derived from stable inputs so a retried enqueue of the same source
material lands on the same row instead of duplicating. The Maildir path derives
it as `maildir:<sha256(idNamespace + "\x00" + uniqueName)>`
(`maildir/ingester.go:441`), which makes rescanning a file after a crash a
no-op. The SMTP path instead uses a fresh random handle
(`smtp/server.go:371`), since an SMTP transaction that was not acknowledged is
retried by the *sender* as a new transaction.

Delivery IDs are deterministic on `(messageID, routeID)`
(`internal/state/store.go:320`), which is what makes redelivered messages fan
out to the same delivery rows.

Synonyms: idempotency key.

### ErrIDCollision

`queue.ErrIDCollision` (`queue/queue.go:15`), returned when an ID is reused for
*different* content. Re-enqueuing the same ID with the same content succeeds as
a no-op; the comparison is canonical rather than byte-exact, done on raw
digest, size, and envelope metadata (`queue.SameContent`, `queue/queue.go:99`),
so an encoder or storage-representation change never masquerades as a
collision.

The Maildir ingester treats a collision as unretryable and
[quarantines](#quarantine) the file (`maildir/ingester.go:358`).

The Cloudflare backend does **not** honor this contract at all — the HTTP API
offers no publish-time dedupe, so `ErrIDCollision` is never returned there. See
[queue contract](queue.md).

### ErrLeaseLost

Two distinct sentinels with the same name and the same meaning in their own
layer:

- `queue.ErrLeaseLost` (`queue/queue.go:16`) — the ingress queue lease was lost.
- `state.ErrLeaseLost` (`internal/state/store.go:36`) — the delivery lease was
  lost, produced by `requireOne` when a fenced update matches no row
  (`internal/state/store.go:515`).

Both are treated as "someone else owns this now; move on" by their workers
(`internal/runtime/worker.go:111`, `internal/runtime/delivery_worker.go:83`).

### Attempt budget

The cap on how many attempts an item gets before it is treated as terminal.
Three separate budgets exist, each with its own default:

| Budget | Default | Defined at |
|---|---|---|
| Intake worker, per message | 5 | `internal/runtime/worker.go:75` |
| Delivery worker, per delivery | 8 | `internal/runtime/delivery_worker.go:44` |
| `webhook.Deliverer` in-process retry loop | 3 | `webhook/delivery.go:57` |

Synonym: `MaxAttempts`. Exhausting the delivery-worker budget moves the delivery
to [dead](#dead-delivery) (`internal/runtime/delivery_worker.go:126`);
exhausting the intake budget [nacks](#nack) the message
(`internal/runtime/worker.go:159`).

### Backoff

The growing wait between retries. The delivery worker doubles from `BaseRetry`
(default 5s) toward `MaxRetry` (default 1h) — `internal/runtime/delivery_worker.go:173`
— and applies **jitter** of ±20% drawn from `crypto/rand`
(`internal/runtime/delivery_worker.go:181`) to break up synchronized retry
storms. Worker run loops use a separate backoff for transient store faults:
100ms doubling to a 5s cap (`internal/runtime/worker.go:42`), giving up after
10 consecutive failures (`internal/runtime/worker.go:39`).

### Temporary vs permanent error

The classification that decides retry versus terminal. In the webhook layer,
network failures, `408`, `429`, and any `5xx` are temporary; every other non-2xx
is permanent (`webhook/delivery.go:171`). DNS resolution failures are
deliberately classed temporary so a resolver blip retries rather than
dead-letters (`webhook/target.go:104`). Tested with `webhook.IsTemporary`
(`webhook/delivery.go:186`).

On the ingress side, parse failures are wrapped `Permanent`
(`internal/runtime/intake.go:76`), as is a message matching no route
(`internal/runtime/intake.go:103`).

### Dead delivery

A delivery in the terminal `dead` state, reached on a permanent error or an
exhausted attempt budget (`internal/runtime/delivery_worker.go:126`, set by
`Store.DeadDelivery` at `internal/state/store.go:438`). Dead deliveries are
listed by `GET /dlq` (`internal/state/api_repository.go:207`) and are never
deleted by retention (`internal/state/retention.go:99` sweeps only `succeeded`).

### DLQ

The dead-letter queue: the `GET /dlq` view over terminal failures
(`api/server.go:152`). It is a *union* of two different things
(`internal/state/api_repository.go:209`): dead deliveries, and ingress failures
synthesized with an `ingress:` ID prefix (`internal/state/api_repository.go:214`).

`DELETE /dlq/{id}` (`api/server.go:153`) is asymmetric, and the `ingress:`
prefix is what the handler switches on (`api/server.go:217`): an ingress failure
is deleted and answered `204`, while a dead delivery is refused `405` — they are
retained deliberately, and the fix is a [replay](#replay) instead
(`api/server.go:226`).

**Easily confused with [quarantine](#quarantine) and [nack](#nack).** They are
three different mechanisms at three different layers: *nack* is the queue-level
act of rejecting a message terminally; *DLQ* is the read view over what ended
terminally, in the database; *quarantine* is a filesystem move of a bad Maildir
file, which never reaches the database at all.

### Ingress failure

A message that failed intake terminally, recorded in `mxhook_ingress_failures`
(`internal/state/store.go:301`) before the queue nack, so the control API can
report a cause the queue row no longer holds. Cleared if a later redelivery
succeeds (`internal/state/store.go:313`, called at
`internal/runtime/worker.go:152`). Surfaced in the DLQ under an `ingress:` ID.

### Replay

Re-scheduling a terminal delivery. `POST /replay/{id}` (`api/server.go:162`)
calls `Store.ScheduleReplay` (`internal/state/api_repository.go:245`), which
targets rows in state `dead` for that message
(`internal/state/api_repository.go:250`), resets `attempt_count` to 0, and
schedules the next attempt.

### Replay generation

The counter incremented on each replay (`internal/state/api_repository.go:248`),
kept on `mxhook_deliveries.replay_generation`. It is part of the attempt-history
key `(delivery_id, replay_generation, attempt_no)`
(`internal/state/store.go:480`), so replaying does not collide with or erase the
attempts of the previous generation.

### Queue retention

The queue keeps only pending, processing, and nacked work: a successful ack
deletes the row outright, because the accepted message already lives in the raw
store and event tables. Nacked rows are kept for the control API. See
[queue contract](queue.md). This behavior is unconditional and is not affected
by the `retention.*` settings.

### State retention

The opposite side: `retention.ttl` (`MXHOOK_RETENTION_TTL`, default `0` =
disabled) bounds growth of *terminal state* by deleting rows older than the TTL.
Configured via `state.RetentionConfig` (`internal/state/retention.go:23`).

**Easily confused with [queue retention](#queue-retention).** Queue retention is
automatic row deletion on ack. State retention is an opt-in, TTL-driven sweep of
the events, deliveries, and raw files left behind afterwards. Turning state
retention off does not make the queue grow; leaving it off makes processed
*state* grow forever.

### Retention janitor

The goroutine that performs state retention, `state.Janitor`
(`internal/state/retention.go:36`), sweeping on `retention.interval` (default
1h — `internal/state/retention.go:31`). One sweep deletes, in order: succeeded
deliveries past the TTL (`internal/state/retention.go:98`), events no delivery
references any more together with their raw files
(`internal/state/retention.go:134`), and expired ingress-failure rows
(`internal/state/retention.go:119`). Dead deliveries and the events they
reference are always kept so DLQ listing and replay keep working
(`internal/state/retention.go:133`). Sweep errors are logged and retried on the
next tick; they never stop the janitor (`internal/state/retention.go:80`).

---

## Delivery and security

### Webhook target policy

The rules a webhook URL must satisfy, implemented as `targetPolicy`
(`webhook/target.go:14`). A target must have a host, carry no user information,
and use `https` — `http` only if `webhook.allow_http` is set
(`webhook/target.go:29`–`44`). Every resolved address is then checked against
the [SSRF filter](#ssrf-filtering).

### SSRF filtering

Rejecting webhook targets that resolve to addresses which are not publicly
routable. `unsafeIP` (`webhook/target.go:168`) rejects unspecified, loopback,
private, link-local, and multicast addresses, plus an explicit list of
special-purpose ranges — CGNAT `100.64.0.0/10`, the RFC 5737 documentation
blocks, RFC 2544 benchmarking, reserved `240.0.0.0/4`, and IPv6 equivalents
(`webhook/target.go:144`). Disabled by `webhook.allow_private_network`.

### Dial-time IP pinning

Re-resolving the host inside the transport's `DialContext` and dialing *only*
addresses the policy just vetted (`webhook/target.go:52`–`80`). This closes the
DNS-rebinding window between validation and connection: a name that passed
validation cannot be re-pointed at a private address before the socket opens.

### Redirect re-validation

Running the same policy against every redirect hop, with a cap of 5
(`webhook/delivery.go:67`–`72`). Without this, a public URL could redirect a
delivery to an internal one.

### HMAC signature

The `X-MXHook-Signature` header authenticating a webhook payload:
`sha256=<hex>` of an HMAC-SHA256 keyed by the route's webhook secret
(`webhook/signer.go:33`). Verified in constant time
(`webhook/signer.go:76`).

### Signing scheme

MXHook has two, and which one applies depends on whether a timestamp header is
present:

- **Timestamped (current).** The MAC covers `"{unix_ts}.{payload}"`
  (`webhook/signer.go:33`), with the timestamp sent as `X-MXHook-Timestamp`
  (`webhook/delivery.go:149`). Verified with
  `webhook.VerifyTimestampedSignature` (`webhook/signer.go:56`). Every attempt
  is signed afresh so retries carry a current timestamp
  (`webhook/delivery.go:119`).
- **Legacy payload-only.** The MAC covers the payload alone
  (`webhook/signer.go:23`), verified with `webhook.VerifySignature`
  (`webhook/signer.go:46`). It carries no freshness field, so a captured request
  verifies forever. It exists only for receivers fed by senders predating
  timestamped signing.

### Tolerance window

The accepted clock skew between signing and verification, bounding the replay
window for timestamped signatures. Default 5 minutes
(`DefaultSignatureTolerance`, `webhook/signer.go:14`); a non-positive tolerance
falls back to it (`webhook/signer.go:57`). Skew is compared in absolute value,
so a far-future timestamp is rejected too (`webhook/signer.go:64`).

### STARTTLS vs implicit TLS

MXHook offers **STARTTLS** — an opportunistic upgrade on the SMTP port — and
does not offer implicit TLS on port 465. The TLS config is attached to the SMTP
server only (`smtp/server.go:133`), with `MinVersion: tls.VersionTLS12`
(`smtp/server.go:135`). Certificate and key must be configured together
(`smtp/server.go:94`).

### require_tls

`smtp.require_tls` / `--smtp-require-tls`. When set, a session that has not
negotiated TLS is refused at `MAIL FROM` with `530 5.7.0 STARTTLS is required`
(`smtp/server.go:295`). Configuring it without a certificate and key is a
startup error (`smtp/server.go:97`).

### TLS hot-reload

Resolving the SMTP certificate per handshake through a reloader
(`smtp/server.go:134`, `GetCertificate`) rather than loading it once at startup,
so a certificate replaced on disk by a renewal is picked up by the next
connection without a restart. Operational detail in
[configuration](configuration.md).

### State encryption key

`MXHOOK_STATE_ENCRYPTION_KEY` — base64 of exactly 32 random bytes, used to build
the AES-256-GCM AEAD that seals route webhook secrets
(`internal/state/store.go:68`, built at `internal/state/crypto.go:12`; the
32-byte length is enforced at `internal/state/crypto.go:13`). Required for the
`all` and `worker` roles
(`cli/start.go:445`). The key is never stored in the database; losing it makes
route secrets unrecoverable.

### Sealed secret

A webhook secret encrypted at rest. Sealed on write (`internal/state/store.go:209`
for routes, `:281` when snapshotting into a delivery row) and opened on read
(`internal/state/store.go:246`, `:374`). Only the sealed form is ever persisted,
and the API never returns a route secret after creation.

### AAD binding

Binding the ciphertext to an owner identifier as additional authenticated data,
so a sealed blob moved to another row fails to decrypt (`seal`,
`internal/state/crypto.go:25`). The owner is the route ID, both for route rows
(`internal/state/store.go:209`) and for the secret snapshotted into a delivery
row (`internal/state/store.go:281`, opened at `:374`).

One nuance: `openSecret` falls back to an unbound decrypt for secrets written
before owner binding existed (`internal/state/crypto.go:48`), upgrading those
rows whenever they are next written. So AAD binding is enforced for anything
written by current code, but a legacy row is still readable.

---

## Maildir

Full guide: [Maildir ingestion](maildir.md).

### Maildir new/, cur/, tmp/

The three standard Maildir directories. MXHook reads `new/` for unclaimed
messages, moves each into `cur/` to claim it, and requires `tmp/` to exist and be
private (`maildir/ingester.go:92`, `:195`). Operationally: files in `new/` mean
provider delivery MXHook has not claimed yet; files in `cur/` mean claimed work
awaiting durable enqueue.

MXHook also creates a fourth directory, `mxhook-failed/`, for
[quarantine](#quarantine) (`maildir/ingester.go:94`).

### Claim (Maildir)

Moving a file from `new/` to `cur/` to take exclusive responsibility for it
(`maildir/ingester.go:283`–`:287`). The claim is the atomic rename itself, so
two scanners cannot both claim the same file. After a successful enqueue the
claimed file is removed (`maildir/ingester.go:366`).

**Distinct from [claim (queue)](#claim-queue)** — same word, different layer.
The Maildir claim is a filesystem rename; the queue claim is a leased database
row.

### No-replace rename

A rename that fails rather than clobbering an existing destination. Used for
every claim and quarantine move (`renameNoReplace`, called at
`maildir/ingester.go:285` and `:454`), with per-platform implementations
(`maildir/rename_linux.go`, `maildir/rename_darwin.go`, `maildir/rename_bsd.go`,
and a POSIX fallback). This is what makes the claim atomic and safe against a
concurrent writer.

### flock ownership

An exclusive, non-blocking `flock` on `.mxhook.lock` inside the Maildir root,
taken at construction (`maildir/ingester.go:103`, implemented at
`maildir/platform_posix.go:60`). A second MXHook process on the same Maildir
fails to start with "already locked by another MXHook process"
(`maildir/platform_posix.go:69`).

Separately, a `.mxhook-owner` marker file records that MXHook owns the directory
(`maildir/ingester.go:27`, written at `maildir/ingester.go:225`).

### AdoptExisting

`--maildir-adopt-existing` / `MXHOOK_MAILDIR_ADOPT_EXISTING`, default false
(`maildir/ingester.go:39`). On a *first* start — when no ownership marker
exists — a non-empty `cur/` is refused unless this is set
(`maildir/ingester.go:216`). It guards against MXHook silently taking over
another mail agent's in-progress state.

### Quarantine

Moving a Maildir file that can never be processed into `mxhook-failed/`
(`maildir/ingester.go:453`), created mode `0700`
(`maildir/platform_posix.go:28`). Triggers: a non-regular file
(`maildir/ingester.go:276`, `:324`), a message over the size limit
(`maildir/ingester.go:332`), unparseable envelope metadata
(`maildir/ingester.go:340`), and [`ErrIDCollision`](#erridcollision)
(`maildir/ingester.go:359`). Name conflicts in the quarantine directory are
resolved with a timestamp-and-random suffix (`maildir/ingester.go:464`).

Synonym for the mechanism: poison-payload handling. See also
[DLQ](#dlq) for the distinction.

### Recipient header trust boundary

Maildir carries no envelope, so MXHook reconstructs the recipient from a header
— `--maildir-recipient-header`, default `Delivered-To`
(`maildir/ingester.go:26`, read at `maildir/ingester.go:400`) — falling back to
`--maildir-default-recipient` when that header is absent
(`maildir/ingester.go:402`). If neither yields an address the message is
quarantined (`maildir/ingester.go:340`). The envelope sender comes from
`Return-Path` (`maildir/ingester.go:411`), with the null sender `<>` mapping to
empty (`maildir/ingester.go:435`).

The boundary: **the local MTA writing the Maildir must be the same agent
injecting that header.** Any writer that can drop a file into `new/` can choose
which route the message matches. This is the one hard requirement of the Maildir
path, and it has no code enforcement — it is a deployment property.

---

## Operations and control plane

### Runtime role

Which parts of the pipeline a process runs, set by `--runtime-role` /
`runtime.role` / `MXHOOK_RUNTIME_ROLE`, default `all`. The set is exactly
`all`, `edge`, `worker` (`cli/start.go:48`), validated at `cli/start.go:458`;
anything else is a startup error (`cli/start.go:468`).

| Role | Ingress | Processing |
|---|---|---|
| `all` | SMTP and/or Maildir required | yes (`cli/start.go:142`) |
| `edge` | SMTP and/or Maildir required | no |
| `worker` | must be disabled (`cli/start.go:465`) | yes |

`all` and `edge` refuse to start with neither ingress enabled
(`cli/start.go:460`). `all` additionally refuses to start with an empty route
set, so it cannot accept and strand mail (`cli/start.go:478`). The API requires
`all` or `worker` with durable state (`cli/start.go:272`). Split roles need a
shared Postgres queue; SQLite is intended for one process.

### Liveness

Whether the process is running at all. Served by `GET /healthz`
(`api/server.go:146`), whose handler unconditionally returns
`{"status":"ok"}` without touching any dependency (`api/server.go:165`).

### Readiness

Whether the process can actually serve — specifically, whether the database
answers. Served by **both** `GET /readyz` and `GET /health`
(`api/server.go:145`, `:147`), which share one handler that pings the database
with a 2-second timeout and returns `503 {"status":"not_ready"}` on failure
(`api/server.go:169`–`178`).

**`/health` is readiness, not liveness — this is the trap.** The name suggests
the shallow check but the `z`-suffixed `/healthz` is the shallow one. Point
liveness probes at `/healthz` and readiness probes at `/readyz`; do not restart a
live process solely because readiness is red ([operations](operations.md)).

### Control plane

The authenticated HTTP API for domains, routes, messages, DLQ, and replay
(`api/server.go:152`–`162`), each guarded by a permission via `requirePerm`.
It fails closed: with no API key configured and no enterprise authorizer
attached, every control route answers `401`, while `/healthz`, `/readyz`,
`/health`, `/metrics`, and `/openapi.yaml` stay reachable for probes and scrapes
([configuration](configuration.md)).

### schema_version

The versioning field on both serialized contracts, so consumers can switch on it
instead of guessing:

- Queue envelope: `mxhook.message.v1` (`queue/queue.go:19`, field at
  `queue/queue.go:23`), rejected if unrecognized (`queue/queue.go:55`).
- Webhook payload: `mxhook.email.v1` (`types/email.go:5`, field at
  `types/email.go:9`).

Distinct from the *database* schema version, an integer gate on the state store
(`internal/state/store.go:34`) that refuses a database newer than the binary
supports (`internal/state/store.go:164`).

### mxhook.message.v1

The versioned ingress envelope shared by every queue backend and both ingestion
paths (`queue/queue.go:19`). It carries schema version, message and trace IDs,
receipt time, source and source ID, envelope sender and ordered recipients, the
raw content's SHA-256 and size, and the raw RFC 5322 bytes
(`queue/queue.go:22`). Every field is validated before persistence, including
re-computing the digest (`queue/queue.go:72`).

---

## Worker / SaaS (experimental)

Everything in this section belongs to the **experimental hosted-service work in
`worker/`** — a Cloudflare Worker, deployed separately, that is not part of the
self-hosted Go binary. See [architecture](architecture.md) for the boundary.

### Tenant

A customer account in the hosted service, with a plan of `free`, `pro`, or
`enterprise`. Defined only in D1: `worker/migrations/0001_initial.sql:4`.

**Tenancy exists only in `worker/`.** The Go OSS binary has no tenant concept at
all — there are zero occurrences of `tenant` anywhere in the Go source or tests.
A tenant may own one or more domains; every SaaS-side table
(`domains`, `routes`, `messages`) carries a `tenant_id` foreign key cascading
from `tenants` (`worker/migrations/0001_initial.sql:13`, `:25`, `:37`).

### Tenant domain

A domain registered by a tenant, held in the D1 `domains` table
(`worker/migrations/0001_initial.sql:11`) with a `verified` flag and a
`txt_record` verification token. Unrelated to the OSS binary's
[accepted recipient domain](#accepted-recipient-domain), which is process
configuration rather than a per-customer record.

### Domain verification TXT record

DNS proof that a tenant controls a domain. The convention is a TXT record at
`_mxhook-verify.<domain>` whose value equals the stored `txt_record` token
exactly (`worker/src/verify.ts:21`, `worker/src/verify.ts:50`). Any one matching
TXT record verifies; the apex is not consulted. Lookups go over DNS-over-HTTPS
because Workers have no `dns` module (`worker/src/verify.ts:24`).

Verification only ever transitions unverified → verified: any transport error,
non-200, malformed JSON, NXDOMAIN, or mismatch leaves the domain alone for the
next cron run, and one bad domain never aborts the batch
(`worker/src/verify.ts:184`). Runs are capped at 50 domains
(`worker/src/verify.ts:27`).

### D1

Cloudflare's SQLite database, bound as `DB` (`worker/wrangler.toml:27`). Holds
the SaaS-side tenants, domains, routes, messages, and delivery logs
(`worker/migrations/0001_initial.sql`). Entirely separate from the OSS binary's
SQLite or Postgres state store.

### R2

Cloudflare's object store, bound as `EMAIL_STORE` over bucket `mxhook-emails`
(`worker/wrangler.toml:35`). Raw messages are keyed `tenant_id/message_id`
(`worker/migrations/0001_initial.sql:44`). The OSS equivalent is the local
filesystem raw store in `storage/`.

### Cloudflare Queue consumer

The `queue` handler in `worker/src/index.ts` that drains batches from
`mxhook-email-events` (`worker/wrangler.toml:17`), with its own dead-letter
queue `mxhook-email-events-dlq` (`worker/wrangler.toml:21`).

Note the asymmetry: MXHook's `queue/cloudflare` adapter is an `Enqueuer` only —
it implements no `Dequeuer`, so a process configured with it cannot run the
`worker` or `all` role, and consumption belongs to this Worker outside the Go
codebase. It also does not honor the [`ErrIDCollision`](#erridcollision)
contract. See [queue contract](queue.md).

This is **not** Cloudflare Email Routing: there is no `email()` handler and no
`send_email` binding. Mail still arrives over SMTP on an edge node you operate,
which publishes the ingress envelope onto the queue.

---

## See also

- [Pointing a domain at MXHook](dns-setup.md) — MX target hostname vs recipient
  domain, DNS records, port 25
- [Architecture](architecture.md) — pipeline, ingestion paths, runtime roles
- [Durable queue contract](queue.md) — ingress and consumption semantics
- [Maildir ingestion](maildir.md) — claims, ownership, quarantine
- [Configuration](configuration.md) — every setting and its default
- [Operations and recovery](operations.md) — health, backup, incidents
