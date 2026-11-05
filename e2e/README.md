# emergent.memory/e2e

End-to-end tests for the [Memory](https://github.com/emergent-company/emergent.memory) CLI.

These tests run inside a Docker container where the `memory` binary is built from the checked-out source and installed under `$HOME/.memory/bin` — the same destination `apps/cli/install.sh` uses. The image builds from the tree under test rather than fetching a released binary, so it validates the CLI as it exists in that checkout against a live Memory server.

[![E2E Tests](https://github.com/emergent-company/emergent.memory/actions/workflows/e2e.yml/badge.svg)](https://github.com/emergent-company/emergent.memory/actions/workflows/e2e.yml)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `MEMORY_TEST_SERVER` | `http://test-emergent-server:3002` | URL of the Memory API server the tests run against. In Docker Compose the compose-internal hostname is used automatically; override to point at a different server. |
| `MEMORY_SERVER_IMAGE` | `ghcr.io/emergent-company/memory-server:latest` | Docker image used for the test server in the full-stack Docker Compose run. |
| `TEST_RUN` | _(none)_ | Optional `-run` filter passed to `go test`. Example: `TEST_RUN=TestCLIInstalled_Version`. Unset means "run everything" **only** when you invoke `go test` directly: the container entrypoint — reached through `run_tests.sh` as well as through CI — substitutes the CI allowlist, so a test that is not in it does not run there unless `TEST_RUN` selects it (see [CI](#ci)). |
| `MEMORY_TEST_BLUEPRINTS_DIR` | _(none)_ | Directory holding the blueprint fixture corpora that `tests/blueprints` and `tests/tools` apply (one subdirectory per blueprint, e.g. `workspace-memory-blueprint`). The corpora are not part of this repo; when the variable is unset — or names a directory without the fixture — those suites skip instead of failing. |
| `E2E_LITELLM_API_KEY` | `sk-test-reextraction-fixture` | Key the reextraction integration test passes when configuring a LiteLLM-backed provider. Override it when the target stack validates real virtual keys. |

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
runlog test localhost                         # loads .env.localhost
runlog test compose TestCLIInstalled_Version  # loads .env.compose, one test
runlog test production -- -v                  # pass extra flags to go test
```

`compose` targets the stack that `task -d e2e server:start` brings up. Its server
publishes an **ephemeral** loopback port, so `.env.compose` has to carry the one
for the current run:

```bash
cd e2e
port="$(docker compose port test-emergent-server 3002 | sed 's/.*://')"
echo "MEMORY_TEST_SERVER=http://127.0.0.1:${port}" > .env.compose
```

`task -d e2e test:compose` starts that stack and then runs this profile.

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

## Test conventions

The rules below apply to every test in this suite. They keep runs reproducible,
reviewable, and faithful to how a real user drives the platform.

1. **CLI first.** If the `memory` CLI can do it, the test uses the CLI. Direct
   HTTP calls are allowed only when no CLI equivalent exists, when the test
   validates HTTP-level behaviour (status codes, headers, SSE framing), or in
   `tests/api/`, which tests the HTTP contract by design.
2. **Ask the platform when unsure.** `memory ask "<question>"` answers questions
   about the CLI, API, and concepts without reading source.
3. **One project per test.** Create a project, use it, and delete it in
   `t.Cleanup`. No test depends on another test's project and none leave one
   behind.
4. **Every test is logged.** Create the run log with `rl := newRunLog(t)` and
   `defer rl.Close()`, describe the test with `rl.Describe(...)`, and log every
   CLI or HTTP call (`rl.CLI`, `rl.CLIStep`). A section must never be empty.
5. **Fail through the log.** Use `rl.Failf(...)` instead of `t.Fatalf(...)` so
   the reason reaches `runlog inspect`. Pre-condition checks may use `t.Fatalf`
   or `t.Skipf` before the run log exists.
6. **Name sections by action.** "Trigger orchestrator agent", not "Step 2".
7. **Never clear `logs/runs.db` automatically.** Historical runs are the
   regression record; clear it only when explicitly asked.
8. **CI runs part of this suite, not all of it.** The CLI job executes only the
   `TEST_RUN` allowlist ([CI](#ci)), so a new `TestCLIInstalled_*` test does not
   run there unless it is added to that default or selected in a
   `workflow_dispatch`. A PR that claims one is verified should link the
   `runlog test` run that executed it.

## CI

Three test jobs run when a change touches `apps/server/**`, `apps/cli/**`,
`deploy/**`, `e2e/**`, `go.work`, `VERSION`, this workflow or `.dockerignore` —
on pushes to `main` and on same-repo pull requests (a fork PR skips them, because
they must not execute on the self-hosted runner). A fourth job, **Reap e2e
stacks**, runs afterwards regardless of the outcome and stops the compose project.

| Job | Description |
|---|---|
| **CLI Install Tests** | Full Docker Compose stack. Builds the `memory` CLI from the checked-out source inside the image and installs it in the container, then runs the **allowlisted** CLI tests (below): five of the 149 `TestCLIInstalled_*` tests plus the two `TestOpencode*` tests — seven tests in total. The rest of the CLI suite runs on demand through `runlog test` (see [Local usage](#local-usage)). |
| **API Tests** | Full Docker Compose stack with a server built from the checked-out source, then `go test -v ./tests/api/...` — the whole package, with no `-run` allowlist, so a new API test runs automatically. |
| **Mailgun Transport Test** | The same stack with the email transport switched from SMTP/Mailpit to a local Mailgun stub, so the Mailgun path runs without real credentials or egress. It runs exactly one test — `TestInvite_EmailDeliveredViaMailgunTransport`, selected with `-run` — so the rest of `tests/api/...` runs only in **API Tests**. |

The CLI allowlist is the default value of `TEST_RUN` in `entrypoint.sh`:

```
TestCLIInstalled_(Version|Help|SetToken|StatusAuthenticated|ConfigShowAndSet)|TestOpencode
```

`TestOpencode` is an unanchored pattern, so it also selects
`TestOpencodeInstalled` and `TestOpencodeSeesInstalledSkills`.

The two suites select tests differently. **API Tests** passes no `-run` filter,
so it runs every test in `tests/api/...`. **CLI Install Tests** runs only the
allowlist above, because `entrypoint.sh` supplies that default when `TEST_RUN`
is unset. `runlog test` runs every package under `tests/` on demand — `tests/api`,
`tests/cli`, `tests/integration`, `tests/blueprints`, `tests/docs`, `tests/tools`
and `tests/acp` — which is how a CLI test outside the allowlist gets verified
locally.

The consequence is worth stating plainly: **a green CLI Install Tests check does
not mean an arbitrary `TestCLIInstalled_*` test ran.** When a change adds one,
either link a `runlog test` run against a build that carries the change, or
select it with the `TEST_RUN` input of a manual `workflow_dispatch` on this
workflow.
