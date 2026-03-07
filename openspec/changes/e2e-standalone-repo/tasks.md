## 1. Create the new GitHub repository

- [x] 1.1 Create `emergent-company/emergent.memory.e2e` as a new public GitHub repository (empty, no template)
- [x] 1.2 Clone the new repo locally
- [ ] 1.3 Add `MEMORY_PROD_TEST_TOKEN` as a repository secret in the new repo's GitHub settings

## 2. Initialise the Go module

- [x] 2.1 Create `go.mod` with module path `github.com/emergent-company/emergent.memory.e2e` and `go 1.24.0`
- [x] 2.2 Run `go mod tidy` to verify the module is valid (no imports, no `go.sum` needed yet)

## 3. Migrate test source files

- [x] 3.1 Copy `tools/cli/tests/docker/install_test.go` into the repo root (package remains `dockertests`)
- [x] 3.2 Copy `tools/cli/tests/docker/production_test.go` into the repo root
- [x] 3.3 Copy `tools/cli/tests/docker/bookstore_fixture.go` into the repo root
- [x] 3.4 Run `go build ./...` in the new repo to confirm the package compiles cleanly

## 4. Fix test isolation (per-test HOME)

- [x] 4.1 In `mustRunCLIInDir()`, extend the env slice returned by `filteredEnv()` to also set `HOME=<t.TempDir()>` so every CLI subprocess gets an isolated home directory
- [x] 4.2 Verify `~/.memory/credentials.json` path in `TestCLIInstalled_SetToken` (line 121) and `TestProduction_SetToken` (line 94) still resolves correctly — update both to use `filepath.Join(home, ".memory", "credentials.json")` where `home` is read from the subprocess env, not `os.Getenv("HOME")`
- [x] 4.3 Write a quick manual test: run `TestCLIInstalled_SetToken` and `TestCLIInstalled_ProjectsList` in the same `go test` invocation and confirm neither test sees the other's credentials file

## 5. Add single-test support (TEST_RUN)

- [x] 5.1 In `entrypoint.sh`, replace the hardcoded `exec go test -v -timeout 10m ./...` with `exec go test -v -timeout 10m ${TEST_RUN:+-run "$TEST_RUN"} .`
- [x] 5.2 In `docker-compose.yml`, remove the `command:` override on `test-emergent-client` (it bypasses `entrypoint.sh`) and add `TEST_RUN: "${TEST_RUN:-}"` to the client's `environment:` block
- [x] 5.3 In `run_tests.sh` `--tests-only` mode (`docker run` invocation), add `-e TEST_RUN="${TEST_RUN:-}"` to the flags
- [x] 5.4 Verify: single-test mode confirmed in CI dispatch — `TEST_RUN=TestCLIInstalled_Version` ran exactly one test and exited 0

## 6. Make server URL overridable in Docker Compose

- [x] 6.1 In `docker-compose.yml`, change the `test-emergent-client` environment entry to `MEMORY_TEST_SERVER: "${MEMORY_TEST_SERVER:-http://test-emergent-server:3002}"`
- [x] 6.2 Verify: server URL override is supported via env var

## 7. Migrate Docker infrastructure

- [x] 7.1 Copy `tools/cli/tests/docker/Dockerfile` into the repo root
- [x] 7.2 Apply the updated `docker-compose.yml` (from tasks 5 and 6) into the repo root
- [x] 7.3 Copy the updated `entrypoint.sh` (from task 5.1) into the repo root; ensure execute bit is set
- [x] 7.4 Copy the updated `run_tests.sh` (from task 5.3) into the repo root; ensure execute bit is set
- [x] 7.5 Add `.gitignore` and `.dockerignore` to the repo root

## 8. Add GitHub Actions CI workflow

- [x] 8.1 Create `.github/workflows/e2e.yml` with `integration` job: checkout → build server image → `./run_tests.sh` → upload `test-logs/` on failure
- [x] 8.2 Add `production` job to the same workflow: runs `go test -v -run TestProduction_ -timeout 2m .` with `MEMORY_PROD_TEST_TOKEN` from secret; skipped when secret absent
- [x] 8.3 Set workflow triggers: `push` to `main`, `pull_request`, and `workflow_dispatch` (with `TEST_RUN` input)
- [x] 8.4 Configure the `integration` job to build `memory-server:ci` from monorepo source

## 9. Add README

- [x] 9.1 Write `README.md` covering: purpose, environment variables, local usage, single-test example, and CI badge

## 10. Verify CI passes in the new repo

- [x] 10.1 Push to `main` and confirm the `integration` job passes (all install tests pass)
- [x] 10.2 Production job passes (skips cleanly when no secret; `TestProduction_ServerHealth` passes)
- [x] 10.3 PR CI verified — both jobs ran correctly on a test PR
- [x] 10.4 Single-test mode verified in CI dispatch: `TEST_RUN=TestCLIInstalled_Version` ran exactly one test

## 11. Remove the old tests from the monorepo

- [x] 11.1 Delete `tools/cli/tests/docker/` from the `emergent.memory` monorepo
- [x] 11.2 Commit the deletion with message referencing the new repo
- [x] 11.3 Removed `production-smoke` job from `.github/workflows/cli.yml`; removed `tools/cli/tests/docker` from `go.work`
- [x] 11.4 Monorepo `Test` job passes after deletion (pre-existing lint failure in `dumper_test.go` is unrelated)
