# Product Requirements Document (PRD)

## Project
**MXHook**

## Organization
**MXHook**

## Repository
`github.com/mxhook/mxhook`

## License
Open Source (likely Apache 2.0 or MIT — final decision TBD)

---

# 1. Overview

MXHook is an open-source inbound email processing platform that converts SMTP email into structured events and webhooks.

It allows developers to programmatically receive emails on their own infrastructure without relying on third-party email APIs (Resend, Sendgrid, Mailgun, etc.).

MXHook acts as an **MX server** capable of:

- receiving email via SMTP
- parsing the email (headers, body, attachments)
- routing messages
- delivering structured events via webhook or queue

It is designed to function as:

1. **Self-hosted open-source inbound email automation engine**
2. **Enterprise deployable platform**
3. **Multi-tenant hosted SaaS platform**

---

# 2. Vision

Make inbound email automation infrastructure **simple, transparent, self-hostable, and programmable**.

MXHook should become the **standard open-source inbound email webhook platform**.

Comparable tools:

- Mailgun Routes
- Sendgrid Parse API
- Resend Inbound
- Postmark Inbound
- MyMX

But open-source and infrastructure-first.

---

# 3. Core Principles

### Infrastructure-first
SMTP reliability must come before application logic.

### Minimal dependencies
Core system must remain lightweight and deployable with minimal infrastructure.

### Horizontally scalable
Stateless workers + persistent queue.

### Observable
Every email must be traceable end-to-end.

### Extensible
Plugins, rules, and routing should be programmable.

---

# 4. Target Users

## Developers
Want inbound email webhooks.

## SaaS products
Need email ingestion pipelines.

## Automation builders
Email → trigger workflows.

## Enterprises
Need private email processing infrastructure.

---

# 5. Product Versions

## Open Source Core
Self-hosted inbound email processor.

## Enterprise Edition (EE)
Adds operational features and analytics.

## Hosted SaaS
Managed multi-tenant MXHook platform.

---

# 6. High-Level Architecture

```
Internet SMTP
      │
      ▼
MXHook SMTP Server
      │
      ▼
Message Queue
      │
      ▼
Parser Service
      │
      ▼
Event Router
      │
      ▼
Delivery System
      │
      ├── Webhooks
      ├── Queue (Kafka/NATS/SQS)
      └── Storage
```

---

# 7. Core Components

## 7.1 SMTP Receiver

Responsible for accepting inbound email.

Responsibilities:

- accept SMTP connections
- enforce recipient validation
- support TLS
- enforce size limits
- enqueue accepted messages
- reject invalid recipients

Libraries:

- `go-smtp`

Protocols:

- SMTP
- ESMTP
- LMTP (optional)

---

## 7.2 Message Queue

Decouples SMTP ingestion from processing.

Supported backends:

- Redis
- NATS
- Kafka
- Postgres queue (default)

Queue guarantees:

- durability
- retry support
- dead-letter queues

---

## 7.3 Parser Service

Converts raw RFC 5322 email into structured data.

Libraries:

- `go-message`
- `enmime`

Extract:

- envelope sender
- envelope recipient
- headers
- subject
- text body
- HTML body
- attachments
- message-id
- thread headers
- DKIM/SPF results (future)

Output:

```
EmailEvent
```

---

## 7.4 Event Router

Routes messages to targets based on rules.

Examples:

```
support@domain.com → webhook
orders@domain.com → webhook
invoice@domain.com → storage
task+label@domain.com → webhook
```

Routing rules:

- recipient
- wildcard recipient
- plus-addressing
- header conditions
- content matching

---

## 7.5 Webhook Delivery System

Responsible for delivering parsed messages.

Features:

- retry logic
- exponential backoff
- signature verification
- timeout enforcement
- dead-letter queue

Payload example:

```
{
  "id": "msg_123",
  "timestamp": "...",
  "envelope": {
    "from": "...",
    "to": "..."
  },
  "headers": {...},
  "subject": "...",
  "text": "...",
  "html": "...",
  "attachments": [...]
}
```

---

## 7.6 Storage Layer

Stores raw and parsed email data.

Storage types:

### Raw message storage
`.eml`

