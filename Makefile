GO             ?= go
NPM            ?= npm
SQLITE         ?= sqlite3
WORKER_DIR     ?= worker
MIGRATIONS_DIR ?= $(WORKER_DIR)/migrations
SCRATCH_DB     ?= /tmp/mxhook-schema-check.db
TEST_LOG       ?= /tmp/mxhook-test.log
GO_PKGS        ?= ./...
COVER_PROFILE  ?= coverage.out
NPM_CI_FLAGS   ?=

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---- Go ----------------------------------------------------------------

.PHONY: build
build: ## Build the mxhook binary into ./bin
	$(GO) build -buildvcs=false -trimpath -o ./bin/mxhook .

.PHONY: test
test: ## Run the Go test suite
	$(GO) test -buildvcs=false $(GO_PKGS) -count=1

.PHONY: compile
compile: ## Compile every package (no binary output)
	$(GO) build -buildvcs=false $(GO_PKGS)

.PHONY: coverage
coverage: ## Race-enabled test run writing a coverage profile
	$(GO) test -buildvcs=false -race -covermode=atomic -coverprofile=$(COVER_PROFILE) $(GO_PKGS)

.PHONY: race
race: ## Run the Go test suite under the race detector
	$(GO) test -buildvcs=false -race $(GO_PKGS) -count=1

.PHONY: vet
vet: ## Run go vet
	$(GO) vet -buildvcs=false $(GO_PKGS)

.PHONY: fmt
fmt: ## Format Go sources
	$(GO) fmt $(GO_PKGS)

# Uses the mise-pinned golangci-lint when available: a stray `go install`ed
# binary in the Go toolchain's bin/ otherwise shadows it and lints with a
# different version than CI.
.PHONY: lint
lint: ## Run golangci-lint at the version CI pins
	@if command -v mise >/dev/null 2>&1; then \
		mise exec aqua:golangci/golangci-lint -- golangci-lint run; \
	else \
		echo "mise not found; falling back to golangci-lint on PATH ($$(golangci-lint --version 2>/dev/null | head -1))"; \
		golangci-lint run; \
	fi

# gofmt over tracked .go files: `go list` needs -buildvcs=false in a worktree
# and would swallow the real result behind a VCS error.
.PHONY: fmt-check
fmt-check: ## Fail if any Go source is unformatted
	@unformatted=$$(git ls-files '*.go' | xargs gofmt -l); \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; echo "$$unformatted"; exit 1; \
	fi
	@echo "gofmt: clean"

# Postgres-backed tests skip unless TEST_DATABASE_URL is set; they are the real
# gate for the postgres queue and state adapters.
.PHONY: test-postgres
test-postgres: ## Run Go tests with Postgres integration enabled (needs TEST_DATABASE_URL)
	@if [ -z "$$TEST_DATABASE_URL" ]; then \
		echo "TEST_DATABASE_URL is not set; postgres integration tests would skip"; \
		echo "example: make test-postgres TEST_DATABASE_URL=postgres://mxhook:mxhook@localhost:5432/mxhook?sslmode=disable"; \
		exit 1; \
	fi
	$(GO) test -buildvcs=false $(GO_PKGS) -count=1

# ---- Worker (Cloudflare, TypeScript) -----------------------------------

.PHONY: worker-deps
worker-deps: ## Install worker dependencies (typecheck/lint fake-pass without them)
	cd $(WORKER_DIR) && $(NPM) ci $(NPM_CI_FLAGS)

.PHONY: worker-typecheck
worker-typecheck: ## Type-check the worker
	cd $(WORKER_DIR) && $(NPM) run typecheck

.PHONY: worker-lint
worker-lint: ## Lint the worker
	cd $(WORKER_DIR) && $(NPM) run lint

.PHONY: worker-dry-run
worker-dry-run: ## Bundle the worker without deploying
	cd $(WORKER_DIR) && $(NPM) exec wrangler -- deploy --dry-run

.PHONY: worker-check
worker-check: worker-typecheck worker-lint ## Type-check and lint the worker

.PHONY: worker-dev
worker-dev: ## Run the worker locally
	cd $(WORKER_DIR) && $(NPM) run dev

# ---- Schema ------------------------------------------------------------

# tsc cannot see inside raw D1 query strings, so applying the migrations and
# executing queries against real SQLite is the only check that catches column
# drift.
.PHONY: schema-check
schema-check: ## Apply all migrations to a scratch SQLite DB and report the tables
	@rm -f $(SCRATCH_DB)
	@for f in $(MIGRATIONS_DIR)/*.sql; do \
		$(SQLITE) $(SCRATCH_DB) ".read $$f" || { echo "FAILED: $$f"; rm -f $(SCRATCH_DB); exit 1; }; \
	done
	@echo "integrity: $$($(SQLITE) $(SCRATCH_DB) 'PRAGMA integrity_check;')"
	@echo "tables:    $$($(SQLITE) $(SCRATCH_DB) "SELECT count(*) FROM sqlite_master WHERE type='table';")"
	@$(SQLITE) $(SCRATCH_DB) "SELECT '  - ' || name FROM sqlite_master WHERE type='table' ORDER BY name;"
	@rm -f $(SCRATCH_DB)

.PHONY: schema-shell
schema-shell: ## Open a SQLite shell on a scratch DB with all migrations applied
	@rm -f $(SCRATCH_DB)
	@for f in $(MIGRATIONS_DIR)/*.sql; do $(SQLITE) $(SCRATCH_DB) ".read $$f" || exit 1; done
	@echo "scratch db: $(SCRATCH_DB) (delete it when done)"
	@$(SQLITE) $(SCRATCH_DB)

# ---- Aggregates --------------------------------------------------------

# Never pipe `go test` into grep/head/tail when the exit status is what you
# care about: the pipeline reports the LAST command's status, so a panicking
# suite can read as success. Capture output to a file, then inspect it.
.PHONY: test-log
test-log: ## Run the suite, capture output to a file, report the real exit code
	@$(GO) test -buildvcs=false $(GO_PKGS) -count=1 >$(TEST_LOG) 2>&1; \
	status=$$?; \
	echo "exit=$$status  failures=$$(grep -cE '^(FAIL|--- FAIL)' $(TEST_LOG))  ok=$$(grep -cE '^ok' $(TEST_LOG))"; \
	echo "log: $(TEST_LOG)"; \
	exit $$status

.PHONY: check
check: fmt-check vet test race ## Go gate: format, vet, tests, race

.PHONY: check-all
check-all: check worker-check schema-check ## Everything CI enforces, minus Postgres and Docker

.PHONY: docker
docker: ## Build the container image
	docker build -t mxhook:local .

.PHONY: clean
clean: ## Remove build output and scratch databases
	rm -rf ./bin
	rm -f $(SCRATCH_DB)
