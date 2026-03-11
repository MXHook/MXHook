# MXHook Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build MXHook — an open-source inbound email processing platform (SMTP → parse → route → webhook) that also powers a hosted SaaS on Cloudflare.

**Architecture:** Go monorepo with package-per-concern (`smtp/`, `parser/`, `router/`, `queue/`, `webhook/`, `api/`, `cli/`). Queue is an interface with Postgres (default), Redis, and NATS adapters. SaaS runs on Cloudflare-hybrid: SMTP edge VMs feed Cloudflare Queues; Workers handle everything downstream.

**Tech Stack:** Go 1.24, `go-smtp`, `enmime`, `cobra`, `viper`, Postgres, Redis, NATS, Prometheus, Stripe, Cloudflare Workers/Queues/D1/R2.

**Naming conventions (Go):**
- Exported types/funcs: `PascalCase`
- Unexported: `camelCase`
- Acronyms all-caps: `SMTPServer`, `HTTPHandler`, `APIKey`, `DLQ`
- Booleans: `isValid`, `hasAttachments`, `canDeliver`
- Constants: group with `iota` or `const` block, no `UPPER_SNAKE_CASE` (Go convention)
- Interfaces: single-method interfaces named by method + `-er` (`Enqueuer`, `Deliverer`, `Storer`)

---

## Phase 1 — Repo Scaffolding + SMTP Ingestion Core

### Task 1.1: Initialize Go module and repo structure

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `internal/types/email.go`

**Step 1: Initialize Go module**

```bash
go mod init github.com/mxhook/mxhook
```

Expected: `go.mod` created with `module github.com/mxhook/mxhook` and `go 1.24`

**Step 2: Create package skeleton directories**

```bash
mkdir -p smtp parser router queue/postgres queue/redis queue/nats \
         storage webhook api cli config internal/types internal/lifecycle
```

**Step 3: Write core `EmailEvent` type**

Create `internal/types/email.go`:

```go
package types

import "time"

// EmailEvent is the structured representation of a received email.
type EmailEvent struct {
	ID          string
	MessageID   string
	ReceivedAt  time.Time
	EnvelopeFrom string
	EnvelopeTo  []string
	Headers     map[string][]string
	Subject     string
	TextBody    string
	HTMLBody    string
	RawPath     string // path/key to raw .eml
	Attachments []Attachment
}

// Attachment holds metadata for a parsed MIME attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	StoragePath string
}
```

**Step 4: Commit**

```bash
git add go.mod main.go internal/types/email.go
git commit -m "chore: initialize Go module and core types"
```

---

### Task 1.2: CI/CD scaffolding (Charmbracelet meta workflows)

**Files:**
- Create: `.github/workflows/build.yml`
- Create: `.github/workflows/lint.yml`
- Create: `.github/workflows/coverage.yml`
- Create: `.github/workflows/release.yml`
- Create: `.github/dependabot.yml`
- Create: `.goreleaser.yaml`
- Create: `.golangci.yml`

**Step 1: Create build workflow**

Create `.github/workflows/build.yml`:

```yaml
name: build
on: [push, pull_request]
jobs:
  build:
    uses: charmbracelet/meta/.github/workflows/build.yml@main
```

**Step 2: Create lint workflow**

Create `.github/workflows/lint.yml`:

```yaml
name: lint
on: [push, pull_request]
jobs:
  lint:
    uses: charmbracelet/meta/.github/workflows/lint.yml@main
```

**Step 3: Create coverage workflow**

Create `.github/workflows/coverage.yml`:

```yaml
name: coverage
on: [push, pull_request]
jobs:
  coverage:
    uses: charmbracelet/meta/.github/workflows/coverage.yml@main
```

**Step 4: Create release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags: ["v*.*.*"]
jobs:
  release:
    uses: charmbracelet/meta/.github/workflows/goreleaser.yml@main
```

**Step 5: Create dependabot config**

Create `.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: "/"
    schedule:
      interval: weekly
      day: monday
      time: "05:00"
      timezone: America/New_York
    labels: [dependencies]
    groups:
      all:
        patterns: ["*"]

  - package-ecosystem: github-actions
    directory: "/"
    schedule:
      interval: weekly
      day: monday
      time: "05:00"
      timezone: America/New_York
    labels: [dependencies]

  - package-ecosystem: docker
    directory: "/"
    schedule:
      interval: weekly
      day: monday
      time: "05:00"
      timezone: America/New_York
    labels: [dependencies]
```

**Step 6: Create GoReleaser config**

Create `.goreleaser.yaml`:

```yaml
# yaml-language-server: $schema=https://goreleaser.com/static/schema-pro.json
version: 2
includes:
  - from_url:
      url: charmbracelet/meta/main/goreleaser-lib.yaml
builds:
  - id: mxhook
    main: .
    binary: mxhook
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
```

**Step 7: Create golangci-lint config**

Create `.golangci.yml`:

```yaml
run:
  timeout: 5m
linters:
  enable:
    - bodyclose
    - goconst
    - gosec
    - misspell
    - nilerr
    - noctx
    - revive
    - wrapcheck
formatters:
  enable:
    - gofumpt
    - goimports
```

**Step 8: Commit**

```bash
git add .github/ .goreleaser.yaml .golangci.yml
git commit -m "chore: add CI/CD workflows, GoReleaser, and dependabot"
```

---

### Task 1.3: Queue interface

**Files:**
- Create: `queue/queue.go`
- Create: `queue/queue_test.go`

**Step 1: Write failing test**

Create `queue/queue_test.go`:

```go
package queue_test

import (
	"context"
	"testing"

	"github.com/mxhook/mxhook/queue"
)

func TestEnqueuerInterface(t *testing.T) {
	// Compile-time check: Enqueuer interface is satisfied
	var _ queue.Enqueuer = (*queue.NoopEnqueuer)(nil)
}

