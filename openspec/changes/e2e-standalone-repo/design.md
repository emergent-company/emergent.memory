## Context

The monorepo (`emergent.memory`) contains a `tools/cli/tests/docker/` directory that is a separate Go module housing Docker-based CLI install tests. These tests exercise the CLI binary as installed by the `install.sh` script — exactly as a real user would — against a live Memory server in a container. They currently live in the monorepo for convenience but have no code dependencies on it; they test external binaries and call HTTP endpoints. Keeping them in the monorepo forces CI to check out the full monorepo to run a test suite that is entirely about released artifacts. The goal is to move them to their own repository so they can evolve, be versioned, and run independently.

The in-process server e2e tests (`apps/server/tests/e2e/`) are intentionally **not moved**: they depend on `testutil.BaseSuite`, transaction rollback for isolation, and Go package imports from the server — they belong in the monorepo.

The existing tests have three usability problems that the migration must fix: shared `~/.memory/credentials.json` between tests (order-dependent outcomes), no mechanism to run a single test, and a hardcoded server URL in `docker-compose.yml` that can't be overridden from the host environment.

## Goals / Non-Goals

**Goals:**
- Create `emergent-company/emergent.memory.e2e` as a standalone Go module.
- Migrate all 14 test functions (10 install, 4 production smoke) from `tools/cli/tests/docker/` into the new repo.
- Fix test isolation: each test gets its own `HOME` directory so `~/.memory/credentials.json` is never shared.
- Add single-test support: `TEST_RUN` env var threads a `-run` filter through `entrypoint.sh` and `run_tests.sh`.
- Make server URL overridable: `docker-compose.yml` reads `MEMORY_TEST_SERVER` from the host environment.
- Migrate the Docker infrastructure (`Dockerfile`, `docker-compose.yml`, `entrypoint.sh`, `run_tests.sh`, `bookstore_fixture.go`).
- Add a GitHub Actions CI workflow that builds the Docker stack, runs tests, and gates production smoke tests on a repository secret.
- Remove `tools/cli/tests/docker/` from the monorepo.

**Non-Goals:**
- Moving `apps/server/tests/e2e/` out of the monorepo.
- Changing test logic or adding new tests (that is follow-on work).
- Setting up an automated trigger from the monorepo CI into the new repo (future work).
- Abstracting a shared test helper library across repos.

## Decisions

### 1. Standalone repo, not a Git submodule

**Decision:** New top-level GitHub repository, not a submodule of `emergent.memory`.

**Rationale:** Submodules add checkout complexity and don't decouple CI. A standalone repo has its own issues, PRs, and CI runs — it can pin to a specific CLI release or `latest` without touching the monorepo.

**Alternatives considered:**
- *Git submodule*: Rejected — submodule checkout is fragile in CI, doesn't decouple CI runs.
- *Keep in monorepo*: Rejected — defeats the purpose; tests still require the full monorepo checkout.

---

### 2. Go module path: `github.com/emergent-company/emergent.memory.e2e`

**Decision:** Use this as the module path (mirrors the monorepo convention).

**Rationale:** Consistent naming, no import path collisions. The module has zero imports from `emergent.memory`.

---

### 3. CLI binary sourced via `install.sh` at container runtime (not build time)

**Decision:** Keep the existing approach — `entrypoint.sh` downloads and runs `install.sh` from GitHub Releases at container startup.

**Rationale:** This is the whole point of the test: verify that the install script works for real end users. Pre-installing the binary at image build time would test a cached artifact, not the actual install path. The trade-off is that the image is not fully reproducible (it downloads the latest release), but this is intentional and acceptable for a smoke test.

**Alternatives considered:**
- *Install at build time with a pinned version*: Rejected — would require updating `Dockerfile` on every release; also doesn't test the install script itself.
- *Copy binary from a CI artifact*: Possible for a "test this specific release" workflow but not the right default.

---

### 4. Production smoke tests gated on `MEMORY_PROD_TEST_TOKEN` secret

**Decision:** Production tests skip automatically when the env var is absent (existing behavior, preserved as-is).

**Rationale:** The production token is sensitive. Local developers and forks run without it; it is injected only in trusted CI via a repository secret. No code change needed.

---

### 5. GitHub Actions workflow: two jobs (`integration`, `production`)

**Decision:** Split into two separate jobs in the same workflow:
- `integration`: Runs the Docker Compose full-stack test (server + client containers).
- `production`: Runs the production smoke tests against `https://memory.emergent-company.ai`, gated on the `MEMORY_PROD_TEST_TOKEN` secret being set.

**Rationale:** Separating jobs gives clear pass/fail signal per concern and allows the `production` job to be skipped in forks without failing the workflow.

### 6. Per-test HOME isolation via `t.TempDir()`

**Decision:** In `mustRunCLIInDir()`, add `HOME=<t.TempDir()>` to the subprocess `cmd.Env` slice alongside the existing `filteredEnv()` output.

