# Load Testing — MXHook SaaS

## Tools

- k6 (https://k6.io); install: `brew install k6`
- Target: Cloudflare Worker + D1

## Scripts

| Script | Purpose |
|---|---|
| `k6/smoke-test.js` | Sanity — 10 VUs, 30s; verify endpoints respond |
| `k6/load-test.js` | Load — ramp to 100 VUs over 5min, sustain 10min |

## Running

### Smoke test

```
k6 run k6/smoke-test.js \
  -e BASE_URL=https://mxhook.io \
  -e API_KEY=<your-api-key>
```

### Full load test

```
k6 run k6/load-test.js \
  -e BASE_URL=https://mxhook.io \
  -e API_KEY=<your-api-key>
```

### Local (Wrangler dev)

```
wrangler dev worker/src/index.ts --local
k6 run k6/smoke-test.js -e BASE_URL=http://localhost:8787
```

## Thresholds

| Metric | Target |
|---|---|
| p(95) latency | < 800ms |
| p(99) latency | < 1500ms |
| Error rate | < 2% |
| Message list p(95) | < 600ms |
| Domain list p(95) | < 400ms |

## Test Scenarios (load-test.js)

| Scenario | Weight | Endpoint |
|---|---|---|
| Message list | 40% | GET /api/v1/messages |
| Domain list | 20% | GET /api/v1/domains |
| Billing usage | 15% | GET /api/v1/billing/usage |
| Status page | 10% | GET /status |
| Health check | 15% | GET /health |

## Interpreting Results

- `http_req_duration` — end-to-end latency; watch p(95) and p(99)
- `errors` — rate of non-2xx/expected responses
- `msg_list_latency` — D1 read performance for message log
- Check Worker CPU time in Cloudflare dashboard alongside k6 run

## SMTP Ingestion

SMTP is TCP-based; k6 does not test SMTP directly.
Use `swaks` or `msmtp` for SMTP throughput validation:

```
# send 100 emails via swaks (requires swaks installed)
for i in $(seq 1 100); do
  swaks --to inbox@yourdomain.test \
        --server mail.mxhook.io:25 \
        --body "load test $i" &
done
wait
```

Monitor queue depth in Cloudflare Queues dashboard.
