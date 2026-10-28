# Prometheus alerts

Load `mxhook-alerts.yml` with the Prometheus rule-file configuration. The rules
assume the scrape job is named `mxhook`; change that matcher if the deployment
uses another job label.

The application exposes queue, outbox, dead-delivery, retry, and durable
snapshot metrics. Host disk usage, Maildir file counts, and certificate expiry
come from the host/node, filesystem, and TLS exporters and remain deployment-
specific.
