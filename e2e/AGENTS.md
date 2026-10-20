# emergent.memory/e2e

End-to-end test suite for the `memory` CLI (`github.com/emergent-company/emergent.memory`).

Tests run inside Docker where the `memory` binary is built from the checked-out source and installed at runtime under `$HOME/.memory/bin` — the same destination `apps/cli/install.sh` produces. The repo is private, so the image does **not** fetch the install script or a release from GitHub. Two categories:
- **CLI Install Tests** (`TestCLIInstalled_*`, `TestOpencode*`): full Docker Compose stack with ephemeral server + Postgres
- **Production Smoke Tests** (`TestProduction_*`): runs against live production server (skipped when `MEMORY_PROD_TEST_TOKEN` is absent)

## Module isolation

`e2e` is **not** part of the repo-root `go.work`; it has its own workspace at
`e2e/go.work`. Its test framework (runlog) pins an older
`google.golang.org/adk` whose telemetry does not compile against the `otel/log`
version the server's `adk/v2` selects, so sharing the root build list would let a
server dependency bump break e2e. A plain `go build ./...` / `go test ./...`
inside `e2e/` resolves through `e2e/go.work`; CI runs the module with
`GOWORK=off`. When you add a module under `e2e/`, list it in `e2e/go.work` — the
root coverage guard (`.github/scripts/check-go-work-coverage.sh`) requires every
module to be claimed by some workspace.

## Run Tests

### Task shortcuts (recommended)

`e2e/Taskfile.yml` wraps the daemon, the browser UI, and the runlog CLI so you
do not have to remember flags or the right working directory. Run them all with
`task -d e2e <name>`.

| Task | What it does |
|---|---|
| `runlog:start` | Start the daemon + web UI on `:17432` (no Docker) |
| `runlog:stop` / `runlog:restart` | Stop / restart the daemon |
| `runlog:status` | Daemon health, `runs.db` path, and recent runs |
| `runlog:ui` | Print (and try to open) the web UI URL |
| `runlog:runs` | List recent runs |
| `runlog:failing` | Failing tests, streak-sorted |
| `runlog:stats` | Per-test pass rate + average duration |
| `runlog:tail` | Stream new events as they arrive |
| `runlog:reap` | Mark stale/orphaned runs as FAIL (`-- --dry-run` to preview) |
| `runlog:clear` | Delete `runs.db` and per-run logs (destructive; refuses while the daemon is up) |
| `runlog:logs:clean` | Remove per-run log directories, keep `runs.db` |
| `runlog:test` | Run the e2e suite through runlog |

All of them pin the working directory to `e2e/`, so the daemon, the CLI, and the
test framework all agree on `<e2e>/.runlog/runs.db`. Pass extra flags after `--`,
e.g. `task -d e2e runlog:runs -- --since 7d`.

Requirements: the `runlog` binary (on `PATH` or `$(go env GOPATH)/bin/runlog`)
and `lsof` (for daemon port detection). `runlog:clear` refuses to run while the
daemon is listening — stop it first with `runlog:stop` to avoid orphaning the
`runs.db` inode.

`daemon:start` / `daemon:stop` / `daemon:restart` / `runs:clear` / `runs:reap` /
`logs:clean` remain as aliases. The one behaviour change: `daemon:start` no
longer pulls in the Docker server — use `test:mcj` or `server:start` for that.

### Supervision

The daemon is a detached background process with no supervisor of its own. Start
it from a shell or an agent session and it stays up only until something sends it
SIGTERM — after which the UI at `:17432` just stops responding and nothing brings
it back. A daemon can also outlive the directory it was started from and end up
bound to a `runs.db` that no longer exists, silently swallowing every run.

`e2e/runlog.service` is the supported fix — install it once so systemd owns the
lifecycle and restarts the daemon automatically. The shipped unit points at the
shared checkout `/root/emergent.memory/e2e`, which exists wherever the repo is
cloned. If you change `WorkingDirectory` (see *Choosing the checkout*), create
that checkout **before** enabling the unit — systemd fails at `CHDIR` and the UI
never starts otherwise:

```bash
sudo cp e2e/runlog.service /etc/systemd/system/runlog.service
sudo systemctl daemon-reload
sudo systemctl enable --now runlog.service
sudo systemctl status runlog.service
```

The `runlog:*` tasks detect the unit and delegate to it (`runlog:start` →
`systemctl start`, `runlog:stop` → `systemctl stop`) rather than racing it for
the port. `runlog:status` reports which supervisor is in charge and which
`runs.db` the unit actually serves, and warns when that differs from the
checkout's expected `runs.db`. With the unit enabled, stop it with `systemctl
stop runlog` — killing the process directly just triggers a restart.

#### Choosing the checkout

The daemon discovers tests by scanning its working directory **at startup**, so
`WorkingDirectory` must point at an e2e directory that contains `tests/` and is
stable. An e2e dir without `tests/` renders an empty UI with no other clue.
`runlog:start` and `runlog:status` probe the unit's `WorkingDirectory` when the
unit supervises the daemon, and warn when it is missing `tests/`.

The shipped default is the **shared checkout** (`/root/emergent.memory/e2e`).
That keeps the UI and the CLI coherent — `runlog test` writes runs into the
*invoking* checkout's `<e2e>/.runlog/runs.db`, so the daemon must serve that same
file for new runs to appear — and it exists out of the box, so `enable --now`
cannot fail at `CHDIR`.

On a busy host, harden this by pointing the daemon at a **dedicated, detached
checkout** so other sessions switching branches or mid-edit files cannot empty
the catalog. Create the checkout first, then apply the drop-in:

```bash
git worktree add --detach /root/emergent.memory-wt/runlog-dashboard origin/main
# refresh it later:
git -C /root/emergent.memory-wt/runlog-dashboard fetch origin main
git -C /root/emergent.memory-wt/runlog-dashboard reset --hard origin/main

