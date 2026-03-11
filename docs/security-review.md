# Security Review — MXHook SaaS

## Scope

Cloudflare Worker API (`worker/src/`), D1 database, R2 bucket, Queues.

---

## Checklist

### Authentication

- [x] Session tokens: HMAC-SHA256 signed, base64url encoded (`worker/src/auth.ts`)
- [x] Token expiry enforced (checked on every request)
- [x] Admin endpoints gated by `X-Admin-Key` header (separate from tenant keys)
- [x] API keys stored hashed (bcrypt) in `api_keys` D1 table
- [x] No keys in Worker source; all via `wrangler secret put`
- [x] Impersonation sessions: 30-min TTL, audit-logged

### Input Validation

- [x] Email parser: uses enmime (Go) — battle-tested; rejects malformed RFC 5321
- [x] Webhook URLs: validated as HTTP/HTTPS before storage; reject `localhost`/RFC1918
- [x] Pagination cursors: numeric IDs only, not user-controlled SQL fragments
- [x] Plan/status updates: allowlist validation before D1 write
- [x] Admin incident create: component allowlist enforced

### Rate Limiting

- [x] Per-IP rate limit: 100 msgs/hour enforced in `worker/src/abuse.ts`
- [x] Cloudflare WAF: enable rate-limiting rule at CF level (10k req/min per IP)
- [ ] TODO: per-tenant API rate limiting (Cloudflare Rate Limiting product)

### HMAC Webhook Signatures

- [x] Outbound webhooks: `X-MXHook-Signature: sha256=<hmac>` header
- [x] HMAC key per route, stored in `routes.webhook_secret` (encrypted at rest via D1)
- [x] Constant-time comparison for signature verification
- [ ] TODO: webhook signature rotation endpoint

### TLS Enforcement

- [x] All Worker traffic: TLS 1.2+ enforced by Cloudflare edge (not configurable to downgrade)
- [x] Webhook delivery: validate `https://` prefix; reject `http://` unless explicitly whitelisted
- [x] SMTP ingestion nodes (fly.io): STARTTLS required; plaintext sessions rejected

### SQL Injection Prevention

- [x] All D1 queries use parameterized `.bind()` — no string interpolation in SQL
- [x] Dynamic column lists (ORDER BY, table names) use allowlists, not user input
- [x] Verified: `worker/src/dashboard/messages.ts`, `admin/tenants.ts`, `statuspage.ts`

### Abuse Mitigation

- [x] Spam scoring: keyword-based signal detection (`worker/src/abuse.ts`)
- [x] Blocklist: per-email and per-domain patterns in D1 `blocked_senders` table
- [x] Blocked messages: logged, not delivered to webhooks
- [x] Audit log for admin actions (impersonation, blocklist changes)

### Data Isolation

- [x] All tenant queries include `tenant_id = ?` predicate
- [x] Session includes `tenantId`; cannot be overridden by request params
- [x] Admin endpoints require separate admin key (not tenant session)

### Secrets Management

- [ ] `SESSION_SECRET`: `wrangler secret put SESSION_SECRET` — rotate quarterly
- [ ] `ADMIN_API_KEY`: `wrangler secret put ADMIN_API_KEY` — rotate after any suspected leak
- [ ] R2 credentials: Cloudflare-managed; no external S3 keys

### Dependency Supply Chain

- Go vendor directory committed (`vendor/`); no network fetch during build
- Worker: minimal TS with no npm deps; built with `tsc` + `esbuild`
- Review vendor/ on each dep update: `go mod verify`

---

## Known Gaps / TODOs

| Item | Priority |
|---|---|
| Per-tenant API rate limiting | High |
| Webhook secret rotation endpoint | Medium |
| CSP / security headers on dashboard HTML | Medium |
| MFA for admin panel | High |
| Regular automated secret rotation | High |
| DMARC/DKIM enforcement on inbound | Medium |
