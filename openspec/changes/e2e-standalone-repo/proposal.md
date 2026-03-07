## Why

The Docker-based CLI install tests (`tools/cli/tests/docker/`) live inside the monorepo but test external binaries and require a live server — they are fundamentally integration tests that don't belong next to unit and in-process e2e tests. A standalone repo lets these tests evolve independently, be versioned separately, run in CI without pulling the full monorepo, and serve as the canonical end-to-end quality gate for released artifacts (the CLI binary and the server image).

The existing tests also have three usability problems that must be fixed during the migration: (1) there is no way to run a single test — all 14 always run together, (2) tests share `~/.memory/credentials.json` so outcomes depend on execution order, and (3) the test server URL is hardcoded in `docker-compose.yml` making it impossible to point at an external server without editing files.

## What Changes

- **New repository** `emergent-company/emergent.memory.e2e` is created as a standalone Go module (`github.com/emergent-company/emergent.memory.e2e`).
- **Migrate** the existing Docker-based CLI install tests from `tools/cli/tests/docker/` into the new repo, preserving all 14 test functions (10 install + 4 production smoke tests).
- **Fix test isolation**: `mustRunCLIInDir()` sets `HOME=<t.TempDir()>` in each subprocess environment so `~/.memory/credentials.json` is never shared between tests.
- **Add single-test support**: `entrypoint.sh` and `run_tests.sh` thread a `TEST_RUN` environment variable through as a `-run` flag to `go test`, enabling `TEST_RUN=TestCLIInstalled_Version ./run_tests.sh`.
- **Make server URL overridable**: `docker-compose.yml` reads `MEMORY_TEST_SERVER` from the host environment (falling back to the compose-internal hostname) so any server can be targeted without editing files.
- **Add CI workflow** (GitHub Actions) in the new repo that:
  - Spins up the Memory server via Docker Compose
  - Runs the full test suite against the live container
  - Runs the production smoke tests against `https://memory.emergent-company.ai` (token-gated)
- **Remove** `tools/cli/tests/docker/` from the monorepo once the new repo is live and CI is green.
- The monorepo's in-process `apps/server/tests/e2e/` tests are **not moved** — they depend on `testutil.BaseSuite` and transaction rollback and are tightly coupled to the server codebase.

## Capabilities

### New Capabilities

- `e2e-repo-structure`: Repository layout, Go module definition, `docker-compose.yml`, and `Dockerfile` for the standalone e2e repo — including `TEST_RUN` env var threading and overridable `MEMORY_TEST_SERVER`.
- `cli-install-tests`: Migration and ownership of the 10 CLI install/smoke test functions with per-test HOME isolation and independent execution.
- `production-smoke-tests`: Migration and ownership of the 4 production smoke tests from `tools/cli/tests/docker/production_test.go`.
- `ci-pipeline`: GitHub Actions workflow that builds, spins up the stack, runs tests, and gates on a production token secret.

### Modified Capabilities

(none — no existing spec-level requirements change)

## Impact

- `tools/cli/tests/docker/` — deleted from monorepo after migration.
- New repo: `emergent-company/emergent.memory.e2e` — Go 1.25+, no shared module with the monorepo.
- CI: new GitHub Actions workflow in the new repo; existing monorepo CI workflows are unaffected.
- No API, schema, or backend code changes.
