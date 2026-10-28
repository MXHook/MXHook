# AGENTS.md — MXHook Project Instructions

Canonical agent-facing instructions. `CLAUDE.md` points here.

## Project Mandates
- **Language:** Go 1.27 (pinned in `go.mod` and `mise.toml`).
- **Core Pattern:** SMTP or Maildir ingress → durable queue → parser → router → webhook deliverer.
- **Enterprise Edition (EE):** `ee/` holds the BSL 1.1 license gate (`ee.License`). RBAC and payload
  transforms plug in through the `api.Authorizer` and `webhook.Transformer` interfaces.
- **CLI:** Cobra/Viper in `cli/`.
- **Platforms:** Linux and macOS; Windows is unsupported (Maildir is POSIX-only).

## Build & Test Commands
- **Everything CI gates:** `make check` (fmt-check, vet, test, race) and `make lint`.
- **Build All:** `make compile` (`go build -buildvcs=false ./...`); binary: `make build`.
- **Package Tests:** `go test -v ./api/...` (or `./cli/...`, `./smtp/...`, `./webhook/...`, `./ee/...`).
- **Single Test:** `go test -v ./api -run TestAPIKey_RequiredWhenConfigured`.
- **Postgres-gated tests:** `make test-postgres` (needs `TEST_DATABASE_URL`).
- Setup and the full target list: [DEVELOPING.md](DEVELOPING.md).

## Code Style & Conventions
- **Naming:** standard Go camelCase; `err` for errors; single-letter receivers (e.g., `func (s *Server)`).
- **Error Handling:** check every error; wrap with context (`fmt.Errorf("api: %w", err)`); discard only
  with `_ =` and a reason.
- **Formatting:** gofumpt + goimports (enforced by golangci-lint).
- **API Pattern:** standard library `net/http` with Go 1.22+ mux routing (e.g., `GET /dlq`).
- **Permission checks:** register handlers with `s.requirePerm(perm, handler)`; the configured
  `api.Authorizer` decides.
- **Commits:** Conventional Commits; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Common Tasks
- **Start Server:** `go run main.go start` (or build and run `mxhook start`).
- **View OpenAPI:** `GET /openapi.yaml` from a running server.
- **Verify Signature:** `webhook.VerifyTimestampedSignature` (constant-time HMAC over `"{ts}.{payload}"`).
