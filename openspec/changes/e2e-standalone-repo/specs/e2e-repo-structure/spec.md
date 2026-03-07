## ADDED Requirements

### Requirement: Standalone Go module
The repository SHALL be a self-contained Go module with path `github.com/emergent-company/emergent.memory.e2e`, requiring Go 1.24+, with no import dependencies on `github.com/emergent-company/emergent.memory`.

#### Scenario: Module initialised
- **WHEN** a developer clones the repo and runs `go mod download`
- **THEN** all dependencies resolve without referencing the monorepo module

---

### Requirement: Repository file layout
The repository root SHALL contain the following files and directories:

| Path | Purpose |
|------|---------|
| `go.mod` | Go module definition |
| `Dockerfile` | Test runner container image |
| `docker-compose.yml` | Full-stack test orchestration (server + client) |
| `entrypoint.sh` | Container entrypoint: installs CLI then runs tests |
| `run_tests.sh` | Local convenience script wrapping docker compose |
| `bookstore_fixture.go` | Shared fixture helper used by install tests |
| `install_test.go` | CLI install / functional tests |
| `production_test.go` | Production smoke tests |
| `.github/workflows/e2e.yml` | GitHub Actions CI workflow |
| `README.md` | Usage, environment variables, CI badge |

#### Scenario: Repository checkout is runnable
- **WHEN** a developer clones the repo and runs `./run_tests.sh`
- **THEN** the Docker Compose stack builds, the server starts healthy, and the test container runs to completion

---

### Requirement: Docker Compose stack
The `docker-compose.yml` SHALL define two services:
- `test-emergent-server`: the Memory API server image (`ghcr.io/emergent-company/memory-server:latest` by default, overridable via `MEMORY_SERVER_IMAGE`).
- `test-emergent-client`: the Go test container built from `Dockerfile`.

The client service SHALL depend on the server being healthy before starting. The server SHALL expose a `/health` endpoint used as the healthcheck. The compose file SHALL mount `./test-logs` into `/test-logs` in the client container.

The `test-emergent-client` service SHALL NOT hardcode the test server URL. `MEMORY_TEST_SERVER` SHALL be read from the host environment with a fallback to the compose-internal hostname: `${MEMORY_TEST_SERVER:-http://test-emergent-server:5300}`.

The `test-emergent-client` service SHALL forward `TEST_RUN` from the host environment to the container: `TEST_RUN: "${TEST_RUN:-}"`. When empty, all tests run; when set, only matching tests run.

The `test-emergent-client` service SHALL NOT define a `command:` override — test execution is controlled entirely by `entrypoint.sh`.

#### Scenario: Stack starts with healthy server
- **WHEN** `docker compose up --build` is run
- **THEN** `test-emergent-server` passes its healthcheck before `test-emergent-client` starts

#### Scenario: Test exit code propagates
- **WHEN** Go tests exit with a non-zero code
- **THEN** `docker compose up --exit-code-from test-emergent-client` exits with the same non-zero code, causing CI to fail

#### Scenario: External server URL overrides compose default
- **WHEN** `MEMORY_TEST_SERVER=http://staging:5300 docker compose up` is run
- **THEN** the test container uses `http://staging:5300` as the server URL rather than the compose-internal hostname

---

### Requirement: Dockerfile builds a runnable test image
The `Dockerfile` SHALL produce an image containing:
- Go 1.24+ toolchain
- `curl`, `bash`, `git`, `ca-certificates`, `jq`
- The `opencode` binary (latest release, installed at build time)
- The test source and pre-downloaded Go module dependencies
- The `entrypoint.sh` script set as `ENTRYPOINT`

The image SHALL NOT pre-install the `memory` CLI at build time.

#### Scenario: Image builds cleanly
- **WHEN** `docker build -t emergent-cli-install-tests .` is run
- **THEN** the build exits 0 and the resulting image contains `opencode` on `PATH`

---

### Requirement: run_tests.sh helper script
The `run_tests.sh` script SHALL support three modes:
- Default (no flags): full stack via Docker Compose.
- `--tests-only`: run the test container only against an externally provided `MEMORY_TEST_SERVER`.
- `--build-only`: build the image without running tests.

The script SHALL forward the `TEST_RUN` env var to the container in all run modes. When `TEST_RUN` is set, only the matching test(s) run; when empty, all tests run.

The script exit code SHALL mirror the Go test exit code.

#### Scenario: Full-stack run via script
- **WHEN** `./run_tests.sh` is executed with Docker available
- **THEN** it builds the image, brings up the compose stack, runs tests, and exits with the test exit code

#### Scenario: Tests-only mode
- **WHEN** `MEMORY_TEST_SERVER=http://localhost:3012 ./run_tests.sh --tests-only` is executed
- **THEN** no server container is started; the test container runs against the provided URL

#### Scenario: Single test via TEST_RUN
- **WHEN** `TEST_RUN=TestCLIInstalled_Version ./run_tests.sh` is executed
- **THEN** only `TestCLIInstalled_Version` runs inside the container; all other tests are skipped

#### Scenario: Single test in tests-only mode
- **WHEN** `TEST_RUN=TestCLIInstalled_Version MEMORY_TEST_SERVER=http://localhost:3012 ./run_tests.sh --tests-only` is executed
- **THEN** only `TestCLIInstalled_Version` runs against the external server

---

### Requirement: entrypoint.sh applies TEST_RUN filter
The `entrypoint.sh` script SHALL pass `-run "$TEST_RUN"` to `go test` when `TEST_RUN` is non-empty, and omit the flag when it is empty (running all tests).

#### Scenario: All tests run when TEST_RUN is empty
- **WHEN** `TEST_RUN` is not set or is an empty string
- **THEN** `go test` runs without a `-run` filter and all tests execute

#### Scenario: Only matching tests run when TEST_RUN is set
- **WHEN** `TEST_RUN=TestCLIInstalled_Version` is set in the container environment
- **THEN** `go test -run TestCLIInstalled_Version` is invoked and only that test executes
