## Context

Runlog is a terminal-native Go test observability tool currently embedded in the `emergent.memory.e2e` monorepo. It consists of two layers:

1. **Framework library** (`framework/` — 24 files): `RunLog` struct, SQLite DB layer, CLI runners, HTTP helpers, LLM analyzer, step API. Zero cross-imports back into the monorepo — it's already a clean leaf dependency.
2. **TUI/CLI binary** (`cmd/runlog/` — ~5700 lines): Bubble Tea TUI with 3 tabs, 9 view states, test launcher, inline search. Imports only the DB/analyzer types from framework (not the test-helper surface).

The two layers are cleanly separated in what they consume from framework:
- **TUI binary uses**: `RunDB`, `OpenDB`, `RunRow`, `EventRow`, `ChildEvent`, `ExperimentSummary`, `SuggestionRow`, `GanttData`, `Analyzer`, `NewAnalyzer`, `AnalyzerEvent`, 9 event-kind constants, `LoadDotEnvFrom`, `FormatInt` (22 symbols total — all DB/analyzer/utility)
- **TUI binary does NOT use**: CLI runners, HTTP client, project/agent helpers, auth, server URL, step API — these are test-authoring concerns only

The main blocker to standalone adoption is the hardcoded `knownTests` slice (290 entries, all specific to this project's test suite) and the module path `github.com/emergent-company/emergent.memory.e2e`.

## Goals / Non-Goals

**Goals:**
- Extract runlog into a standalone Go module (`github.com/emergent-company/runlog`) that any Go project can import
- Produce cross-platform binaries via goreleaser so users can install the TUI without building from source
- Remove project-specific hardcoding (knownTests, path assumptions) so the TUI works generically
- Maintain backward compatibility: existing `emergent.memory.e2e` tests continue working with minimal import path changes
- Ship with standalone documentation (README, quick-start, install)

**Non-Goals:**
- Rewriting the TUI (keep Bubble Tea architecture as-is)
- Adding new features to the framework or TUI (extraction only — features come later)
- Supporting non-Go test frameworks (future scope)
- Creating a SaaS/hosted version
- Changing the SQLite schema or breaking existing `runs.db` files
- Splitting the analyzer into a separate module (it stays in framework for now; optional via build tags or lazy initialization)

## Decisions

### Decision 1: Single module with two packages, not a multi-module repo

**Choice**: One Go module `github.com/emergent-company/runlog` containing `package runlog` (library) and `cmd/runlog` (binary).

**Alternatives considered**:
- **Multi-module** (`runlog` lib + `runlog/cmd/runlog` binary): adds version coordination complexity, confuses `go install` resolution. Unnecessary since the binary only imports from its own module.
- **Separate repos** (lib repo + binary repo): even more overhead, no benefit when there's one team.

**Rationale**: The TUI binary only imports from the framework library, never the reverse. A single module with `go install github.com/emergent-company/runlog/cmd/runlog@latest` is the simplest path. Users who only want the library import `github.com/emergent-company/runlog` directly.

### Decision 2: Package layout — flat `runlog` package for library, keep `cmd/runlog` for binary

**Choice**: The framework library becomes the root package (`package runlog`) in the new module. The binary stays at `cmd/runlog/main.go`.

```
github.com/emergent-company/runlog/
├── go.mod                    # module github.com/emergent-company/runlog
├── runlog.go                 # RunLog struct, Section, Printf, CLI, etc.
├── db.go                     # RunDB, OpenDB, migrations
├── cli.go                    # MustRunCLI, CLI runners
├── client.go                 # DoJSON, HTTP helpers
├── step.go                   # Step type
├── step_result.go            # CLIResult, HTTPResult
├── test_context.go           # TestContext, NewTest
├── analyzer.go               # LLM analyzer (optional)
├── ...                       # remaining framework files
└── cmd/
    └── runlog/
        └── main.go           # TUI binary
```

**Alternatives considered**:
- **Keep `framework/` subpackage**: import path becomes `github.com/emergent-company/runlog/framework` — unnecessarily deep for a library that IS the product. "runlog/framework" implies framework is subordinate to something else.
- **`pkg/runlog/`** subpackage: Go convention discourages `pkg/` in modern projects.

**Rationale**: The library IS runlog. Root package = cleanest import (`import "github.com/emergent-company/runlog"`).

### Decision 3: Replace knownTests with auto-discovery + optional config

**Choice**: The TUI discovers tests from the SQLite database (every test that has ever written a run row) and optionally reads a `.runlog.yaml` config file for category overrides and display names.

**Mechanism**:
1. On startup, query `SELECT DISTINCT test_name FROM test_runs` to populate the test list
2. If `.runlog.yaml` exists in the DB directory or working directory, load category/display-name overrides
3. The "launch test" feature reads a configurable test command (default: `go test -v -run <name> ./...`) from config

**Alternatives considered**:
- **Code generation**: Run a tool to scan `_test.go` files and generate a registry. Too fragile — test files may not be in the working directory.
- **AST parsing**: Parse Go test files at runtime. Heavy dependency, slow, breaks on build tags.
- **Keep hardcoded list**: Non-starter for a generic tool.

**Rationale**: The DB already has every test that has ever run. Auto-discovery from DB is zero-config and works for any project. Config file handles the 20% case (custom categories, display names).

### Decision 4: goreleaser for builds, GitHub Actions for CI/CD

**Choice**: Use goreleaser for cross-compilation and GitHub release publishing. GitHub Actions for CI (test, lint) and CD (tag-triggered releases).

**Build matrix**: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.

**Install methods**:
1. `go install github.com/emergent-company/runlog/cmd/runlog@latest`
2. Download binary from GitHub Releases
3. Homebrew tap: `brew install emergent-company/tap/runlog`
4. Install script: `curl -sSfL https://raw.githubusercontent.com/emergent-company/runlog/main/install.sh | sh`

**Rationale**: goreleaser is the de facto standard for Go CLI releases. Pure-Go SQLite (`modernc.org/sqlite`) means no CGO complications for cross-compilation.

### Decision 5: Phased migration for this repo

**Choice**: Two-phase migration of `emergent.memory.e2e`:

1. **Phase A (extraction)**: Create the new `runlog` repo with all code. Keep the old `framework/` in this repo temporarily as a thin re-export wrapper that imports and re-exports from `github.com/emergent-company/runlog`. This lets all existing test files compile without import path changes.
2. **Phase B (cleanup)**: Update all import paths in test files to use the new module directly. Remove the wrapper `framework/` package. Update `cmd/runlog/` to be a simple `go install` reference in docs.

**Alternatives considered**:
- **Big bang**: Change everything at once. High risk — 80+ test files need import path updates simultaneously.
- **Never migrate**: Keep a copy in both repos. Drift is inevitable and unacceptable.

**Rationale**: The re-export wrapper provides a safe transition. Phase B can happen at any pace.

### Decision 6: Versioning — start at v0.1.0

**Choice**: Start at `v0.1.0` to signal "usable but API may evolve." Follow semver. First stable release (`v1.0.0`) after at least one external adopter validates the API.

**Rationale**: The framework API has been in use internally for months and is stable in practice, but external feedback may surface naming or ergonomic issues worth fixing before committing to v1.

## Risks / Trade-offs

- **[Google AI SDK dependency weight]** → The analyzer pulls in `google.golang.org/adk` and `google.golang.org/genai`, adding ~30+ transitive dependencies. Mitigation: Document that the analyzer is optional. Consider build-tag isolation in a future release (not in scope for this change).
- **[Two-repo maintenance overhead]** → Now there are two repos to maintain. Mitigation: This repo becomes a pure consumer. The new repo has its own CI. Clear ownership boundaries.
- **[Breaking import paths]** → Every test file in `emergent.memory.e2e` needs import path updates (Phase B). Mitigation: Phase A's re-export wrapper makes this non-urgent. Can be done incrementally.
- **[DB path discovery]** → Current `resolveDB()` has 6+ hardcoded path candidates including Docker-specific paths (`/test-logs/runs.db`). Mitigation: In the standalone version, simplify to: (1) `$RUNLOG_DB` env var, (2) `.runlog.yaml` config, (3) `./runs.db`, (4) `./logs/runs.db`. Docker-specific paths are a consumer concern.
- **[Namespace squatting]** → `github.com/emergent-company/runlog` module path ties to the org. Mitigation: Acceptable for now. A vanity import path (`runlog.dev/runlog`) can be added later via meta tags.