func TestNoopEnqueue(t *testing.T) {
	q := &queue.NoopEnqueuer{}
	err := q.Enqueue(context.Background(), queue.Message{
		ID:   "test-1",
		Body: []byte("raw email body"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./queue/...
```

Expected: FAIL — `queue.Enqueuer` and `queue.NoopEnqueuer` undefined

**Step 3: Write minimal implementation**

Create `queue/queue.go`:

```go
package queue

import "context"

// Message is the unit of work passed through the queue.
type Message struct {
	ID   string
	Body []byte
	Meta map[string]string
}

// Enqueuer is the interface for adding messages to the queue.
type Enqueuer interface {
	Enqueue(ctx context.Context, msg Message) error
}

// Dequeuer is the interface for consuming messages from the queue.
type Dequeuer interface {
	Dequeue(ctx context.Context) (<-chan Message, error)
	Ack(ctx context.Context, id string) error
	Nack(ctx context.Context, id string, reason string) error
}

// NoopEnqueuer discards all messages. Useful in tests.
type NoopEnqueuer struct{}

func (n *NoopEnqueuer) Enqueue(_ context.Context, _ Message) error {
	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./queue/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add queue/
git commit -m "feat(queue): add Enqueuer/Dequeuer interfaces and NoopEnqueuer"
```

---

### Task 1.4: Postgres queue adapter

**Files:**
- Create: `queue/postgres/adapter.go`
- Create: `queue/postgres/adapter_test.go`
- Create: `queue/postgres/schema.sql`

**Step 1: Write failing test**

Create `queue/postgres/adapter_test.go`:

```go
package postgres_test

import (
	"context"
	"testing"

	"github.com/mxhook/mxhook/queue"
	pgqueue "github.com/mxhook/mxhook/queue/postgres"
)

func TestAdapter_ImplementsEnqueuer(t *testing.T) {
	var _ queue.Enqueuer = (*pgqueue.Adapter)(nil)
}

func TestAdapter_ImplementsDequeuer(t *testing.T) {
	var _ queue.Dequeuer = (*pgqueue.Adapter)(nil)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./queue/postgres/...
```

Expected: FAIL — `pgqueue.Adapter` undefined

**Step 3: Create schema**

Create `queue/postgres/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS mxhook_queue (
    id          TEXT PRIMARY KEY,
    body        BYTEA NOT NULL,
    meta        JSONB NOT NULL DEFAULT '{}',
    state       TEXT NOT NULL DEFAULT 'pending', -- pending, processing, acked, nacked
    attempts    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_mxhook_queue_state ON mxhook_queue (state, created_at);
```

**Step 4: Write minimal adapter**

Create `queue/postgres/adapter.go`:

```go
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/mxhook/mxhook/queue"
)

// Adapter implements queue.Enqueuer and queue.Dequeuer using Postgres.
type Adapter struct {
	db *sql.DB
}

// New creates a new Postgres queue Adapter.
func New(db *sql.DB) *Adapter {
	return &Adapter{db: db}
}

func (a *Adapter) Enqueue(ctx context.Context, msg queue.Message) error {
	meta, err := json.Marshal(msg.Meta)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx,
		`INSERT INTO mxhook_queue (id, body, meta) VALUES ($1, $2, $3)`,
		msg.ID, msg.Body, meta,
	)
	return err
}

func (a *Adapter) Dequeue(ctx context.Context) (<-chan queue.Message, error) {
	ch := make(chan queue.Message)
	go func() {
		defer close(ch)
		for {
			msg, err := a.poll(ctx)
			if err != nil || msg == nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			select {
			case ch <- *msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (a *Adapter) poll(ctx context.Context) (*queue.Message, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var msg queue.Message
	var meta []byte
	err = tx.QueryRowContext(ctx, `
		UPDATE mxhook_queue SET state='processing', locked_until=NOW()+interval '30s', updated_at=NOW()
		WHERE id = (
			SELECT id FROM mxhook_queue
			WHERE state='pending' OR (state='processing' AND locked_until < NOW())
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING id, body, meta
	`).Scan(&msg.ID, &msg.Body, &meta)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(meta, &msg.Meta)
	tx.Commit()
	return &msg, nil
}

func (a *Adapter) Ack(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx,
		`UPDATE mxhook_queue SET state='acked', updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (a *Adapter) Nack(ctx context.Context, id string, reason string) error {
	_, err := a.db.ExecContext(ctx,
		`UPDATE mxhook_queue SET state='nacked', meta=meta||$2, updated_at=NOW() WHERE id=$1`,
		id, `{"nack_reason":"`+reason+`"}`)
	return err
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./queue/postgres/... -v
```

Expected: PASS (compile-time interface checks)

**Step 6: Add dependency**

```bash
go get github.com/lib/pq
go mod tidy
```

**Step 7: Commit**

```bash
git add queue/postgres/ go.mod go.sum
git commit -m "feat(queue/postgres): add Postgres queue adapter"
```

---

### Task 1.5: SMTP receiver

**Files:**
- Create: `smtp/server.go`
- Create: `smtp/server_test.go`
- Create: `smtp/handler.go`

**Step 1: Add go-smtp dependency**

```bash
go get github.com/emersion/go-smtp
go mod tidy
```

**Step 2: Write failing test**

Create `smtp/server_test.go`:

```go
package smtp_test

import (
	"testing"

	mxsmtp "github.com/mxhook/mxhook/smtp"
)

func TestServer_StartsAndAcceptsConfig(t *testing.T) {
	cfg := mxsmtp.Config{
		ListenAddr:   "127.0.0.1:0",
		Domain:       "example.com",
		MaxMessageSize: 25 * 1024 * 1024, // 25MB
	}
	srv := mxsmtp.NewServer(cfg, nil)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./smtp/...
```

Expected: FAIL — `mxsmtp.Config` and `mxsmtp.NewServer` undefined

**Step 4: Write SMTP handler**

Create `smtp/handler.go`:

```go
package smtp

import (
	"context"
	"io"
	"strings"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/mxhook/mxhook/queue"

	"github.com/google/uuid"
)

// session handles a single SMTP session.
type session struct {
	enqueuer     queue.Enqueuer
	allowedDomains map[string]bool
	envelopeFrom string
	envelopeTo   []string
}

func (s *session) AuthPlain(_ string, _ string, _ string) error {
	return nil // no auth for inbound
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.envelopeFrom = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	parts := strings.SplitN(to, "@", 2)
	if len(parts) != 2 {
		return gosmtp.ErrBadParams
	}
	domain := strings.ToLower(parts[1])
	if !s.allowedDomains[domain] {
		return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 1, 1}, Message: "unknown recipient domain"}
	}
	s.envelopeTo = append(s.envelopeTo, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	body, err := io.ReadAll(io.LimitReader(r, 25*1024*1024))
	if err != nil {
		return err
	}
	msg := queue.Message{
		ID:   uuid.NewString(),
		Body: body,
		Meta: map[string]string{
			"envelope_from": s.envelopeFrom,
			"received_at":   time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	for i, to := range s.envelopeTo {
		msg.Meta[fmt.Sprintf("envelope_to_%d", i)] = to
	}
	return s.enqueuer.Enqueue(context.Background(), msg)
}

func (s *session) Reset() {
	s.envelopeFrom = ""
	s.envelopeTo = nil
}

func (s *session) Logout() error { return nil }
```

**Step 5: Write SMTP server**

Create `smtp/server.go`:

```go
package smtp

import (
	gosmtp "github.com/emersion/go-smtp"
	"github.com/mxhook/mxhook/queue"
)

// Config holds SMTP server configuration.
type Config struct {
	ListenAddr     string
	Domain         string
	MaxMessageSize int64
	AllowedDomains []string
}

// Server wraps go-smtp with MXHook configuration.
type Server struct {
	inner    *gosmtp.Server
	enqueuer queue.Enqueuer
}

// NewServer creates a configured SMTP server.
func NewServer(cfg Config, enqueuer queue.Enqueuer) *Server {
	allowed := make(map[string]bool)
	for _, d := range cfg.AllowedDomains {
		allowed[d] = true
	}
	if cfg.Domain != "" {
		allowed[cfg.Domain] = true
	}

	backend := &backend{
		enqueuer:       enqueuer,
		allowedDomains: allowed,
	}

	s := gosmtp.NewServer(backend)
	s.Addr = cfg.ListenAddr
	s.Domain = cfg.Domain
	s.MaxMessageBytes = cfg.MaxMessageSize
	s.AllowInsecureAuth = true // TLS handled at load balancer in prod

	return &Server{inner: s, enqueuer: enqueuer}
}

// ListenAndServe starts the SMTP server.
func (s *Server) ListenAndServe() error {
	return s.inner.ListenAndServe()
}

// Close shuts down the server.
func (s *Server) Close() error {
	return s.inner.Close()
}

type backend struct {
	enqueuer       queue.Enqueuer
	allowedDomains map[string]bool
}

func (b *backend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{
		enqueuer:       b.enqueuer,
		allowedDomains: b.allowedDomains,
	}, nil
}
```

**Step 6: Add uuid dependency**

```bash
go get github.com/google/uuid
go mod tidy
```

**Step 7: Run test to verify it passes**

```bash
go test ./smtp/... -v
```

Expected: PASS

**Step 8: Commit**

```bash
git add smtp/ go.mod go.sum
git commit -m "feat(smtp): add SMTP server with recipient validation and enqueue"
```

---

### Task 1.6: Storage interface + filesystem adapter

**Files:**
- Create: `storage/storage.go`
- Create: `storage/fs/adapter.go`
- Create: `storage/fs/adapter_test.go`

**Step 1: Write failing test**

Create `storage/fs/adapter_test.go`:

```go
package fs_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mxhook/mxhook/storage"
	fsstore "github.com/mxhook/mxhook/storage/fs"
)

func TestFSAdapter_StoreAndRetrieve(t *testing.T) {
	dir := t.TempDir()
	adapter := fsstore.New(dir)

	content := []byte("raw eml content")
	key, err := adapter.Store(context.Background(), "msg-1", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	r, err := adapter.Retrieve(context.Background(), key)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	defer r.Close()

	got, _ := io.ReadAll(r)
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestFSAdapter_ImplementsStorer(t *testing.T) {
	var _ storage.Storer = (*fsstore.Adapter)(nil)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./storage/fs/...
```

Expected: FAIL

**Step 3: Define storage interface**

Create `storage/storage.go`:

```go
package storage

import "context"
import "io"

// Storer persists raw email data and attachments.
type Storer interface {
	Store(ctx context.Context, id string, r io.Reader) (key string, err error)
	Retrieve(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
```

**Step 4: Write filesystem adapter**

Create `storage/fs/adapter.go`:

```go
package fs

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// Adapter stores files on the local filesystem.
type Adapter struct {
	baseDir string
}

// New creates a filesystem Adapter rooted at baseDir.
func New(baseDir string) *Adapter {
	return &Adapter{baseDir: baseDir}
}

func (a *Adapter) Store(_ context.Context, id string, r io.Reader) (string, error) {
	path := filepath.Join(a.baseDir, id+".eml")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	return path, nil
}

func (a *Adapter) Retrieve(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(key)
}

func (a *Adapter) Delete(_ context.Context, key string) error {
	return os.Remove(key)
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./storage/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add storage/
git commit -m "feat(storage): add Storer interface and filesystem adapter"
```

---

### Task 1.7: Docker Compose for local dev

**Files:**
- Create: `docker-compose.yml`
- Create: `Dockerfile`

**Step 1: Write Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mxhook .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /mxhook /usr/local/bin/mxhook
ENTRYPOINT ["/usr/local/bin/mxhook"]
```

**Step 2: Write Docker Compose**

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: mxhook
      POSTGRES_USER: mxhook
      POSTGRES_PASSWORD: mxhook
    ports: ["5432:5432"]
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./queue/postgres/schema.sql:/docker-entrypoint-initdb.d/01-queue.sql

  mxhook:
    build: .
    depends_on: [postgres]
    ports:
      - "2525:2525"  # SMTP
      - "8080:8080"  # API
    environment:
      MXHOOK_SMTP_ADDR: "0.0.0.0:2525"
      MXHOOK_QUEUE_BACKEND: postgres
      MXHOOK_DATABASE_URL: "postgres://mxhook:mxhook@postgres:5432/mxhook?sslmode=disable"

volumes:
  pgdata:
```

**Step 3: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "chore: add Dockerfile and Docker Compose for local dev"
```

---

## Phase 2 — Parser Service

### Task 2.1: Email parser (enmime)

**Files:**
- Create: `parser/parser.go`
- Create: `parser/parser_test.go`

**Step 1: Add enmime dependency**

```bash
go get github.com/jhillyerd/enmime
go mod tidy
```

**Step 2: Write failing test**

Create `parser/parser_test.go`:

```go
package parser_test

import (
	"strings"
	"testing"

	"github.com/mxhook/mxhook/parser"
)

const sampleEML = `From: sender@example.com
To: recipient@example.com
Subject: Test email
Message-ID: <test-123@example.com>
MIME-Version: 1.0
Content-Type: text/plain

Hello, world!
`

func TestParser_ParsesBasicEmail(t *testing.T) {
	p := parser.New()
	event, err := p.Parse("msg-1", "sender@example.com", []string{"recipient@example.com"}, strings.NewReader(sampleEML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if event.Subject != "Test email" {
		t.Errorf("Subject: got %q, want %q", event.Subject, "Test email")
	}
	if event.TextBody != "Hello, world!\n" {
		t.Errorf("TextBody: got %q", event.TextBody)
	}
	if event.MessageID != "<test-123@example.com>" {
		t.Errorf("MessageID: got %q", event.MessageID)
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./parser/...
```

Expected: FAIL

**Step 4: Write parser**

Create `parser/parser.go`:

```go
package parser

import (
	"fmt"
	"io"
	"time"

	"github.com/jhillyerd/enmime"
	"github.com/mxhook/mxhook/internal/types"
)

// Parser converts raw RFC 5322 email into an EmailEvent.
type Parser struct{}

// New creates a Parser.
func New() *Parser { return &Parser{} }

// Parse reads a raw email from r and returns a structured EmailEvent.
func (p *Parser) Parse(id, envelopeFrom string, envelopeTo []string, r io.Reader) (*types.EmailEvent, error) {
	env, err := enmime.ReadEnvelope(r)
	if err != nil {
		return nil, fmt.Errorf("parser: read envelope: %w", err)
	}

	event := &types.EmailEvent{
		ID:           id,
		MessageID:    env.GetHeader("Message-ID"),
		ReceivedAt:   time.Now().UTC(),
		EnvelopeFrom: envelopeFrom,
		EnvelopeTo:   envelopeTo,
		Subject:      env.GetHeader("Subject"),
		TextBody:     env.Text,
		HTMLBody:     env.HTML,
		Headers:      headersToMap(env),
	}

	for _, att := range env.Attachments {
		data, err := io.ReadAll(att.Content)
		if err != nil {
			continue
		}
		event.Attachments = append(event.Attachments, types.Attachment{
			Filename:    att.FileName,
			ContentType: att.ContentType,
			Size:        int64(len(data)),
		})
	}

	return event, nil
}

func headersToMap(env *enmime.Envelope) map[string][]string {
	h := make(map[string][]string)
	for _, k := range []string{"From", "To", "CC", "Reply-To", "Date", "References", "In-Reply-To"} {
		if v := env.GetHeader(k); v != "" {
			h[k] = []string{v}
		}
	}
	return h
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./parser/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add parser/ go.mod go.sum
git commit -m "feat(parser): add MIME email parser using enmime"
```

---

### Task 2.2: Worker pool (pulls from queue, parses, stores)

**Files:**
- Create: `parser/worker.go`
- Create: `parser/worker_test.go`

**Step 1: Write failing test**

Create `parser/worker_test.go`:

```go
package parser_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/parser"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/storage"
)

type captureStore struct {
	stored map[string]string
}

func (c *captureStore) Store(_ context.Context, id string, r io.Reader) (string, error) {
	b, _ := io.ReadAll(r)
	c.stored[id] = string(b)
	return "key:" + id, nil
}
func (c *captureStore) Retrieve(_ context.Context, _ string) (io.ReadCloser, error) { return nil, nil }
func (c *captureStore) Delete(_ context.Context, _ string) error { return nil }

type captureEvents struct {
	events []*types.EmailEvent
}
func (c *captureEvents) Save(_ context.Context, e *types.EmailEvent) error {
	c.events = append(c.events, e)
	return nil
}

func TestWorker_ProcessesMessage(t *testing.T) {
	ch := make(chan queue.Message, 1)
	ch <- queue.Message{
		ID:   "msg-1",
		Body: []byte(sampleEML),
		Meta: map[string]string{
			"envelope_from": "sender@example.com",
			"envelope_to_0": "recipient@example.com",
		},
	}
	close(ch)

	store := &captureStore{stored: make(map[string]string)}
	events := &captureEvents{}
	p := parser.New()
	w := parser.NewWorker(p, store, events)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w.Run(ctx, ch)

	if len(events.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events.events))
	}
	if events.events[0].Subject != "Test email" {
		t.Errorf("unexpected subject: %q", events.events[0].Subject)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./parser/... -run TestWorker
```

Expected: FAIL

**Step 3: Define EventSaver interface and write worker**

Create `parser/worker.go`:

```go
package parser

import (
	"context"
	"bytes"
	"log/slog"
	"strings"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/storage"
)

// EventSaver persists a parsed EmailEvent.
type EventSaver interface {
	Save(ctx context.Context, event *types.EmailEvent) error
}

// Worker consumes raw messages from the queue and parses them.
type Worker struct {
	parser *Parser
	store  storage.Storer
	saver  EventSaver
}

// NewWorker creates a Worker.
func NewWorker(p *Parser, store storage.Storer, saver EventSaver) *Worker {
	return &Worker{parser: p, store: store, saver: saver}
}

// Run processes messages from ch until ctx is done or ch is closed.
func (w *Worker) Run(ctx context.Context, ch <-chan queue.Message) {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := w.process(ctx, msg); err != nil {
				slog.Error("worker: process failed", "id", msg.ID, "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (w *Worker) process(ctx context.Context, msg queue.Message) error {
	from := msg.Meta["envelope_from"]
	var to []string
	for i := 0; ; i++ {
		v, ok := msg.Meta[strings.Join([]string{"envelope_to_", string(rune('0'+i))}, "")]
		if !ok { break }
		to = append(to, v)
	}

	rawKey, err := w.store.Store(ctx, msg.ID, bytes.NewReader(msg.Body))
	if err != nil {
		return err
	}

	event, err := w.parser.Parse(msg.ID, from, to, bytes.NewReader(msg.Body))
	if err != nil {
		return err
	}
	event.RawPath = rawKey

	return w.saver.Save(ctx, event)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./parser/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add parser/worker.go parser/worker_test.go
git commit -m "feat(parser): add worker pool for queue consumption and parsing"
```

---

## Phase 3 — Event Router

### Task 3.1: Routing rule types and matcher

**Files:**
- Create: `router/rule.go`
- Create: `router/matcher.go`
- Create: `router/matcher_test.go`

**Step 1: Write failing test**

Create `router/matcher_test.go`:

```go
package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/router"
)

func TestMatcher_ExactAddress(t *testing.T) {
	rule := router.Rule{
		RecipientPattern: "support@example.com",
		Target:           router.TargetWebhook,
		WebhookURL:       "https://example.com/hook",
	}
	m := router.NewMatcher([]router.Rule{rule})

	matched, ok := m.Match("support@example.com")
	if !ok {
		t.Fatal("expected match")
	}
	if matched.WebhookURL != "https://example.com/hook" {
		t.Errorf("wrong webhook URL: %q", matched.WebhookURL)
	}
}

func TestMatcher_WildcardDomain(t *testing.T) {
	rule := router.Rule{RecipientPattern: "*@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("anything@example.com")
	if !ok {
		t.Fatal("expected wildcard match")
	}
}

func TestMatcher_PlusAddressing(t *testing.T) {
	rule := router.Rule{RecipientPattern: "task@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("task+label@example.com")
	if !ok {
		t.Fatal("expected plus-address match")
	}
}

func TestMatcher_NoMatch(t *testing.T) {
	rule := router.Rule{RecipientPattern: "support@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("other@example.com")
	if ok {
		t.Fatal("expected no match")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./router/...
```

Expected: FAIL

**Step 3: Write rule types**

Create `router/rule.go`:

```go
package router

// Target defines where a matched email is delivered.
type Target string

const (
	TargetWebhook Target = "webhook"
	TargetQueue   Target = "queue"
	TargetStorage Target = "storage"
)

// Rule defines a routing rule for an email recipient.
type Rule struct {
	ID               string
	Domain           string
	RecipientPattern string // exact, wildcard (*@domain.com), or plus-stripped (base@domain.com)
	Target           Target
	WebhookURL       string
	WebhookSecret    string
	IsCatchAll       bool
	Priority         int
}
```

**Step 4: Write matcher**

Create `router/matcher.go`:

```go
package router

import "strings"

// Matcher routes recipient addresses to Rules.
type Matcher struct {
	rules []Rule
}

// NewMatcher creates a Matcher from a set of rules.
func NewMatcher(rules []Rule) *Matcher {
	return &Matcher{rules: rules}
}

// Match returns the first matching Rule for a recipient address.
func (m *Matcher) Match(recipient string) (Rule, bool) {
	normalized := normalizeRecipient(recipient)
	for _, r := range m.rules {
		if matches(r.RecipientPattern, normalized) {
			return r, true
		}
	}
	return Rule{}, false
}

// normalizeRecipient strips plus-addressing: task+label@domain.com → task@domain.com
func normalizeRecipient(addr string) string {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return addr
	}
	local := parts[0]
	if i := strings.Index(local, "+"); i >= 0 {
		local = local[:i]
	}
	return local + "@" + parts[1]
}

func matches(pattern, addr string) bool {
	if pattern == addr {
		return true
	}
	// Wildcard: *@domain.com
	if strings.HasPrefix(pattern, "*@") {
		domain := pattern[2:]
		return strings.HasSuffix(addr, "@"+domain)
	}
	return false
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./router/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add router/
git commit -m "feat(router): add routing rule types and address matcher"
```

---

## Phase 4 — Webhook Delivery

### Task 4.1: Webhook payload signing and delivery

**Files:**
- Create: `webhook/delivery.go`
- Create: `webhook/delivery_test.go`
- Create: `webhook/signer.go`

**Step 1: Write failing test**

Create `webhook/delivery_test.go`:

```go
package webhook_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/webhook"
)

func TestDelivery_SignsAndPosts(t *testing.T) {
	var received []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-MXHook-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{
		MaxAttempts: 1,
		TimeoutSecs: 5,
	})

	event := &types.EmailEvent{
		ID:      "msg-1",
		Subject: "Test",
	}
	err := d.Deliver(context.Background(), webhook.Endpoint{
		URL:    srv.URL,
		Secret: "mysecret",
	}, event)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(received) == 0 {
		t.Fatal("no payload received")
	}
	if gotSig == "" {
		t.Fatal("expected X-MXHook-Signature header")
	}
	// Verify signature
	if !webhook.VerifySignature(received, "mysecret", gotSig) {
		t.Error("signature verification failed")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./webhook/...
```

Expected: FAIL

**Step 3: Write signer**

Create `webhook/signer.go`:

```go
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Sign returns the HMAC-SHA256 hex signature of payload using secret.
func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature returns true if sig matches the expected HMAC of payload.
func VerifySignature(payload []byte, secret, sig string) bool {
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(sig), []byte(expected))
}
```

**Step 4: Write deliverer**

Create `webhook/delivery.go`:

```go
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mxhook/mxhook/internal/types"
)

// Endpoint is a webhook delivery target.
type Endpoint struct {
	URL    string
	Secret string
}

// Config controls delivery behavior.
type Config struct {
	MaxAttempts int
	TimeoutSecs int
}

// Deliverer sends EmailEvents to webhook endpoints.
type Deliverer struct {
	cfg    Config
	client *http.Client
}

// NewDeliverer creates a Deliverer.
func NewDeliverer(cfg Config) *Deliverer {
	return &Deliverer{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(cfg.TimeoutSecs) * time.Second},
	}
}

// Deliver sends event to endpoint with HMAC signing and retry.
func (d *Deliverer) Deliver(ctx context.Context, ep Endpoint, event *types.EmailEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	sig := Sign(payload, ep.Secret)

	var lastErr error
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		if err := d.post(ctx, ep.URL, sig, payload); err != nil {
			lastErr = err
			wait := time.Duration(attempt*attempt) * time.Second
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("webhook: all %d attempts failed: %w", d.cfg.MaxAttempts, lastErr)
}

func (d *Deliverer) post(ctx context.Context, url, sig string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MXHook-Signature", sig)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: server returned %d", resp.StatusCode)
	}
	return nil
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./webhook/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add webhook/
git commit -m "feat(webhook): add signed webhook delivery with exponential backoff"
```

---

## Phase 5 — Observability

### Task 5.1: Message lifecycle log

**Files:**
- Create: `internal/lifecycle/lifecycle.go`
- Create: `internal/lifecycle/lifecycle_test.go`
- Create: `internal/lifecycle/schema.sql`

**Step 1: Write failing test**

Create `internal/lifecycle/lifecycle_test.go`:

```go
package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/lifecycle"
)

func TestLogger_RecordsTransitions(t *testing.T) {
	var captured []lifecycle.Event
	store := &fakeStore{fn: func(e lifecycle.Event) { captured = append(captured, e) }}
	logger := lifecycle.NewLogger(store)

	logger.Record(context.Background(), lifecycle.Event{
		MessageID: "msg-1",
		State:     lifecycle.StateQueued,
		At:        time.Now(),
	})

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	if captured[0].State != lifecycle.StateQueued {
		t.Errorf("wrong state: %v", captured[0].State)
	}
}

type fakeStore struct{ fn func(lifecycle.Event) }
func (f *fakeStore) Save(_ context.Context, e lifecycle.Event) error {
	f.fn(e)
	return nil
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/lifecycle/...
```

Expected: FAIL

**Step 3: Write lifecycle package**

Create `internal/lifecycle/lifecycle.go`:

```go
package lifecycle

import (
	"context"
	"time"
)

// State represents a message's lifecycle state.
type State string

const (
	StateAccepted  State = "accepted"
	StateQueued    State = "queued"
	StateParsed    State = "parsed"
	StateRouted    State = "routed"
	StateDelivered State = "delivered"
	StateFailed    State = "failed"
	StateRetried   State = "retried"
	StateDLQ       State = "dlq"
)

// Event is a single lifecycle transition.
type Event struct {
	MessageID string
	State     State
	At        time.Time
	Detail    string
}

// EventStore persists lifecycle events.
type EventStore interface {
	Save(ctx context.Context, e Event) error
}

// Logger records message lifecycle transitions.
type Logger struct {
	store EventStore
}

// NewLogger creates a Logger.
func NewLogger(store EventStore) *Logger {
	return &Logger{store: store}
}

// Record saves a lifecycle transition.
func (l *Logger) Record(ctx context.Context, e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	_ = l.store.Save(ctx, e) // log error via slog, don't block callers
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/lifecycle/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/lifecycle/
git commit -m "feat(lifecycle): add message lifecycle state logger"
```

---

### Task 5.2: Prometheus metrics

**Files:**
- Create: `internal/metrics/metrics.go`

**Step 1: Add Prometheus dependency**

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
go mod tidy
```

**Step 2: Write metrics**

Create `internal/metrics/metrics.go`:

```go
package metrics

import "github.com/prometheus/client_golang/prometheus"
import "github.com/prometheus/client_golang/prometheus/promauto"

var (
	// MessagesReceived counts SMTP-accepted messages.
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_messages_received_total",
		Help: "Total emails accepted by SMTP receiver.",
	})

	// ParseErrors counts failed parse attempts.
	ParseErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mxhook_parse_errors_total",
		Help: "Total email parse failures.",
	})

	// WebhookDeliveries tracks delivery outcomes.
	WebhookDeliveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mxhook_webhook_deliveries_total",
		Help: "Total webhook delivery attempts by outcome.",
	}, []string{"outcome"}) // success, failure, dlq

	// DeliveryLatency measures end-to-end ingestion-to-delivery latency.
	DeliveryLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mxhook_delivery_latency_seconds",
		Help:    "End-to-end latency from SMTP accept to webhook delivery.",
		Buckets: prometheus.DefBuckets,
	})
)
```

**Step 3: Commit**

```bash
git add internal/metrics/
git commit -m "feat(metrics): add Prometheus counters and latency histogram"
```

---

## Phase 6 — CLI

### Task 6.1: cobra/viper CLI scaffold

**Files:**
- Create: `cli/root.go`
- Create: `cli/start.go`
- Create: `cli/domain.go`
- Create: `cli/route.go`
- Create: `cli/logs.go`
- Create: `cli/replay.go`
- Modify: `main.go`

**Step 1: Add cobra and viper**

```bash
go get github.com/spf13/cobra
go get github.com/spf13/viper
go mod tidy
```

**Step 2: Write root command**

Create `cli/root.go`:

```go
package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Root is the top-level cobra command.
var Root = &cobra.Command{
	Use:   "mxhook",
	Short: "MXHook — inbound email processing platform",
}

func init() {
	cobra.OnInitialize(initConfig)
	Root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: mxhook.yaml)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("mxhook")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/mxhook/")
	}
	viper.SetEnvPrefix("MXHOOK")
	viper.AutomaticEnv()
	viper.ReadInConfig()
}
```

**Step 3: Write start command**

Create `cli/start.go`:

```go
package cli

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start MXHook services",
	RunE:  runStart,
}

func init() {
	Root.AddCommand(startCmd)
	startCmd.Flags().String("smtp-addr", "0.0.0.0:25", "SMTP listen address")
	startCmd.Flags().String("api-addr", "0.0.0.0:8080", "API listen address")
}

func runStart(cmd *cobra.Command, _ []string) error {
	// wired in Phase 7 when API is ready
	cmd.Println("Starting MXHook...")
	return nil
}
```

**Step 4: Write main.go**

Modify `main.go`:

```go
package main

import (
	"os"

	"github.com/mxhook/mxhook/cli"
)

func main() {
	if err := cli.Root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

**Step 5: Build and verify**

```bash
go build -o mxhook . && ./mxhook --help
```

Expected: cobra help output with `start` subcommand

**Step 6: Commit**

```bash
git add cli/ main.go go.mod go.sum
git commit -m "feat(cli): add cobra/viper CLI scaffold with start command"
```

---

## Phase 7 — REST API

### Task 7.1: HTTP API with domain and route endpoints

**Files:**
- Create: `api/server.go`
- Create: `api/handlers.go`
- Create: `api/server_test.go`

**Step 1: Add net/http router (stdlib, no extra dep)**

**Step 2: Write failing test**

Create `api/server_test.go`:

```go
package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

func TestHealth_Returns200(t *testing.T) {
	srv := api.NewServer(api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("unexpected body: %v", body)
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./api/...
```

Expected: FAIL

**Step 4: Write API server**

Create `api/server.go`:

```go
package api

import (
	"net/http"
)

// Config holds API server configuration.
type Config struct {
	ListenAddr string
	APIKey     string
}

// Server is the MXHook REST API server.
type Server struct {
	cfg  Config
	mux  *http.ServeMux
}

// NewServer creates an API Server.
func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /domains", s.requireAPIKey(s.handleCreateDomain))
	s.mux.HandleFunc("POST /routes", s.requireAPIKey(s.handleCreateRoute))
	s.mux.HandleFunc("GET /messages", s.requireAPIKey(s.handleListMessages))
	s.mux.HandleFunc("GET /messages/{id}", s.requireAPIKey(s.handleGetMessage))
	s.mux.HandleFunc("POST /replay/{id}", s.requireAPIKey(s.handleReplay))
}
```

Create `api/handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) handleGetMessage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (s *Server) handleReplay(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey != "" && r.Header.Get("X-API-Key") != s.cfg.APIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./api/... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add api/
git commit -m "feat(api): add REST API with health, domains, routes, messages, replay"
```

---

## Phase 8 — Enterprise Edition (EE)

### Task 8.1: License gate

**Files:**
- Create: `ee/license.go`
- Create: `ee/license_test.go`

**Step 1: Write failing test**

Create `ee/license_test.go`:

```go
package ee_test

import (
	"testing"

	"github.com/mxhook/mxhook/ee"
)

func TestLicense_IsValidWithKey(t *testing.T) {
	l := ee.ParseLicense("valid-ee-key-abc123")
	if !l.IsValid() {
		t.Error("expected valid license")
	}
}

func TestLicense_IsInvalidWithoutKey(t *testing.T) {
	l := ee.ParseLicense("")
	if l.IsValid() {
		t.Error("expected invalid license")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./ee/...
```

Expected: FAIL

**Step 3: Write license gate**

Create `ee/license.go`:

```go
// Package ee contains Enterprise Edition features.
// Features are gated behind a valid license key.
package ee

// License represents an EE license.
type License struct {
	key string
}

// ParseLicense creates a License from a key string.
func ParseLicense(key string) License {
	return License{key: key}
}

// IsValid returns true if the license key is non-empty.
// Production: replace with cryptographic verification.
func (l License) IsValid() bool {
	return l.key != ""
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./ee/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add ee/
git commit -m "feat(ee): add license gate for Enterprise Edition features"
```

---

### Task 8.2: OSS release prep

**Files:**
- Create: `LICENSE`
- Create: `README.md`
- Create: `.github/ISSUE_TEMPLATE/bug_report.yml`
- Create: `.github/ISSUE_TEMPLATE/config.yml`
- Modify: `README.md`

**Step 1: Add Apache 2.0 LICENSE**

```bash
curl -s https://www.apache.org/licenses/LICENSE-2.0.txt > LICENSE
```

**Step 2: Write README scaffold**

README must include: badges (Release, GoDoc, Build), description, quickstart via Docker Compose, CLI reference, contributing link.

**Step 3: Add issue templates**

Create `.github/ISSUE_TEMPLATE/config.yml`:

```yaml
blank_issues_enabled: true
```

**Step 4: Commit**

```bash
git add LICENSE README.md .github/ISSUE_TEMPLATE/
git commit -m "chore: add LICENSE, README, and issue templates for OSS release"
```

**Step 5: Tag v1.0.0**

```bash
git tag v1.0.0
git push origin main --tags
```

---

## Phase 9 — SaaS Infrastructure (Cloudflare-hybrid)

### Task 9.1: Cloudflare Worker for parser/router/delivery

**Files:**
- Create: `saas/worker/src/index.ts`
- Create: `saas/worker/wrangler.toml`
- Create: `saas/worker/package.json`

**Step 1: Initialize Cloudflare Worker**

```bash
cd saas/worker
npm create cloudflare@latest . -- --type=hello-world --no-deploy
```

**Step 2: Write wrangler.toml**

Create `saas/worker/wrangler.toml`:

```toml
name = "mxhook-worker"
main = "src/index.ts"
compatibility_date = "2025-01-01"

[[queues.consumers]]
queue = "mxhook-inbound"
max_batch_size = 10
max_batch_timeout = 5

[[queues.producers]]
queue = "mxhook-dlq"
binding = "DLQ"

[[d1_databases]]
binding = "DB"
database_name = "mxhook"
database_id = "FILL_IN_AFTER_CF_D1_CREATE"

[[r2_buckets]]
binding = "RAW_STORE"
bucket_name = "mxhook-raw"
```

**Step 3: Write worker entry**

Create `saas/worker/src/index.ts`:

```typescript
export interface Env {
  DB: D1Database;
  RAW_STORE: R2Bucket;
  DLQ: Queue;
}

export default {
  async queue(batch: MessageBatch<ArrayBuffer>, env: Env): Promise<void> {
    for (const msg of batch.messages) {
      try {
        await processMessage(msg.body, env);
        msg.ack();
      } catch (err) {
        console.error("processing failed", err);
        msg.retry();
      }
    }
  },
};

async function processMessage(raw: ArrayBuffer, env: Env): Promise<void> {
  const id = crypto.randomUUID();
  // Store raw .eml in R2
  await env.RAW_STORE.put(`${id}.eml`, raw);
  // TODO: parse, route, deliver (wired in subsequent tasks)
}
```

**Step 4: Commit**

```bash
git add saas/worker/
git commit -m "feat(saas): add Cloudflare Worker for queue-driven email processing"
```

---

### Task 9.2: Create Cloudflare D1 schema

**Files:**
- Create: `saas/worker/schema.sql`

**Step 1: Write D1 schema**

Create `saas/worker/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    plan TEXT NOT NULL DEFAULT 'free',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS domains (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    domain TEXT NOT NULL UNIQUE,
    verified INTEGER NOT NULL DEFAULT 0,
    verification_token TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS routes (
    id TEXT PRIMARY KEY,
    domain_id TEXT NOT NULL REFERENCES domains(id),
    recipient_pattern TEXT NOT NULL,
    target TEXT NOT NULL,
    webhook_url TEXT,
    webhook_secret TEXT,
    is_catch_all INTEGER NOT NULL DEFAULT 0,
    priority INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    envelope_from TEXT,
    envelope_to TEXT,
    subject TEXT,
    raw_r2_key TEXT,
    received_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued'
);

CREATE TABLE IF NOT EXISTS delivery_logs (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES messages(id),
    attempt INTEGER NOT NULL,
    outcome TEXT NOT NULL,
    status_code INTEGER,
    latency_ms INTEGER,
    attempted_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

**Step 2: Apply schema**

```bash
npx wrangler d1 create mxhook
npx wrangler d1 execute mxhook --file=schema.sql
```

**Step 3: Commit**

```bash
git add saas/worker/schema.sql
git commit -m "feat(saas/d1): add D1 schema for tenants, domains, routes, messages"
```

---

## Phase 10 — Multi-Tenancy

### Task 10.1: Tenant isolation middleware

**Files:**
- Create: `saas/worker/src/middleware/tenant.ts`

**Step 1: Write tenant resolver**

Create `saas/worker/src/middleware/tenant.ts`:

```typescript
export interface Tenant {
  id: string;
  plan: string;
}

export async function resolveTenant(
  domain: string,
  db: D1Database
): Promise<Tenant | null> {
  const row = await db
    .prepare(
      `SELECT t.id, t.plan FROM tenants t
       JOIN domains d ON d.tenant_id = t.id
       WHERE d.domain = ? AND d.verified = 1`
    )
    .bind(domain)
    .first<{ id: string; plan: string }>();
  return row ?? null;
}
```

**Step 2: Commit**

```bash
git add saas/worker/src/middleware/
git commit -m "feat(saas/tenant): add domain-to-tenant resolver"
```

---

## Phase 11 — Accounts & Billing

### Task 11.1: Stripe webhook handler (subscription events)

**Files:**
- Create: `saas/worker/src/billing/stripe.ts`

**Step 1: Write Stripe webhook handler**

Create `saas/worker/src/billing/stripe.ts`:

```typescript
const STRIPE_WEBHOOK_SECRET = "whsec_..."; // from env

export async function handleStripeWebhook(
  request: Request,
  db: D1Database
): Promise<Response> {
  const sig = request.headers.get("stripe-signature");
  const body = await request.text();

  // Verify signature (use Stripe SDK or manual HMAC check)
  // const event = stripe.webhooks.constructEvent(body, sig, STRIPE_WEBHOOK_SECRET);

  const event = JSON.parse(body); // simplified — add sig verification

  switch (event.type) {
    case "customer.subscription.updated":
    case "customer.subscription.created": {
      const sub = event.data.object;
      await db
        .prepare(`UPDATE tenants SET plan = ? WHERE id = ?`)
        .bind(sub.metadata.plan ?? "free", sub.metadata.tenant_id)
        .run();
      break;
    }
    case "customer.subscription.deleted": {
      const sub = event.data.object;
      await db
        .prepare(`UPDATE tenants SET plan = 'free' WHERE id = ?`)
        .bind(sub.metadata.tenant_id)
        .run();
      break;
    }
  }

  return new Response("ok");
}
```

**Step 2: Commit**

```bash
git add saas/worker/src/billing/
git commit -m "feat(saas/billing): add Stripe webhook handler for subscription events"
```

---

### Task 11.2: Usage metering via Analytics Engine

**Files:**
- Create: `saas/worker/src/billing/metering.ts`

**Step 1: Write metering helper**

Create `saas/worker/src/billing/metering.ts`:

```typescript
export interface Env {
  ANALYTICS: AnalyticsEngineDataset;
}

export function recordEmailReceived(env: Env, tenantID: string): void {
  env.ANALYTICS.writeDataPoint({
    blobs: [tenantID, "email_received"],
    doubles: [1],
    indexes: [tenantID],
  });
}

export function recordWebhookDelivery(
  env: Env,
  tenantID: string,
  outcome: "success" | "failure"
): void {
  env.ANALYTICS.writeDataPoint({
    blobs: [tenantID, "webhook_delivery", outcome],
    doubles: [1],
    indexes: [tenantID],
  });
}
```

**Step 2: Commit**

```bash
git add saas/worker/src/billing/metering.ts
git commit -m "feat(saas/metering): add usage metering via Analytics Engine"
```

---

## Phase 12 — SaaS Operations

### Task 12.1: Customer dashboard (Cloudflare Pages)

**Files:**
- Create: `saas/dashboard/` (React/Next.js app)

**Step 1: Scaffold dashboard**

```bash
cd saas/dashboard
npx create-next-app@latest . --typescript --tailwind --app
```

Key pages to implement:
- `/` — overview: messages received, delivery rate
- `/messages` — message log with lifecycle states
- `/domains` — domain management + DNS verification guide
- `/billing` — plan, usage, credits, Stripe portal link
- `/settings` — API key management, webhook secrets

**Step 2: Add Cloudflare Pages config**

Create `saas/dashboard/wrangler.toml`:

```toml
name = "mxhook-dashboard"
pages_build_output_dir = ".next"
compatibility_date = "2025-01-01"
```

**Step 3: Commit**

```bash
git add saas/dashboard/
git commit -m "feat(saas/dashboard): scaffold customer dashboard on Cloudflare Pages"
```

---

### Task 12.2: Admin panel

Key pages:
- `/admin/tenants` — list all tenants, usage, plan
- `/admin/messages` — global message search
- `/admin/abuse` — flag/block domains

Implement as a separate route group in the dashboard app, protected by admin role check.

**Step 1: Commit after implementation**

```bash
git add saas/dashboard/app/admin/
git commit -m "feat(saas/dashboard): add admin panel for tenant and abuse management"
```

---

### Task 12.3: Status page

Use `instatus.com` or build a simple Cloudflare Worker + D1 status page.

Key components:
- SMTP ingestion health
- Parser/router uptime
- Webhook delivery success rate (last 24h)
- Incident history

**Commit after implementation:**

```bash
git commit -m "feat(saas): add status page"
```

---

## Final Checklist Before SaaS Launch

- [ ] SMTP edge nodes deployed (fly.io VMs or dedicated servers)
- [ ] Cloudflare Queues binding `mxhook-inbound` created
- [ ] D1 database created and schema applied
- [ ] R2 buckets: `mxhook-raw`, `mxhook-attachments` created
- [ ] Stripe products and webhook configured
- [ ] DNS verification flow tested end-to-end
- [ ] Load test: 100 emails/sec through SMTP → webhook
- [ ] Grafana dashboards connected to Prometheus (self-hosted metrics)
- [ ] Status page live
- [ ] Security: HMAC signatures verified, API key rotation flow
- [ ] OSS v1.0 tagged and GitHub release published