### Metadata storage
SQL

### Attachments
object storage

Supported storage:

- filesystem
- S3
- Minio

---

# 8. Domain Routing

MXHook must support multiple domains.

Example:

```
domain.com
support.domain.com
mail.domain.com
```

Domain configuration includes:

- MX routing
- webhook endpoint
- allowed recipients
- wildcard rules

---

# 9. Address Handling

Supported formats:

```
support@domain.com
sales@domain.com
task+label@domain.com
anything@hooks.domain.com
```

Must support:

- plus addressing
- wildcard routes
- catch-all routes

---

# 10. Security

Required features:

- webhook signing (HMAC)
- domain ownership verification
- size limits
- rate limits
- attachment filtering
- MIME validation
- spam mitigation hooks

---

# 11. Observability

Must provide:

- message logs
- processing status
- webhook delivery logs
- retries
- dead-letter tracking

Log structure:

```
SMTP accepted
Parsed
Queued
Delivered
Failed
Retried
```

---

# 12. CLI

MXHook must include CLI tooling.

Examples:

```
mxhook start
mxhook domain add
mxhook route add
mxhook logs
mxhook replay
```

---

# 13. API

REST API required.

Examples:

```
POST /domains
POST /routes
GET /messages
GET /deliveries
POST /replay
```

---

# 14. Enterprise Edition (EE)

Additional capabilities:

### Analytics

Metrics:

- email volume
- processing latency
- delivery success rate
- attachment statistics
- domain traffic

### Monitoring

- dashboards
- alerts
- SLA metrics

### Advanced routing

- rules engine
- transformations
- script hooks

### RBAC

User roles:

- admin
- operator
- developer

---

# 15. Hosted SaaS Version

Multi-tenant infrastructure.

Features:

### Accounts

User accounts.

### Domains

Customers register domains.

### Billing

Balance system.

### Recharge

Add credits.

### Metering

Usage measured by:

- emails received
- storage
- webhook deliveries

### Limits

Plan-based limits.

---

# 16. Multi-Tenant Architecture

Tenant isolation must exist at:

- domain level
- storage level
- webhook credentials
- routing configuration

---

# 17. Scaling Strategy

SMTP servers should scale horizontally.

Architecture:

```
SMTP Nodes
     │
Load Balancer
     │
Queue
     │
Workers
```

Workers scale independently.

---

# 18. Performance Targets

MVP targets:

| Metric | Target |
|------|------|
SMTP connections | 10k concurrent |
Message throughput | 100 emails/sec |
Webhook latency | <1s |
Message size | 25MB |

---

# 19. Deployment

Supported deployment modes:

### Docker

```
docker-compose
```

### Kubernetes

Helm chart.

### Bare metal

Single binary.

---

# 20. MVP Scope

Required for v1:

SMTP receiver  
Parser  
Webhook delivery  
Domain routing  
Basic storage  
CLI  
Single-node deployment

---

# 21. Future Features

Planned roadmap:

Spam filtering  
DKIM verification  
Inbound email signatures  
Workflow engine  
GraphQL API  
Event streaming  
Webhook batching  
Inbound email sandbox testing

---

# 22. Suggested Go Package Structure

```
mxhook/
 ├ smtp/
 ├ parser/
 ├ router/
 ├ queue/
 ├ storage/
 ├ webhook/
 ├ api/
 ├ cli/
 ├ config/
 └ internal/
```

---

# 23. Suggested Technology Stack

Language:

Go

Libraries:

```
go-smtp
go-message
enmime
```

Database:

Postgres

Queue:

NATS or Redis

Storage:

S3 compatible

---

# 24. Success Metrics

Open source adoption:

- GitHub stars
- contributors
- deployments

Operational:

- webhook delivery success rate
- ingestion latency

---

# 25. Timeline

Phase 1 – SMTP ingestion MVP  
Phase 2 – parsing + routing  
Phase 3 – webhook delivery  
Phase 4 – observability  
Phase 5 – SaaS platform  

---

# 26. Non Goals

Not building:

- email sending infrastructure
- IMAP server
- full mail client
- mailing list software

MXHook is strictly **inbound email infrastructure**.

---