sudo systemctl edit runlog.service
#   [Service]
#   WorkingDirectory=/root/emergent.memory-wt/runlog-dashboard/e2e
#   ExecStart=
#   ExecStart=/root/go/bin/runlog --daemon --db /root/emergent.memory-wt/runlog-dashboard/e2e/.runlog/runs.db --port=17432
```

> **Caveat.** A dedicated checkout decouples the daemon's `runs.db` from the
> checkout you run tests in. `runlog test` from your normal checkout then writes
> to *its* `.runlog/runs.db` while the daemon serves the dedicated checkout's
> copy, so those runs will not show in the UI. Either run the tests from the
> dedicated checkout, or point the tasks at the daemon's DB. `runlog:status`
> warns when the two paths differ.

`WorkingDirectory=` does not expand environment variables, so use the drop-in
above rather than an `EnvironmentFile`.

**Restart after any change** to the checkout, its branch, or its config —
discovery is computed once at startup and cached:

```bash
sudo systemctl restart runlog
```

#### Version pin

The daemon must be the same runlog version this module is compiled against
(`e2e/go.mod`). An older daemon creates `runs.db` with an older schema, so tables
added later (e.g. `test_definitions`, migration 27 in v0.3.0) never exist; writes
to them fail silently (`UpsertDefinition`'s error is discarded) and the daemon
reports no tests. `runlog:status` and `runlog:start` both check this and warn on
a mismatch.

Note the binary is a host tool and is *not* pinned by the Go toolchain —
`go install ...@latest` silently gives you whatever is newest, and
`go install ...@v0.3.0` fails outright because the module has `replace`
directives. Build it from a tag checkout instead:

```bash
git clone --branch v0.3.0 --depth 1 https://github.com/emergent-company/runlog /root/runlog
cd /root/runlog && GOWORK=off go build -o /root/go/bin/runlog ./cmd/runlog
go version -m /root/go/bin/runlog | grep emergent-company/runlog   # confirm
```

#### Reading the CLI

`runlog tests` lists only tests that have **already run** — it sources names from
`test_runs`, while the web UI sources from the full catalog including never-run
tests. A fresh project therefore shows an empty `runlog tests` table next to a
populated UI. This is upstream: `emergent-company/runlog#52`.

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

### GitHub live data-source test (opt-in)

`TestGitHubDataSource_LiveSyncAndVersion` runs the GitHub connector against the
real GitHub API: private-repo auth, a branch ref containing `/`, and a contents
commit that must produce document version 2 while the unchanged file is skipped.
It is skipped unless the repo and read token are set, and is never run in CI.

| Variable | Required | Description |
|---|---|---|
| `E2E_GITHUB_LIVE_REPO` | yes | Private fixture repository, `owner/name` |
| `E2E_GITHUB_LIVE_TOKEN` | yes | Token with `Contents: read` on that repo, used by the source |
| `E2E_GITHUB_LIVE_WRITE_TOKEN` | no | Token with `Contents: read and write` for the fixture branch and commits; defaults to `E2E_GITHUB_LIVE_TOKEN` |
| `E2E_GITHUB_LIVE_API_URL` | no | GitHub API origin, default `https://api.github.com` (GitHub Enterprise) |

The test creates a throwaway branch `e2e/<run>` with two markdown files under
`e2e/<run>/`, syncs it, commits a new `guide.md`, re-syncs, and asserts version 2
plus the unchanged file's revision skip. The branch is deleted on cleanup; the
fixture repo must allow branch creation and content writes (no protection rule
covering `e2e/**`).

Run it against a server configured for real GitHub. Do **not** point it at the
compose stack: that server sets `GITHUB_API_BASE_URL` to the hermetic stub, so
the connection test fails. For GitHub Enterprise, set `GITHUB_API_BASE_URL` on
the server under test as well as `E2E_GITHUB_LIVE_API_URL` here — the fixture
setup and the connector must hit the same origin.

```bash
MEMORY_TEST_ENV=mcj-emergent \
E2E_GITHUB_LIVE_REPO=acme/private-docs \
E2E_GITHUB_LIVE_TOKEN=ghp_... \
E2E_GITHUB_LIVE_WRITE_TOKEN=ghp_... \
go test -run TestGitHubDataSource_LiveSyncAndVersion ./tests/api/
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
| `TEST_RUNS_DB` | _(none)_ | Explicit override for `runs.db` path. Default: `<e2e>/.runlog/runs.db` |
| `TEST_RUNNER` | _(auto)_ | Runner label stored in runs DB: `host`, `docker`, etc. Auto-detected if unset. |

### Named env files

| File | Target |
|---|---|
| `.env` | Docker Compose standalone server (CI default) |
| `.env.mcj-emergent` | Shared `mcj-emergent` test server (`MEMORY_AUTH_MODE=account`) |
| `.env.localhost` | Local standalone server on `localhost:3012` |

## Key Conventions

- Direct Go dependencies: `github.com/emergent-company/runlog` (framework) and `github.com/google/uuid`
- `opencode` binary is installed in the Dockerfile as test infrastructure
- Go module: `github.com/emergent-company/emergent.memory/e2e`
- The `runlog` TUI binary has been extracted to [`github.com/emergent-company/runlog`](https://github.com/emergent-company/runlog). Build it at the version this module pins — see [Version pin](#version-pin) under Supervision; `go install ...@latest` is not a safe way to get it.

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

Import alias: `fixtures "github.com/emergent-company/emergent.memory/e2e/fixtures"`
