# Launch Checklist — MXHook SaaS

## Pre-Launch Gate

All items must be green before flipping public DNS.

---

### Infrastructure

- [ ] Cloudflare Worker deployed to production account
- [ ] D1 database created + all migrations applied (0001–0007)
- [ ] R2 bucket created; CORS policy set
- [ ] Cloudflare Queue created + Worker consumer bound
- [ ] SMTP edge nodes (fly.io) deployed, health checks passing
- [ ] fly.io → Cloudflare Queue IAM credentials set

### Secrets

- [ ] `SESSION_SECRET` set via `wrangler secret put`
- [ ] `ADMIN_API_KEY` set and stored in password manager
- [ ] Stripe keys configured (if billing enabled)

### DNS

- [ ] `mxhook.dev` A/AAAA → Cloudflare Worker route
- [ ] MX records for `mail.mxhook.dev` → SMTP edge nodes
- [ ] `status.mxhook.dev` CNAME → Worker `/status.html`
- [ ] SPF / DKIM / DMARC records verified

### Security

- [ ] Security review checklist complete (`docs/security-review.md`)
- [ ] Admin panel URL not publicly linked (security by obscurity + key)
- [ ] WAF rate-limiting rules active
- [ ] `security.txt` accessible at `/.well-known/security.txt`

### Testing

- [ ] Smoke test passing: `k6 run k6/smoke-test.js -e BASE_URL=https://mxhook.dev`
- [ ] Load test passing: `k6 run k6/load-test.js -e BASE_URL=https://mxhook.dev`
- [ ] Manual end-to-end: send email → verify webhook delivery
- [ ] Dashboard UI: login, view messages, view domains, billing

### Observability

- [ ] Cloudflare Workers analytics enabled
- [ ] Error alerting set up (Workers tail log or Logpush → alerting)
- [ ] Status page live at `/status.html`
- [ ] Admin health check passing: `GET /admin/health`

### Business / Legal

- [ ] Privacy Policy published
- [ ] Terms of Service published
- [ ] Pricing page live
- [ ] Stripe checkout flow tested end-to-end
- [ ] Support email / contact form live

### Launch

- [ ] README updated with SaaS tier info and signup link
- [ ] Announce: X / HN / Discord / email list
- [ ] Monitor error rates for 24h post-launch
- [ ] Rollback plan documented (Worker version pin)
