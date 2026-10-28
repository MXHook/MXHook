# Pointing a domain at MXHook

This guide takes a domain you own and a host you control, and ends with mail for
that domain arriving at MXHook. It assumes no prior experience with MX records.

Throughout, `example.com` is the domain whose mail you want to receive,
`mx.example.com` is the hostname of the machine running MXHook, and
`203.0.113.10` stands in for that machine's public IP address. Substitute your
own values consistently: wherever this guide writes `mx.example.com` it means
the server, and wherever it writes `example.com` it means the domain in the
recipient's email address.

## MXHook receives mail; it never sends it

MXHook is an inbound-only server. It accepts SMTP connections, persists what it
receives, and forwards each message to your application as an HTTPS webhook. It
never connects out to another mail server, never generates bounces, and never
relays. The only outbound network traffic it makes is the webhook POST.

This changes what DNS you need, and it is the single most common source of
wasted effort in a first setup:

- **You do not need an SPF record for MXHook.** SPF authorizes hosts to *send*
  mail as your domain. MXHook sends nothing, so it does not belong in one.
- **You do not need a DKIM key for MXHook.** DKIM signs outgoing mail. MXHook
  signs nothing — the HMAC on the webhook is a separate mechanism, unrelated to
  DKIM, and documented in the [README](../README.md).
- **Adding an MX record does not disturb your existing outbound records.** MX,
  SPF, and DKIM are independent record sets. If your domain already sends mail
  through a provider, adding or changing an MX record leaves that sending path
  and its SPF/DKIM records untouched. Mail flow out of your domain is unaffected.
- **DMARC concerns your domain's senders, not this MX.** A DMARC policy on
  `example.com` tells *other* receivers what to do with mail claiming to be from
  you. It does not govern what MXHook accepts. MXHook does not currently
  evaluate inbound SPF, DKIM, or DMARC; every message that passes recipient
  checks is enqueued, and any authentication decisions are yours to make in the
  webhook receiver from the headers in the payload.

**PTR / reverse DNS is a lower-stakes question here, but not a non-issue.** A
PTR record maps `203.0.113.10` back to a hostname. Sending servers are judged
harshly for a missing or mismatched PTR; a receiving server mostly is not,
because the peer connecting to you has no reason to look you up. Two cases still
justify setting it:

- Some senders and greylisting implementations do check that the MX host they
  connected to has a sane forward-confirmed reverse DNS entry.
- Diagnosing a delivery problem is markedly easier when the IP in someone else's
  logs resolves to a name you recognize.

Set the PTR for `203.0.113.10` to `mx.example.com` — matching the MX target's
forward record — through your hosting provider's control panel. PTR records live
with whoever controls the IP address, not with your domain registrar. Treat it
as good hygiene, not a prerequisite for receiving mail.

## The two hostnames, and why setups fail here

MXHook takes two domain-shaped settings that look interchangeable and are not.
They appear next to each other in [configuration.md](configuration.md) and in
this YAML fragment:

```yaml
smtp:
  domain: mx.example.com        # --smtp-domain      the MX target hostname
  allowed_domains: [example.com] # --recipient-domain the accepted recipient domain
```

They mean different things:

| Setting | Flag | Environment | Example | What it is |
|---|---|---|---|---|
| `smtp.domain` | `--smtp-domain` | `MXHOOK_SMTP_DOMAIN` | `mx.example.com` | The **server's own name**. Announced in the SMTP greeting and in `EHLO` responses. This is the hostname your MX record points *to*. |
| `smtp.allowed_domains` | `--recipient-domain` | `MXHOOK_SMTP_ALLOWED_DOMAINS` | `example.com` | The **recipient domain(s) you accept mail for**. The part after the `@` in the addresses you want delivered. |

The distinction matters because only one of them decides whether a message is
accepted. When a sender issues `RCPT TO:<support@example.com>`, MXHook compares
the part after the `@` against `allowed_domains`, case-insensitively and
ignoring a trailing dot. A recipient domain not on that list is rejected with
`550 5.1.1 recipient not accepted`. `smtp.domain` plays no part in this decision
— it only labels the server.

So the failure mode is precise and easy to hit. Set `--recipient-domain` to
`mx.example.com` (the server's name) instead of `example.com` (the domain in the
address), and everything looks correct: DNS resolves, the port answers, the
banner greets, `MAIL FROM` succeeds — and then every real message is rejected at
`RCPT TO`, because nobody sends mail to `anything@mx.example.com`.

