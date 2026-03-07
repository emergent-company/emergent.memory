## ADDED Requirements

### Requirement: GitHub Actions workflow file
The repository SHALL contain `.github/workflows/e2e.yml` defining a CI workflow that runs on push to `main` and on pull requests.

#### Scenario: Workflow triggers on push and PR
- **WHEN** a commit is pushed to `main` or a pull request is opened
- **THEN** the GitHub Actions workflow is triggered automatically

---

### Requirement: Integration job runs full Docker Compose stack
The workflow SHALL include an `integration` job that:
1. Checks out the repository.
2. Logs in to GHCR (using `GITHUB_TOKEN`) to pull the server image.
3. Runs `./run_tests.sh` (full Docker Compose stack).
4. Uploads `./test-logs/` as a workflow artifact on failure.

#### Scenario: Integration job passes
- **WHEN** the `integration` job runs and all install tests pass
- **THEN** the job exits 0

#### Scenario: Test logs uploaded on failure
- **WHEN** any install test fails
- **THEN** the `test-logs/` directory is uploaded as a GitHub Actions artifact named `e2e-test-logs`

---

### Requirement: Production job runs smoke tests with secret token
The workflow SHALL include a `production` job that:
1. Runs only when the `MEMORY_PROD_TEST_TOKEN` secret is available (i.e., not a fork PR with no secrets).
2. Executes `go test -v -run TestProduction_ -timeout 2m ./...` directly (no Docker — tests hit the live production server).
3. Sets `MEMORY_PROD_TEST_TOKEN` from the repository secret.

The `production` job SHALL be independent of (not depend on) the `integration` job.

#### Scenario: Production job skips in forks
- **WHEN** the workflow runs in a forked PR where `MEMORY_PROD_TEST_TOKEN` is not set
- **THEN** the production tests are reported as skipped and the job still exits 0

#### Scenario: Production job passes with valid token
- **WHEN** `MEMORY_PROD_TEST_TOKEN` is set and the production server is reachable
- **THEN** all `TestProduction_*` tests pass and the job exits 0

---

### Requirement: MEMORY_SERVER_IMAGE is configurable in CI
The integration job SHALL support overriding the Memory server Docker image via the `MEMORY_SERVER_IMAGE` environment variable, defaulting to `ghcr.io/emergent-company/memory-server:latest`.

#### Scenario: Default image used when variable absent
- **WHEN** `MEMORY_SERVER_IMAGE` is not set
- **THEN** the compose stack uses `ghcr.io/emergent-company/memory-server:latest`

#### Scenario: Custom image used when variable set
- **WHEN** `MEMORY_SERVER_IMAGE` is set to a specific digest or tag
- **THEN** the compose stack pulls and uses that image

---

### Requirement: Workflow runs Go tests with a timeout
The integration job SHALL pass `-timeout 10m` to `go test` to prevent indefinitely hanging CI runs.

#### Scenario: Timeout applied
- **WHEN** the test container runs `go test`
- **THEN** the command is invoked with `-timeout 10m`
