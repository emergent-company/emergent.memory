# emergent.memory.e2e

End-to-end test suite for the `memory` CLI (`github.com/emergent-company/emergent.memory`).

Tests run inside Docker where the `memory` binary is installed at runtime via `install.sh` — exactly as a real end-user would. Two categories:
- **CLI Install Tests** (`TestCLIInstalled_*`, `TestOpencode*`): full Docker Compose stack with ephemeral server + Postgres
- **Production Smoke Tests** (`TestProduction_*`): runs against live production server (skipped when `MEMORY_PROD_TEST_TOKEN` is absent)

## Run Tests

### Host runner (preferred for local dev — no Docker needed)

```bash
runlog test                                       # all tests, .env defaults
runlog test mcj-emergent                          # against shared test server
runlog test localhost                             # against local dev server (localhost:3012)
runlog test mcj-emergent TestCLIInstalled_Version # single test, named env
runlog test mcj-emergent -- -count 1             # pass extra go test flags after --

# Shell vars override env file values:
MEMORY_TEST_SERVER=http://custom:9000 runlog test mcj-emergent
```

### Docker runner (CI / full stack with ephemeral server)

```bash
./run_tests.sh                              # full stack (server + tests) — Docker standalone
./run_tests.sh --tests-only                 # tests only against MEMORY_TEST_SERVER
./run_tests.sh --build-only                 # build Docker image only
TEST_RUN=TestCLIInstalled_Version ./run_tests.sh --tests-only  # single test
```

### Raw go test (memory binary must be on PATH)

```bash
MEMORY_TEST_ENV=mcj-emergent go test -v ./...   # against shared test server
MEMORY_TEST_ENV=localhost     go test -v ./...   # against local standalone server
```

## Key Environment Variables

| Variable | Default | Description |
|---|---|---|
| `MEMORY_TEST_ENV` | _(none)_ | Load `.env.<name>` overlay (e.g. `mcj-emergent`, `localhost`) |
| `MEMORY_TEST_SERVER` | `http://test-emergent-server:3002` | URL of the Memory server under test |
| `MEMORY_TEST_TOKEN` | `e2e-test-user` | API key / token for test server auth |
| `MEMORY_AUTH_MODE` | `standalone` | Auth style: `standalone` (X-API-Key) or `account` (Bearer + X-API-Key) |
| `MEMORY_SET_TOKEN` | `all-scopes` | Bearer value written to credentials.json when `MEMORY_AUTH_MODE=account` |
| `MEMORY_SERVER_IMAGE` | `ghcr.io/emergent-company/memory-server:latest` | Docker image for test server |
| `MEMORY_PROD_TEST_TOKEN` | _(none)_ | Token for production smoke tests |
| `TEST_RUN` | _(none)_ | Optional `-run` filter for `go test` |
| `TEST_LOG_DIR` | _(none)_ | Directory for flat log files (run.log, session logs). Set to `/test-logs` in Docker. |
| `TEST_RUNS_DB` | _(none)_ | Explicit override for `runs.db` path. Default: `<repo_root>/logs/runs.db` |
| `TEST_RUNNER` | _(auto)_ | Runner label stored in runs DB: `host`, `docker`, etc. Auto-detected if unset. |

### Named env files

| File | Target |
|---|---|
| `.env` | Docker Compose standalone server (CI default) |
| `.env.mcj-emergent` | Shared `mcj-emergent` test server (`MEMORY_AUTH_MODE=account`) |
| `.env.localhost` | Local standalone server on `localhost:3012` |

## Key Conventions

- No external Go dependencies — standard library only
- `opencode` binary is installed in the Dockerfile as test infrastructure
- Go module: `github.com/emergent-company/emergent.memory.e2e`
- The `runlog` TUI binary has been extracted to [`github.com/emergent-company/runlog`](https://github.com/emergent-company/runlog). Install with `go install github.com/emergent-company/runlog/cmd/runlog@latest`

## Install Skills

The `runlog` binary bundles 5 embedded skills (`runlog-*` prefixed) and includes a `skills install` subcommand that copies them into each configured AI agent tool's install directory. No external skill source directories are needed.

```bash
# List embedded skills
runlog skills list

# Interactive: detect tools, prompt for selection
runlog skills install

# Install for all detected tools without prompting
runlog skills install --all

# Install for specific tools
runlog skills install --tools opencode,claude

# Preview what would be installed (no writes)
runlog skills install --dry-run --all

# Overwrite existing installs
runlog skills install --all --force
```

Embedded skills: `runlog-test-designer`, `runlog-verify-e2e-changes`, `runlog-verify-runs`, `runlog-clear`, `runlog-install-skills`.

Detected tools are identified by the presence of their config directory (e.g. `.opencode/`, `.claude/`, `.cursor/`). Use `--tools` to bypass detection and target a specific set of tool IDs.

## Before Writing Tests

| Creating... | Read first... |
|---|---|
| New e2e test | `.opencode/skills/create-e2e-test/SKILL.md` — workflow, skeleton, and patterns |
| Framework helper | [`github.com/emergent-company/runlog`](https://github.com/emergent-company/runlog) source + docs |

## Package Layout

### `github.com/emergent-company/runlog` — `package runlog`

Standalone Go module containing the full framework library. All test files
import it directly with the alias `framework`:

```go
import framework "github.com/emergent-company/runlog"
```

The library provides `RunLog`, `TestContext`, `Step`, CLI runners, HTTP helpers,
project/agent helpers, SQLite DB layer, LLM analyzer, and more. See
`.opencode/skills/create-e2e-test/reference/framework-api.md` for the full API.

### `fixtures/` — `package e2efixtures`

Reusable test workspace setup.

| File | Exports |
|---|---|
| `bookstore.go` | `NewBookstoreWorkspace`, `BookstoreWorkspace` |

Import alias: `fixtures "github.com/emergent-company/emergent.memory.e2e/fixtures"`
