# Developing MXHook

Maintainer guide: toolchain, local environment, and the commands CI runs. For
how changes are proposed and reviewed, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Supported platforms

Linux and macOS. Windows is unsupported: Maildir ingestion relies on POSIX
file semantics (`O_NOFOLLOW`, `flock`, no-replace renames). CI tests on
`ubuntu-latest` and `macos-latest`.

## Before you begin

You need either Docker (for the devcontainer) or
[mise](https://mise.jdx.dev) (for a local toolchain). Everything else — Go,
Node, golangci-lint, sqlite — is installed from the pinned versions. Familiarity
with SMTP and MIME parsing is recommended.

## Set up the dev environment

Two paths are supported. Both give you the same toolchain versions CI uses —
Go 1.27.3, Node 22, and golangci-lint 2.14.0, pinned in `mise.toml`.

### Devcontainer (recommended)

Open the repository in VS Code and accept the "Reopen in Container" prompt, or
run `devcontainer up --workspace-folder .` with the devcontainer CLI.

The container installs mise and the pinned toolchain, then runs
`make worker-deps`. It composes `.devcontainer/docker-compose.devcontainer.yml`
over the repository `docker-compose.yml` so the existing `postgres` service is
reused rather than duplicated, and presets `TEST_DATABASE_URL` against it. That
matters: the Postgres-gated integration tests actually run here instead of
skipping.

Startup seeds `.devcontainer/.env` from the tracked `.env.example` if it is
missing. Environment files are never committed — `.env` and every variant
(`.env.docker`, `.env.staging`, …) are gitignored, and only `*.env.example`
templates are tracked. If you drive Compose by hand rather than through the
devcontainer, copy it first, because `docker-compose.yml` resolves its
`${VAR:?}` guards when Compose *parses* the file:

```bash
cp .devcontainer/.env.example .devcontainer/.env
docker compose --env-file .devcontainer/.env \
  -f docker-compose.yml -f .devcontainer/docker-compose.devcontainer.yml \
  up -d postgres
```

### Local

Install [mise](https://mise.jdx.dev), then:

```bash
git clone git@github.com:MXHook/MXHook.git
cd mxhook
mise trust && mise install
make worker-deps
```

`mise trust` is required — mise refuses to run an untrusted config file.
Postgres-gated tests skip unless `TEST_DATABASE_URL` is set; `make test` and
`make race` never need a database. To run them, start one with
`docker compose --profile postgres up -d postgres`, then:

```bash
make test-postgres \
  TEST_DATABASE_URL='postgres://mxhook:mxhook@localhost:5432/mxhook?sslmode=disable'
```

Verify the setup:

```bash
make build
```

The binary lands in `./bin/mxhook`.

## Run the tests

`make help` lists every target and is the default goal, so a bare `make`
prints it.

```bash
make test          # Go test suite
make race          # same suite under the race detector
make vet           # go vet
make fmt           # format Go sources
make fmt-check     # fail if anything is unformatted
make lint          # golangci-lint at the version CI pins
```

`make lint` runs golangci-lint through mise when mise is available. A
`go install`ed binary in the Go toolchain's `bin/` otherwise shadows the pinned
one and lints with a different version than CI.

### Postgres-gated tests

```bash
make test-postgres
```

The Postgres queue and state adapters are only exercised when
`TEST_DATABASE_URL` is set; without it those tests skip. `make test-postgres`
refuses to run and exits non-zero rather than reporting a green suite that
silently covered nothing — a failure mode this repository has hit before.

### Worker and schema

```bash
make worker-deps      # npm ci; typecheck and lint fake-pass without it
make worker-typecheck
make worker-lint
make worker-check     # both of the above
make schema-check
```

`make schema-check` applies every migration in `worker/migrations/` to a
scratch SQLite database and reports the resulting tables. The worker's D1
queries are raw strings, so `tsc` cannot inspect the columns they name;
executing them against a real schema is the only check that catches column
drift. Six runtime bugs of exactly that class were found recently.

### Before you push

```bash
make check       # fmt-check, vet, test, race
make check-all   # the above plus worker-check and schema-check
```

`make check-all` covers everything CI enforces except the Postgres suite and
the container build (`make docker`).

### Pre-commit hooks

```bash
pre-commit install
```

`.pre-commit-config.yaml` mirrors the CI lint gate: gofumpt/goimports through
golangci-lint, actionlint for workflows, and basic whitespace checks.

## CI

CI runs the same Makefile targets through mise (`jdx/mise-action` +
`mise exec -- make <target>`), so local and CI toolchains match:

| Job | Targets |
|---|---|
| `build / go` (ubuntu, macos) | `compile vet test race build` |
| `build / postgres` | `test-postgres` |
| `build / container` | Compose validation, `docker` |
| `build / worker-contract` | `worker-deps worker-check worker-dry-run` |
| `coverage` | `coverage` (artifact upload) |
| `lint` | golangci-lint, actionlint |

## Releasing

Two protected, merge-only lines: `next` (default branch, prerelease line) and
`main` (stable line). release-please runs on `next`: it keeps a standing
release PR from the Conventional Commits there, and merging that PR tags
`vX.Y.Z-alpha.N` and GoReleaser publishes the binaries. Stable releases are cut
on `main`; that wiring arrives with the first stable line.
The release workflow needs the release-bot App secrets
(`RELEASE_BOT_APP_ID`, `RELEASE_BOT_PRIVATE_KEY`); without them it skips.
