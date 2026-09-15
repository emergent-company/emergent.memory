# emergent.memory/e2e

End-to-end tests for the [Memory](https://github.com/emergent-company/emergent.memory) CLI.

These tests run inside a Docker container where the `memory` binary is installed from the GitHub release via `install.sh` — exactly as a real end-user would. They exercise the full install path (download, extract, PATH setup) and validate CLI behaviour against a live Memory server.

[![E2E Tests](https://github.com/emergent-company/emergent.memory/e2e/actions/workflows/e2e.yml/badge.svg)](https://github.com/emergent-company/emergent.memory/e2e/actions/workflows/e2e.yml)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `MEMORY_TEST_SERVER` | `http://test-emergent-server:5300` | URL of the Memory API server the tests run against. In Docker Compose the compose-internal hostname is used automatically; override to point at a different server. |
| `MEMORY_SERVER_IMAGE` | `ghcr.io/emergent-company/memory-server:latest` | Docker image used for the test server in the full-stack Docker Compose run. |
| `MEMORY_PROD_TEST_TOKEN` | _(none)_ | Pre-issued API token for production smoke tests. Must be set as a repository secret in CI. When absent all `TestProduction_*` tests are skipped. |
| `TEST_RUN` | _(none)_ | Optional `-run` filter passed to `go test`. Example: `TEST_RUN=TestCLIInstalled_Version`. When unset, all tests run. |

## Local usage

### Prerequisites

- Docker and Docker Compose
- Access to pull `ghcr.io/emergent-company/memory-server:latest` (or set `MEMORY_SERVER_IMAGE`)

### Full stack (server + tests)

```bash
./run_tests.sh
```

Spins up the Memory server container and the test runner container together. Test logs are written to `./test-logs/`.

### Tests only (against an existing server)

```bash
MEMORY_TEST_SERVER=http://localhost:3012 ./run_tests.sh --tests-only
```

Skips the server — requires `MEMORY_TEST_SERVER` to point at a running Memory server.

### Build only (no test run)

```bash
./run_tests.sh --build-only
```

### Single-test mode

Run exactly one test and exit immediately:

```bash
TEST_RUN=TestCLIInstalled_Version ./run_tests.sh --tests-only
```

Works with either `--tests-only` or the full-stack mode.

### Running tests directly (outside Docker)

Use `runlog test` for local development — it handles environment loading with profile support and automatically records which environment was used:

```bash
# All tests with base .env
runlog test

# Tests with environment profile overlay
runlog test localhost                     # loads .env.localhost
runlog test mcj-emergent TestCLI_Auth     # loads .env.mcj-emergent, runs one test
runlog test production -- -v              # pass extra flags to go test
```

The `runlog test` command:
- Loads `.env` from the repo directory
- Overlays `.env.<profile>` if a profile is specified (via `MEMORY_TEST_ENV`)
- Sets up `PATH` to find the `memory` binary
- Configures the test database location
- Records the test run with environment info in the runlog database

You can inspect results with:

```bash
# List recent test runs with environment info
runlog runs --since 5m

# Inspect a specific run (shows which environment was used)
runlog inspect <run-id>

# View all events for a run
runlog show <run-id>
```

For raw `go test` without these conveniences:

```bash
MEMORY_TEST_SERVER=http://localhost:3012 go test -v ./...
```

The `memory` binary must be on `PATH` already (i.e. installed via `install.sh`).

## CI

Two jobs run on every push to `main` and on pull requests:

| Job | Description |
|---|---|
| **CLI Install Tests** | Full Docker Compose stack. Installs the `memory` CLI from the GitHub release inside the container, then runs all `TestCLIInstalled_*` and `TestOpencode*` tests. |
| **Production Smoke Tests** | Runs `TestProduction_*` against the live production server using `MEMORY_PROD_TEST_TOKEN`. Skipped when the secret is absent (e.g. on forks). |