Two rules keep it straight:

- `--smtp-domain` is where mail *goes* (the machine). It is what you write on
  the right-hand side of the MX record.
- `--recipient-domain` is what mail is *addressed to* (the domain). It is what
  appears after the `@` in your users' addresses.

They are frequently *not* the same value, and there is no requirement that they
be related at all. One MXHook instance running as `mx.example.com` can accept
mail for several unrelated domains by repeating the flag:

```bash
mxhook start \
  --smtp-domain mx.example.com \
  --recipient-domain example.com \
  --recipient-domain example.net
```

The environment form takes a comma- or space-separated list:

```text
MXHOOK_SMTP_ALLOWED_DOMAINS=example.com,example.net
```

At least one recipient domain is mandatory whenever SMTP is enabled. MXHook
refuses to start without one rather than open a listener that would accept
nothing, and `mxhook config validate` reports the same error before any listener
is opened.

Adding a recipient domain is a configuration change followed by a restart. Set
`--recipient-domain` or `MXHOOK_SMTP_ALLOWED_DOMAINS` and restart the process;
the new domain is accepted from that point on. You will also want a route
matching `*@example.com` so accepted mail has somewhere to go — see the
[CLI reference](cli.md) for `mxhook route add`.

## The DNS records

Two records are required, and they must be created in this order, because the
second one refers to the first.

### 1. An address record for the server

`mx.example.com` has to resolve to the IP address of the machine running MXHook,
via an `A` record for IPv4 and/or an `AAAA` record for IPv6:

```text
mx.example.com.   300   IN   A      203.0.113.10
mx.example.com.   300   IN   AAAA   2001:db8::10
```

IPv4 alone is sufficient. Add the `AAAA` record only if the host genuinely has a
working IPv6 address and MXHook is listening on it — an `AAAA` record pointing
somewhere unreachable causes intermittent, hard-to-diagnose delivery delays as
senders try IPv6 first and time out.

### 2. An MX record for the domain

The MX record tells the world which host accepts mail for `example.com`:

```text
example.com.   300   IN   MX   10   mx.example.com.
```

Reading it left to right: mail addressed to `example.com` should be delivered to
`mx.example.com`, with preference `10`.

The **priority** (`10`) orders multiple MX hosts, and lower wins. A single MX
record's priority value is arbitrary — `10` is conventional. Priorities matter
only when you publish more than one host, in which case senders try the lowest
number first and fall back to higher ones when it is unreachable:

```text
example.com.   300   IN   MX   10   mx.example.com.
example.com.   300   IN   MX   20   mx-backup.example.com.
```

Do not add a backup MX unless something is actually running there. A backup MX
that refuses connections adds delivery delay; one that accepts mail and has
nowhere to forward it loses mail outright. One MX record is the right answer for
almost every MXHook deployment.

Note the trailing dots. In a zone file, `mx.example.com.` is absolute while
`mx.example.com` (no dot) is relative and expands to
`mx.example.com.example.com.` — a classic and mystifying error. Most hosted DNS
control panels handle this for you and expect the name without a dot; if you are
editing a zone file directly, include it.

### The rule about MX targets

**An MX record must point to a hostname that has an A or AAAA record. It must
never point to a CNAME, and never to a bare IP address.**

Both violations are worth stating explicitly because both are tempting and both
fail:

```text
example.com.   300   IN   MX   10   203.0.113.10          # WRONG: bare IP
example.com.   300   IN   MX   10   mail.hosting.example. # WRONG if that name is a CNAME
```

An MX record's value is defined as a domain name, so an IP address there is not
merely discouraged — it is not valid data, and senders will fail to resolve it.
A CNAME target is prohibited by the DNS specifications for MX records; some
senders follow it anyway, others do not, and you get delivery that works from
some networks and silently fails from others. If your provider gave you a CNAME
for your host, resolve it yourself and publish an `A` record for
`mx.example.com` with the resulting address.

### TTL: lower it before, raise it after

The TTL (`300` above, in seconds) tells other servers how long to cache the
record. It is the lever that controls how fast a change takes effect.

Lower the TTL on the MX record — and on the MX target's A/AAAA record — to
`300` at least a full old-TTL period *before* you plan to cut over. If the
record currently has a TTL of `86400`, resolvers may hold the old value for up
to a day, so lower it a day ahead. The lowering itself only takes effect once
the old cached copies expire, which is why it must come first.

