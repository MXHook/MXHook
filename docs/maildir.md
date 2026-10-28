# Maildir Ingestion

MXHook can ingest complete RFC 5322 messages from a Maildir. Any producer that
writes Maildir works: a local Postfix or Dovecot delivering for your own domain,
a provider such as exe.dev that delivers to the filesystem instead of exposing
public SMTP, or a forwarding rule from an existing Google Workspace or Fastmail
account. None of them require you to open port 25.

Maildir ingestion requires a supported Unix-like host; Windows builds remain
available for non-Maildir commands. For how this path compares with running
your own MX, see [the ingestion path comparison](architecture.md#choosing-an-ingestion-path).

## Ownership and safety

The configured Maildir must contain `new/`, `cur/`, and `tmp/`. MXHook treats
the mailbox as exclusively owned:

- it writes a versioned `.mxhook-owner` marker before claiming mail;
- it holds a non-blocking exclusive `.mxhook.lock` for the process lifetime, so
  a second MXHook process refuses the same Maildir;
- a first start refuses a nonempty `cur/` unless `--maildir-adopt-existing` is
  explicitly supplied;
- the Maildir root and its `new/`, `cur/`, and `tmp/` directories must not be
  group- or world-writable; startup refuses such permissions (use `0700`);
- no MUA or second MXHook process may consume the same Maildir;
- dotfiles, directories, symlinks, and other non-regular entries are never read;
- permanent invalid or oversized messages move to `mxhook-failed/`;
- upstream `tmp/` files are never touched.

Adoption is a one-time migration operation. Inspect `cur/` before enabling it.

### Permissions

Startup checks the Maildir root and its `new/`, `cur/`, and `tmp/`
subdirectories and refuses any that is group- or world-writable. Group-*readable*
is fine; only the write bits are rejected. `mxhook-failed/` is created `0700`.

This rejects the conventional group-mail delivery layout, where the mailbox is
owned by a mail group and left `0770` so several daemons can write it. MXHook
will not start on such a directory, by design: any member of that group could
otherwise inject messages that become webhook deliveries. The fix is to make
the MXHook service account the sole owner and run the delivery agent as that
same account, as the Postfix example below does — never to relax the mode.

```bash
chmod 0700 /var/lib/mxhook/Maildir /var/lib/mxhook/Maildir/{new,cur,tmp}
```

Each of the four paths must also be a real directory, not a symlink.

## Delivery lifecycle

1. The provider completes delivery into `new/<unique>`.
2. MXHook atomically claims it as `cur/<unique>:2,`.
3. MXHook reads at most the configured size limit without changing the bytes.
4. `Delivered-To` supplies the envelope recipient. `Return-Path` supplies the
   envelope sender when present.
5. A deterministic delivery ID and raw message are durably enqueued.
6. The claimed file is deleted only after enqueue succeeds.

Queue failures leave claims in `cur/`. Startup and periodic scans retry them.
A crash after enqueue but before deletion may enqueue the same ID again, so the
SQLite and Postgres queues treat identical IDs as idempotent. The guarantee is
at-least-once, not exactly-once.

The Cloudflare Queues backend does not deduplicate by ID: a crash-retry
re-enqueues the same message and downstream consumers receive duplicates. With
`--queue-backend cloudflare`, webhook consumers must dedupe on the delivery ID
themselves; prefer the SQLite or Postgres queue when that is not possible.

Maildir does not standardize envelope recipients. Generic providers must add a
trusted recipient header or configure `--maildir-default-recipient`; see
[the recipient header trust boundary](#the-recipient-header-trust-boundary) for
what makes such a header trustworthy.

## CLI

The examples below assume at least one durable API route already exists, or
that `MXHOOK_WEBHOOK_URL` and `MXHOOK_WEBHOOK_SECRET` define a static route.
The `all` role refuses to start ingress with no effective route.

Maildir only, embedded SQLite:

```bash
mxhook start \
  --smtp-addr "" \
  --maildir "$HOME/Maildir" \
  --db-url ./data/mxhook.db
```

SMTP and Maildir together:

```bash
mxhook start \
  --smtp-addr :2525 \
  --maildir /srv/Maildir
```

Relevant environment variables use the same names with an `MXHOOK_` prefix and
underscores, for example `MXHOOK_MAILDIR_PATH` and
`MXHOOK_MAILDIR_POLL_INTERVAL`.

## The recipient header trust boundary

MXHook reads the envelope recipient from the header named by
`--maildir-recipient-header` (default `Delivered-To`), and falls back to
`--maildir-default-recipient` only when that header is absent or blank. When
neither yields an address the message is quarantined to `mxhook-failed/`.

The header is read with Go's `net/mail` header lookup, which returns the
**first** occurrence when a message carries several. That single detail decides
whether the value can be trusted:

- A message arriving from the internet may already carry a `Delivered-To`
  written by anyone. Nothing about the header is authenticated.
- A local MTA delivering the message **prepends** its own `Delivered-To` above
  every header already present, so its value is the first occurrence and the
  one MXHook uses.

So the header is trustworthy exactly when the process that wrote the Maildir is
the process that injected the header, and untrustworthy when it was copied in
from an upstream you do not control. If MXHook drained a Maildir written by
something that merely relays messages without prepending its own header, a
sender could choose its own envelope recipient by adding a `Delivered-To` line
and route mail to any webhook you have configured.

Two ways to keep the boundary intact:

1. Have the local MTA prepend the header, as the Postfix and Dovecot examples
   below do. This preserves per-recipient routing.
2. Pin the recipient with `--maildir-default-recipient` and point
   `--maildir-recipient-header` at a header no upstream will supply. Every
   message then resolves to one address regardless of its contents. Use this
   for a single-purpose mailbox where per-recipient routing is not needed.

Do not point `--maildir-recipient-header` at `To` or `Cc`. MXHook deliberately
refuses to infer envelope recipients from them: they omit Bcc, they need not
match SMTP delivery at all, and they are entirely sender-controlled.

`Return-Path` supplies the envelope sender when present. An empty
`Return-Path: <>` (a bounce) is accepted and yields an empty sender.

## Generic MTA: Postfix

This delivers `*@example.com` into one Maildir that MXHook owns. It assumes
Postfix already accepts mail for the domain.

Create a dedicated unprivileged account whose home holds the Maildir. MXHook
and Postfix both run as this user, which keeps the mailbox mode `0700`:

```bash
sudo useradd --system --create-home --home-dir /var/lib/mxhook --shell /usr/sbin/nologin mxhook
sudo install -d -o mxhook -g mxhook -m 0700 /var/lib/mxhook/Maildir
sudo install -d -o mxhook -g mxhook -m 0700 /var/lib/mxhook/Maildir/new
sudo install -d -o mxhook -g mxhook -m 0700 /var/lib/mxhook/Maildir/cur
sudo install -d -o mxhook -g mxhook -m 0700 /var/lib/mxhook/Maildir/tmp
```

In `/etc/postfix/main.cf`:

```text
virtual_mailbox_domains = example.com
virtual_mailbox_base = /var/lib/mxhook
virtual_mailbox_maps = hash:/etc/postfix/vmailbox
virtual_minimum_uid = 100
virtual_uid_maps = static:$(id -u mxhook)
virtual_gid_maps = static:$(id -g mxhook)
```

Replace `$(id -u mxhook)` and `$(id -g mxhook)` with the literal numeric IDs;
Postfix does not expand shell syntax. Then map every address in the domain to
the one Maildir. The trailing slash is what makes Postfix write Maildir format
rather than mbox:

```text
# /etc/postfix/vmailbox
@example.com    Maildir/
```

Build the map and reload:

```bash
sudo postmap /etc/postfix/vmailbox
sudo postfix reload
```

Postfix's `virtual(8)` delivery agent prepends a `Delivered-To` header carrying
the envelope recipient it delivered for, which is exactly the header MXHook
reads. No extra configuration is needed to produce it.

Point MXHook at the mailbox, with SMTP disabled because Postfix owns port 25:

```bash
mxhook start \
  --smtp-addr "" \
  --maildir /var/lib/mxhook/Maildir \
  --maildir-id-namespace mxhook-postfix-example \
  --db-url /var/lib/mxhook/data/mxhook.db
```

Run MXHook as the `mxhook` user. Postfix's `virtual` agent writes into `new/`;
MXHook claims from `new/` into `cur/`. Nothing else may consume this mailbox —
no IMAP server, no MUA, no second MXHook process.

### Dovecot LDA alternative

Where Dovecot already handles local delivery, hand messages to `dovecot-lda`
and let it write the Maildir. In `/etc/dovecot/conf.d/10-mail.conf`:

```text
mail_location = maildir:/var/lib/mxhook/Maildir
```

In `/etc/dovecot/conf.d/15-lda.conf`:

```text
protocol lda {
  mail_plugins = $mail_plugins
}
lda_mailbox_autocreate = yes
postmaster_address = postmaster@example.com
```

`dovecot-lda` writes a `Delivered-To` header from the envelope recipient it was
invoked with. Pass that recipient explicitly with `-d` so the value is the
envelope address rather than a guess from the message body:

```text
# /etc/postfix/main.cf
virtual_transport = dovecot

# /etc/postfix/master.cf
dovecot   unix  -       n       n       -       -       pipe
  flags=DRhu user=mxhook:mxhook argv=/usr/lib/dovecot/dovecot-lda -f ${sender} -d ${recipient}
```

Dovecot's `lda_original_recipient_header` setting can override which header LDA
consults for the original recipient; leave it unset unless you understand the
consequences, because it makes LDA trust a header from the incoming message.

## Keeping your existing provider

The lowest-friction setup keeps Google Workspace, Fastmail, or whatever already
hosts the domain, and forwards only the addresses MXHook should see. Nothing
about port 25, reverse DNS, or certificates is involved: the provider stays the
public MX, and MXHook only ever reads a local Maildir.

Configure a forwarding rule at the provider — a Gmail filter forwarding
`hooks@example.com`, a Fastmail rule for one alias — targeting an address that
lands in the Maildir the local MTA writes, for example
`hooks@mail.internal.example.com`.

Forwarding changes the envelope recipient. Once the provider forwards
`hooks@example.com` to `hooks@mail.internal.example.com`, the receiving MTA's
`RCPT TO` carries the forwarding target, so the `Delivered-To` it prepends names
that target, not the original address. Routes must match what the header
actually contains.

Two workable arrangements:

**One address per forwarded mailbox.** Forward each original address to its own
distinct local address and create MXHook routes for the local addresses:

```text
hooks@example.com    -> hooks@mail.internal.example.com
alerts@example.com   -> alerts@mail.internal.example.com
```

Both land in one Maildir via the `@mail.internal.example.com` catch-all above,
and the `Delivered-To` header distinguishes them, so per-address routing keeps
working. This is the recommended shape.

**One pinned recipient.** Forward everything to a single address and pin the
recipient, ignoring the header entirely:

```bash
mxhook start \
  --smtp-addr "" \
  --maildir /var/lib/mxhook/Maildir \
  --maildir-recipient-header X-MXHook-Recipient \
  --maildir-default-recipient hooks@example.com \
  --db-url /var/lib/mxhook/data/mxhook.db
```

`X-MXHook-Recipient` is a header no upstream will send, so the lookup misses
and every message resolves to `hooks@example.com`. This is safe precisely
because the fallback is fixed configuration, not message content. Use it when
one webhook handles all forwarded mail.

Recovering the *original* recipient from provider-added headers such as
`X-Forwarded-To` or `X-Original-To` is possible with
`--maildir-recipient-header`, but only sound when the local MTA strips or
overwrites that header on the way in. A header the forwarding provider adds is
still a header a direct sender can forge, so pointing MXHook at it re-opens the
trust boundary unless nothing can reach the mailbox except the provider.

Whichever arrangement you pick, keep the forwarded volume in mind: the provider
still applies its own spam filtering before forwarding, which is usually a
benefit, and MXHook enforces `--maildir-max-message-bytes` (default 10 MiB) on
what arrives.

## [exe.dev](https://exe.dev/docs/receive-email)

Enable filesystem email delivery for the VM:

```bash
ssh exe.dev share receive-email <vm-name> on --reply-policy=none
```

exe.dev writes messages to `~/Maildir/new/`, injects `Delivered-To` as the first
header, limits messages to 1 MiB, and disables receiving when `new/` exceeds
1,000 files. Monitor both `new/` and `cur/` backlog; clearing `new/` alone does
not mean downstream enqueue is healthy.

exe.dev currently receives only `*@<vm-name>.exe.xyz`. To use a custom domain,
forward into that address from wherever the domain's mail is hosted — the same
pattern as [Keeping your existing provider](#keeping-your-existing-provider)
above, with `<vm-name>.exe.xyz` as the forwarding target. The forwarded envelope
recipient becomes `<something>@<vm-name>.exe.xyz`, and that is what exe.dev
writes into `Delivered-To`, so create MXHook routes for the `exe.xyz` addresses
rather than the original ones — or pin a recipient with
`--maildir-default-recipient` as described there.

Keep receive-email disabled during the worker/API bootstrap; enable it only
after a real receiver route exists and the `all` role is ready.

## Docker Compose

```bash
cp .env.example .env
MAILDIR_PATH="$HOME/Maildir" \
  docker compose -f docker-compose.yml -f docker-compose.maildir.yml up -d --build
```

The container runs as UID/GID 1000. The Maildir and data volume must be writable
by that identity. Do not weaken mailbox permissions to group- or world-writable
to make that work — MXHook refuses to start on such a Maildir. Make UID 1000
the owner of the Maildir instead.

## Configuration

| Flag | Environment | Default |
|---|---|---|
| `--maildir` | `MXHOOK_MAILDIR_PATH` | disabled |
| `--maildir-poll-interval` | `MXHOOK_MAILDIR_POLL_INTERVAL` | `2s` |
| `--maildir-max-message-bytes` | `MXHOOK_MAILDIR_MAX_MESSAGE_BYTES` | `10485760` |
| `--maildir-recipient-header` | `MXHOOK_MAILDIR_RECIPIENT_HEADER` | `Delivered-To` |
| `--maildir-default-recipient` | `MXHOOK_MAILDIR_DEFAULT_RECIPIENT` | empty |
| `--maildir-id-namespace` | `MXHOOK_MAILDIR_ID_NAMESPACE` | absolute Maildir path |
| `--maildir-adopt-existing` | `MXHOOK_MAILDIR_ADOPT_EXISTING` | `false` |

Set an explicit stable ID namespace when the Maildir path may change between
deployments. Changing the namespace changes delivery IDs and can cause replayed
files to be treated as new deliveries.
