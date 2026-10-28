# MXHook Grafana Dashboard

Import `mxhook-dashboard.json` into Grafana to get the MXHook overview dashboard.

## Panels

- **Messages Received** — total SMTP/Maildir messages durably enqueued
- **Parse Errors** — total parse failures
- **Webhook Deliveries by Outcome** — rate by success/retry/dead
- **Delivery Latency** — p50/p95/p99 end-to-end latency
- **Queue Depth** — current queue depth
- **Outbox Depth** — pending or in-progress webhook deliveries
- **Dead Deliveries** — terminal ingress and webhook failures
- **Durable Snapshot** — database-backed backlog metric refresh health
- **Webhook Retries** — retry rate

## Setup

1. In Grafana: Dashboards → Import → Upload JSON file
2. Select your Prometheus datasource
3. Click Import