Once the new MX has been serving correctly for a day or two, raise the TTL back
to something like `3600` or `14400`. A long TTL reduces lookup load and makes
your domain more resilient to a brief nameserver outage; you only want it short
while a change is in flight.

## Port 25 reality

Mail servers talk to each other on TCP port 25. This is not configurable — a
sending server looks up your MX record and connects to port 25 on that host.
There is no way to publish "use port 2525 instead" in DNS for inbound mail.

That fact collides with a policy nearly every cloud provider enforces, and it is
worth understanding precisely, because the restriction is usually **outbound**
while what you need is **inbound**.

### MXHook needs inbound 25 only

MXHook receives; it never sends. A provider that blocks *outbound* port 25 does
not prevent MXHook from working at all. Inbound port 25 is generally permitted
by default even by providers with aggressive outbound blocks — but you still
have to open it explicitly in your own firewall and in the provider's network
layer, which are separate things.

There is one real trap in this asymmetry, covered under
[Smoke-testing](#smoke-testing-before-cutover) below: the outbound block *does*
stop you from testing your own server with a mail client running on that same
host, even though the server itself is fine. Test from somewhere else.

### What each provider does

| Provider | Outbound 25 | How to lift it |
|---|---|---|
| **AWS EC2** | Blocked by default; throttled rather than fully closed on some accounts | File the "Request to Remove Email Sending Limitations" form; AWS also expects an Elastic IP with a matching reverse DNS record |
| **Google Cloud** | Blocked permanently, with no exception process | Not liftable. Outbound mail must go through a third-party relay on another port |
| **DigitalOcean** | Blocked by default for new accounts | Open a support ticket describing the use case |
| **Vultr** | Blocked by default for new accounts | Open a support ticket |
| **Hetzner** | Blocked by default for new accounts | Request a lift through the support/robot console |

Because MXHook is receive-only, none of these outbound blocks stand between you
and a working inbound MX. Even the permanent GCP block is not disqualifying for
MXHook. Filing the unblock request is still worth doing if the same host needs
to send mail for any other reason, and on AWS the same request is bundled with
the reverse-DNS setup you may want anyway.

### Opening inbound 25

Two layers usually need changing, and forgetting the second is the most common
reason a correct DNS setup still refuses connections.

The provider's network layer:

- **AWS**: add an inbound rule to the security group for TCP port 25 from
  `0.0.0.0/0` (and `::/0` if you publish an AAAA record).
- **GCP**: add a VPC firewall rule allowing `tcp:25` ingress.
- **DigitalOcean / Vultr / Hetzner**: allow TCP 25 inbound in the cloud
  firewall, if one is attached.

The host's own firewall:

```bash
# ufw (Debian/Ubuntu)
sudo ufw allow 25/tcp

# firewalld (RHEL/Fedora)
sudo firewall-cmd --permanent --add-port=25/tcp
sudo firewall-cmd --reload
```

A static or Elastic IP is effectively mandatory. Your MX record points at an IP
address, and DNS caching means a changed IP takes a full TTL to propagate — mail
sent in that window goes to whoever holds the old address. Allocate a static
address before you publish any records.

### Why the container listens on 2525

The port numbers in this repository can look contradictory. They are not; they
describe two different layers.

- The **binary** defaults `--smtp-addr` to `0.0.0.0:25` — the standard port,
  which is what you want when running MXHook directly on a host as a service.
- The **container** sets `MXHOOK_SMTP_ADDR=:2525` in the `Dockerfile`. The base
  Compose file defaults to the same value, and the STARTTLS overlay sets it
  outright. The container runs as an unprivileged numeric user (UID 1000), and
  an unprivileged process cannot bind a port below 1024. Listening on 2525 is
  what lets the container drop root entirely.

Nothing is lost by this, because publishing a container port remaps it. The
STARTTLS overlay maps host port 25 to container port 2525:

```yaml
ports:
  - "${MXHOOK_SMTP_BIND:-0.0.0.0}:${MXHOOK_SMTP_PORT:-25}:2525"
```

The outside world connects to port 25 on the host, exactly as the MX record
promises; Docker forwards to 2525 inside, where an unprivileged MXHook is
listening. Port 25 is bound by the Docker daemon, which is already privileged,
so MXHook itself never needs the capability.

The base `docker-compose.yml` deliberately does not publish the SMTP port at
all. Add the overlay to accept mail from the internet:

```bash
docker compose -f docker-compose.yml -f docker-compose.smtp-tls.yml up -d --build
```

The overlay requires `MXHOOK_SMTP_CERT_FILE` and `MXHOOK_SMTP_KEY_FILE` in
`.env`, and it sets `MXHOOK_SMTP_REQUIRE_TLS=true`. Override `MXHOOK_SMTP_PORT`
to publish somewhere other than 25 while testing.

Running the binary directly on the host has the mirror-image problem: binding
port 25 needs privileges. Grant the capability rather than running as root:

```bash
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/mxhook
```

Re-apply it after every upgrade — replacing the binary discards the capability.

## Verification

Work through these in order. Each one isolates a different layer, so the first
that fails tells you where the problem is.

### DNS resolves

```bash
dig MX example.com +short
```

Expect the priority and target, with a trailing dot:

```text
10 mx.example.com.
```

Then confirm the target has an address record — this is the step that catches a
CNAME target or a typo in the hostname:

```bash
dig A mx.example.com +short
dig AAAA mx.example.com +short
```

`dig A` must return the IP address of your server. If it returns nothing, or
returns a name rather than an address, the MX target is broken regardless of how
correct the MX record itself looks.

To see the TTLs you are actually serving, ask for the full answer section:

```bash
dig MX example.com +noall +answer
```

Query an external resolver to check what the rest of the world sees, rather than
a local cache that may still hold old data:

```bash
dig @1.1.1.1 MX example.com +short
```

### The port answers

From a machine *other than* the MXHook host:

```bash
nc -vz mx.example.com 25
```

A successful connection reports `succeeded`. A hang means a firewall is dropping
packets silently — check the provider's security group first, then the host
firewall. Immediate `Connection refused` means packets reach the host but
nothing is listening: check that MXHook is running and that the container port
is published.

To see the SMTP banner itself, and confirm it is MXHook answering rather than
some other daemon:

```bash
nc -w 5 mx.example.com 25
```

The greeting names the host from `--smtp-domain`. Type `QUIT` and press enter to
close cleanly.

If STARTTLS is configured, verify the certificate:

```bash
openssl s_client -starttls smtp -connect mx.example.com:25 -servername mx.example.com
```

Check that the certificate's subject matches `mx.example.com` and that the chain
validates. The certificate must be issued for the **MX target hostname**, not
for the recipient domain — another place the two names get confused.

### End-to-end with swaks

[`swaks`](https://www.jetmore.org/john/code/swaks/) is the standard tool for
scripted SMTP testing. Install it with your package manager (`apt install
swaks`, `brew install swaks`).

```bash
swaks --to test@example.com \
      --from sender@example.net \
      --server mx.example.com:25 \
      --h-Subject "MXHook connectivity test"
```

Read the transcript rather than just the exit code. The lines that matter:

- `250` after `RCPT TO` — the recipient domain is in `allowed_domains`. A
  `550 5.1.1 recipient not accepted` here is the two-hostnames mistake: check
  `--recipient-domain` is the address domain, not the server name.
- `250` after the final `.` — the message was accepted and durably enqueued.

Add `--tls` to force STARTTLS, which is required when the server runs with
`--smtp-require-tls` (as the STARTTLS overlay does):

```bash
swaks --to test@example.com \
      --from sender@example.net \
      --server mx.example.com:25 \
      --tls
```

Then confirm the message actually made it through the pipeline:

```bash
mxhook logs --api-key "$MXHOOK_API_KEY"
```

A message that appears here but never reaches your webhook is a delivery
problem, not a DNS problem — check `GET /dlq` and see
[Operations and recovery](operations.md).

### Smoke-testing before cutover

While the MX record still points elsewhere, test by naming the server directly.
`swaks --server mx.example.com:25` connects to that host regardless of what DNS
says about `example.com`'s MX, which lets you validate the whole path before any
mail depends on it. The same works against the non-privileged port during
staging:

```bash
swaks --to test@example.com --from sender@example.net --server mx.example.com:2525
```

**The trap:** if you run this test *from the MXHook host itself*, and your
provider blocks outbound port 25, the test fails with a timeout while the server
is perfectly healthy. The block is on the outbound connection your test client
is making, not on the inbound path real senders use. This produces a
convincingly wrong conclusion — an operator concludes the server is broken and
starts changing a working configuration.

Test from a laptop, a different VPS, or any host outside the blocked network.
If you must test from the same host, target the loopback interface to stay
inside the machine and avoid the provider's egress filter entirely:

```bash
swaks --to test@example.com --from sender@example.net --server 127.0.0.1:2525
```

A loopback test proves MXHook is accepting and routing correctly, but proves
nothing about firewalls or DNS. It is a first step, not a substitute for an
external test.

## Cutover checklist

Run in order. Each step is verifiable before the next, so a failure never
strands live mail.

1. **Verify locally.** Start MXHook with `--recipient-domain example.com` and at
   least one route. Confirm `mxhook config validate` passes and `/readyz` is
   green. Send a message over loopback and confirm it reaches your webhook.

2. **Open the firewall.** Allow inbound TCP 25 in the provider's security group
   or cloud firewall *and* in the host firewall. Confirm the server has a static
   or Elastic IP.

3. **Publish the A/AAAA record** for `mx.example.com` pointing at that IP, with
   a TTL of `300`. Verify with `dig A mx.example.com +short`. Do this before the
   MX record — an MX record naming a host that does not resolve is broken.

4. **Obtain a TLS certificate** for `mx.example.com` (the MX target hostname,
   not the recipient domain). Configure `--smtp-tls-cert` and `--smtp-tls-key`,
   or the Compose overlay's `MXHOOK_SMTP_CERT_FILE` and `MXHOOK_SMTP_KEY_FILE`.
   MXHook supports STARTTLS, not implicit TLS. It reloads the certificate from
   disk without a restart, so renewals do not require downtime.

5. **Smoke-test from outside.** With the server reachable but the MX record not
   yet changed, run `nc -vz mx.example.com 25` and a `swaks` test naming the
   server directly, from a host that is not the MXHook machine. During staging
   the same test works against the unpublished port, e.g. `:2525`. Fix anything
   that fails here — this is the last cheap moment.

6. **Lower the TTL** on the existing MX record (and on the target's A/AAAA
   record) to `300`. Wait at least one full *previous* TTL for caches to expire
   before proceeding. Skipping this wait is why cutovers appear to "not take
   effect".

7. **Change the MX record** to point at `mx.example.com`. If you are migrating
   from another mail host, remove or re-prioritize its MX record in the same
   edit so mail is not split between two servers.

8. **Confirm delivery.** Watch `dig MX example.com +short` from several networks
   until it consistently returns the new value, then send real mail from an
   external account to an address on `example.com`. Verify it appears in
   `mxhook logs` and reaches your webhook. Watch `/metrics` and `GET /dlq` for
   the first hour.

9. **Raise the TTL** back to `3600` or higher once delivery has been stable for
   a day or two.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `dig MX example.com +short` returns nothing | MX record not published, or not yet propagated. Check the zone at the authoritative nameserver. |
| `dig A mx.example.com +short` returns nothing | The MX target has no address record, or is a CNAME. Publish an `A` record for the target. |
| `nc -vz` hangs | Packets dropped. Provider security group or host firewall is blocking inbound 25. |
| `nc -vz` reports connection refused | Nothing listening. MXHook is not running, or the container's SMTP port is not published. |
| Test from the MXHook host times out, external test works | Provider blocks *outbound* 25. The server is fine; test from elsewhere. |
| `550 5.1.1 recipient not accepted` | The recipient's domain is not in `allowed_domains`. Almost always `--recipient-domain` was set to the server hostname instead of the address domain. |
| `530 5.7.0 STARTTLS is required` | `--smtp-require-tls` is on and the client did not upgrade. Add `--tls` to the swaks command. |
| Startup fails: at least one allowed recipient domain is required | No `--recipient-domain` / `MXHOOK_SMTP_ALLOWED_DOMAINS` set. |
| `451 4.3.0 temporary ingestion failure` | Accepted but not enqueued — a storage or database fault. Check `/readyz` and see [Operations and recovery](operations.md). |
| Mail accepted but no webhook | A routing or delivery problem, not DNS. Check `GET /dlq`. |

## Related documentation

- [Configuration](configuration.md) — full settings reference
- [CLI reference](cli.md) — flags and control commands
- [Operations and recovery](operations.md) — health, incidents, backup
- [Maildir ingestion](maildir.md) — accepting mail without exposing SMTP