**Rationale:** All CLI invocations that write credentials use `$HOME/.memory/credentials.json`. By giving each test its own `HOME`, every `set-token` call writes to a fresh, test-scoped directory that `t.Cleanup` removes automatically. This makes all 14 tests unconditionally independent with zero changes to test logic or assertions.

**Alternatives considered:**
- *Pass `--config <tempfile>` to every `memory` invocation*: Rejected — requires modifying every `mustRunCLI` call site and assumes the CLI consistently honours a `--config` flag across all sub-commands.
- *Call `t.Setenv("HOME", t.TempDir())`*: Rejected — `t.Setenv` modifies the test process's own environment, which affects all concurrently-running tests and the `logStatusPreamble` goroutine.

**What changes in code:**
- `filteredEnv()` (or its call site in `mustRunCLIInDir`) appends `"HOME=" + t.TempDir()` to the returned slice.
- No changes to any test function body.

---

### 7. Single-test selection via `TEST_RUN` env var

**Decision:** Thread a `TEST_RUN` env var through the entire run stack as a `-run` filter to `go test`.

**Changes required:**
- `entrypoint.sh`: change `exec go test -v -timeout 10m ./...` to `exec go test -v -timeout 10m ${TEST_RUN:+-run "$TEST_RUN"} ./...`
- `docker-compose.yml`: remove the hardcoded `command:` override (which bypasses `entrypoint.sh`'s filter) and add `TEST_RUN: "${TEST_RUN:-}"` to the client's `environment:` block.
- `run_tests.sh`: pass `-e TEST_RUN="${TEST_RUN:-}"` in both the `docker run` invocation and ensure the compose path inherits it from the host environment.

**Usage:**
```bash
TEST_RUN=TestCLIInstalled_Version ./run_tests.sh
TEST_RUN=TestCLIInstalled_Version ./run_tests.sh --tests-only
```
An empty `TEST_RUN` runs all tests (the default).

**Alternatives considered:**
- *Accept a positional argument to `run_tests.sh`*: Rejected — mixing positional args with the existing flag-style interface (`--tests-only`, `--build-only`) is awkward. An env var is composable and already idiomatic in this codebase (`MEMORY_TEST_SERVER`, `MEMORY_SERVER_IMAGE`).

---

### 8. Overridable server URL in Docker Compose

**Decision:** Change `docker-compose.yml` `test-emergent-client.environment.MEMORY_TEST_SERVER` from the hardcoded string `"http://test-emergent-server:5300"` to `"${MEMORY_TEST_SERVER:-http://test-emergent-server:5300}"`.

**Rationale:** The hardcoded value means the compose stack can only test against the bundled server container. With the override, a developer can run:
```bash
MEMORY_TEST_SERVER=http://my-staging-server:5300 ./run_tests.sh
```
and the test container will hit that server instead of spinning up a local one (it still spins up the local server container, which is harmless — use `--tests-only` to skip it entirely).

**Alternatives considered:**
- *Add a new `--server` flag to `run_tests.sh`*: Rejected — `MEMORY_TEST_SERVER` is already the established env var for this purpose; a flag would be redundant.

## Risks / Trade-offs

- **Docker image availability**: `ghcr.io/emergent-company/memory-server:latest` must be publicly pullable (or the workflow must authenticate to GHCR). If the image is private, the CI workflow needs `GHCR_TOKEN`. → *Mitigation*: Add GHCR login step in CI; document the requirement.
- **Install script flakiness**: `install.sh` downloads from GitHub Releases at runtime; network failures or a bad release can break the test. → *Mitigation*: Tests already have timeouts and skip-on-down logic. CI flakiness is acceptable for a smoke test.
- **Drift from monorepo**: Once decoupled, changes to the CLI or server may not automatically trigger the e2e tests. → *Mitigation*: Document that this repo should be run manually or on a schedule (nightly) as part of the release checklist. Automated cross-repo triggers are out of scope for now.
- **`bookstore_fixture.go` has no external dependencies**: It uses only stdlib, so migration is straightforward with no risk of import breakage.

## Migration Plan

1. Create `emergent-company/emergent.memory.e2e` repository on GitHub.
2. Copy all files from `tools/cli/tests/docker/` into the new repo root.
3. Update `go.mod` module path to `github.com/emergent-company/emergent.memory.e2e`.
4. Add `.github/workflows/e2e.yml` GitHub Actions workflow.
5. Verify CI passes in the new repo (both `integration` and `production` jobs).
6. Delete `tools/cli/tests/docker/` from the monorepo in a single commit.
7. Update monorepo `README` / CI docs to reference the new repo.

**Rollback:** The monorepo deletion commit can be reverted at any point before step 7. If the new repo CI is broken, the old tests remain in the monorepo until fixed.

## Open Questions

- Should the `integration` job also trigger on pushes to the monorepo (via `repository_dispatch` or a scheduled cron)? Deferred to follow-on work.
- Does `ghcr.io/emergent-company/memory-server:latest` require authentication to pull in the new repo's CI? Needs verification when setting up the workflow.
