# MXHook Grafana Dashboard

Import `mxhook-dashboard.json` into Grafana to get the MXHook overview dashboard.

## Panels

- **Messages Received** — total SMTP-accepted emails
- **Parse Errors** — total parse failures
- **Webhook Deliveries by Outcome** — rate by success/failure/dlq
- **Delivery Latency** — p50/p95/p99 end-to-end latency
- **Queue Depth** — current queue depth
- **Webhook Retries** — retry rate

## Setup

1. In Grafana: Dashboards → Import → Upload JSON file
2. Select your Prometheus datasource
3. Click Import
