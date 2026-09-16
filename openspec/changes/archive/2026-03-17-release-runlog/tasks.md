## 1. New Repository Scaffolding

- [x] 1.1 Create `github.com/emergent-company/runlog` repository on GitHub with MIT license
- [x] 1.2 Initialize `go.mod` with module path `github.com/emergent-company/runlog` and same Go version (1.24.4)
- [x] 1.3 Copy all `framework/*.go` files to the repository root, changing `package e2eframework` to `package runlog`
- [x] 1.4 Copy `cmd/runlog/main.go` to `cmd/runlog/main.go`, updating the framework import path from `github.com/emergent-company/emergent.memory.e2e/framework` to `github.com/emergent-company/runlog`
- [x] 1.5 Run `go mod tidy` to populate `go.sum` with all required dependencies
- [x] 1.6 Verify `go build ./...` succeeds with no errors
- [x] 1.7 Verify `go vet ./...` passes with no warnings

## 2. Remove Hardcoded knownTests

- [x] 2.1 Add a `Config` struct to the library with fields: `Categories map[string][]string`, `TestCommand string`, `DBPath string`
- [x] 2.2 Add `LoadConfig(paths ...string)` function that searches for `.runlog.yaml` in: `$RUNLOG_CONFIG`, DB directory, working directory
- [x] 2.3 Add YAML config file parsing (`.runlog.yaml`) supporting `categories`, `testCommand`, and `db` fields
- [x] 2.4 In the TUI binary, replace the `knownTests` slice with a `SELECT DISTINCT test_name FROM test_runs` query
- [x] 2.5 Merge database-discovered tests with config-file categories (uncategorized tests go to "Uncategorized" group)
- [x] 2.6 Update the test launcher to use configurable `testCommand` (default: `go test -v -run {name} ./...`)
- [x] 2.7 Verify TUI starts and shows tests from an existing `runs.db` without any config file

## 3. Simplify Database Path Resolution

- [x] 3.1 Refactor `resolveDB()` in the TUI binary to use the simplified search order: `$RUNLOG_DB` → `.runlog.yaml` `db` field → `./runs.db` → `./logs/runs.db`
- [x] 3.2 Remove Docker-specific hardcoded paths (`/test-logs/runs.db`, `memory-cli-docker-tests`)
- [x] 3.3 Remove source-file-relative path walking (runtime.Caller-based resolution)
- [x] 3.4 Support `$RUNLOG_DB` environment variable for explicit DB path override
- [x] 3.5 Verify TUI finds DB in each of the four search path positions

## 4. Release Automation

- [x] 4.1 Create `.goreleaser.yaml` with builds for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 with `CGO_ENABLED=0`
- [x] 4.2 Create `.github/workflows/ci.yml` — runs `go vet`, `go build ./...`, `go test ./...` on push to main and PRs
- [x] 4.3 Create `.github/workflows/release.yml` — triggered on `v*` tag push, runs goreleaser to publish GitHub Release with binaries and checksums
- [x] 4.4 Create `install.sh` script that detects OS/arch and downloads the appropriate binary from the latest GitHub Release
- [x] 4.5 Verify `goreleaser build --snapshot --clean` produces all five binary targets
- [x] 4.6 Tag `v0.1.0` and verify the release workflow produces a GitHub Release

## 5. Standalone Documentation

- [x] 5.1 Create `README.md` with project overview, feature highlights, and one-line description
- [x] 5.2 Add installation section with all four methods: `go install`, binary download, install script, homebrew tap
- [x] 5.3 Add quick-start example showing `RunLog` usage in a Go test function
- [x] 5.4 Add feature overview section (TUI, structured logging, SQLite DB, Gantt charts, LLM analyzer, test launcher)
- [x] 5.5 Add `.runlog.yaml` configuration reference section
- [x] 5.6 Create `CHANGELOG.md` with `## [0.1.0]` entry listing initial features
- [x] 5.7 Create `MIGRATION.md` documenting import path changes, config setup, and binary installation for existing `emergent.memory.e2e` users

## 6. Consumer Migration (emergent.memory.e2e)

- [x] 6.1 Add `github.com/emergent-company/runlog v0.1.0` to `go.mod` in this repository
- [x] 6.2 Convert `framework/` into a thin re-export wrapper: each file exports type aliases and variable assignments referencing `github.com/emergent-company/runlog`
- [x] 6.3 Verify `go build ./...` succeeds in this repo with the re-export wrapper
- [~] 6.4 Verify all existing tests pass with `runlog test mcj-emergent` using the wrapper — skipped (requires live server)
- [x] 6.5 Remove `cmd/runlog/` from this repo (replaced by `go install` from new module)
- [x] 6.6 Update `AGENTS.md` and README to reference the standalone `runlog` module for TUI installation
- [x] 6.7 Create `.runlog.yaml` in this repo with the 290 test categories (migrated from the old `knownTests` slice)

## 7. Cleanup (Phase B — can be deferred)

- [x] 7.1 Update all test file imports from `github.com/emergent-company/emergent.memory.e2e/framework` to `github.com/emergent-company/runlog`
- [x] 7.2 Remove the re-export wrapper `framework/` package
- [x] 7.3 Update `helpers_test.go` files to import directly from the new module
- [~] 7.4 Run full test suite to verify no regressions after direct import migration — `go build ./...` and `go vet ./...` pass; full live-server run skipped (requires live server)
- [x] 7.5 Remove unused `framework/` directory and update `.gitignore` / CI as needed
