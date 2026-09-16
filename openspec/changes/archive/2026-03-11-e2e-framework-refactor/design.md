## Context

The `emergent.memory.e2e` repo is a single flat Go package (`dockertests`) with ~8,500 lines across 10 test files. As the suite has grown, helpers have been duplicated across files, large test files have accumulated inlined helpers, and every orchestrator test repeats ~80 lines of identical project-setup boilerplate. There is no shared package other than the root test package, which prevents clean import reuse and creates tight coupling between test logic and infrastructure plumbing.

Current structure highlights:
- `doJSON` / `readBody` duplicated in `install_test.go` and `brave_search_test.go`
- `orchestrator_test.go` is 3,160 lines — 6 tests buried under hundreds of lines of helpers
- 6 orchestrator tests each open with identical project-create / provider-configure / blueprint-install / cleanup boilerplate
- `pollAgentUntilSuccess` lives in `ai_news_blueprint_test.go`; similar logic lives separately in `orchestrator_test.go`
- Gantt / token-usage summary helpers buried in `helpers_test.go` with no clear home
- No contributor skill to guide adding new tests

## Goals / Non-Goals

**Goals:**
- Introduce `framework/` as a proper Go sub-package (`package e2eframework`) importable by all test files
- Introduce `fixtures/` as a Go sub-package (`package e2efixtures`) for reusable test fixtures
- Eliminate all duplicated helper code
- Replace per-test project-setup boilerplate with `framework.CreateProject` / `framework.DeleteProjectOnCleanup`
- Produce a `.opencode/skills/create-e2e-test/` skill for consistent test authoring
- Keep all existing test logic and assertions identical — pure structural refactor
- Update `AGENTS.md` to document the new layout

**Non-Goals:**
- Adding new tests or changing what is tested
- Adding external Go dependencies
- Changing the Docker Compose setup or test runner invocation
- Refactoring the main `emergent.memory` server code

## Decisions

### D1: Separate Go package, not build tags

**Decision:** `framework/` is `package e2eframework` (a normal importable package), not a `_test.go` file or build-tag trick.

**Rationale:** Test helpers in `_test.go` files cannot be imported by other packages. A named sub-package is the standard Go pattern for shared test infrastructure. The `framework` package itself does not contain test functions (`Test*`), so `go test ./...` will compile it but not run anything in it directly.

**Alternative considered:** Keep everything in root package with `_test.go` files using `//go:build` tags — rejected because it still requires duplication and does not enable clean import paths.

### D2: One file per responsibility in `framework/`

**Decision:** Split helpers into focused files rather than one monolithic `framework.go`.

| File | Responsibility |
|------|---------------|
| `client.go` | `DoJSON`, `ReadBody`, `SetAuthHeader`, `DoMCPJSON` |
| `server.go` | `ServerURL`, `SkipIfServerDown`, `E2ETestToken`, `FilteredEnv` |
| `project.go` | `CreateProject`, `DeleteProjectOnCleanup`, `ConfigureGoogleProvider`, `InstallBlueprint` |
| `agents.go` | `CreateAgent`, `TriggerAgent`, `PollUntilSuccess`, `ParseAgentID`, `DumpAgentRunDetails` |
| `graph.go` | `ListByType`, `ListByLabel`, `ListRelationships` |
| `cli.go` | `MustRunCLI`, `MustRunCLIInDir`, `MustRunCLIInDirWithHome`, `LogStatusPreamble` |
| `runlog.go` | `RunLog` struct, Gantt timeline renderer, token usage summary |
| `env.go` | `LoadDotEnv`, `BlueprintEnvVar`, `ParseBlueprintEnvFiles`, skip guards |
| `parse.go` | `ParseProjectID`, `ParseAgentID`, `ParseJSONField`, `CompactRunsOutput`, `AllRunsTerminal` |

**Rationale:** Keeps each file under ~200 lines, makes the API surface scannable, and allows contributors to find helpers by domain without reading everything.

### D3: Exported names in `framework/` (PascalCase)

**Decision:** All framework functions use exported (PascalCase) names since they are in a separate package.

**Rationale:** Standard Go convention. Existing unexported helpers (`doJSON`, `readBody`, etc.) simply get renamed when moved.

### D4: `fixtures/` is a separate package

**Decision:** `bookstore_fixture.go` moves to `fixtures/bookstore.go` as `package e2efixtures`.

**Rationale:** Fixtures are reusable test data, not framework infrastructure. Separating them makes the dependency graph clear: test files can import `fixtures/` without pulling in all of `framework/`.

### D5: Skill lives in the e2e repo

**Decision:** `.opencode/skills/create-e2e-test/` is created inside `emergent.memory.e2e`, not in `emergent.memory`.

**Rationale:** The skill is specific to the e2e repo's patterns and is most useful when an agent is working inside that repo. Co-location follows the same principle as `AGENTS.md`.

## Risks / Trade-offs

- **Compile-time risk during migration:** While refactoring, test files will temporarily reference helpers that have moved. Mitigation: migrate one file at a time, keeping the old stub in the root package as a forwarding alias until the file is fully updated, then delete the stub.
- **`_test.go` restriction:** Helpers currently in `_test.go` files cannot be referenced by the new package directly — they must be copied, not moved, until the test file is updated to import the framework. Mitigation: the migration plan does this file-by-file.
- **Naming collisions:** Some local variable names in test functions share names with the new exported helpers (e.g., a local `projectID` vs `framework.ParseProjectID`). Mitigation: review each call site during migration.
- **No behavioral change guarantee:** Risk of subtle logic change during extraction. Mitigation: the extracted helpers must be byte-for-byte identical to the originals in logic; only signatures and package change.

## Migration Plan

1. Create `framework/` directory and write all files with exported functions (sourced from existing helpers).
2. Create `fixtures/bookstore.go` from `bookstore_fixture.go`.
3. Update test files one at a time:
   - Add `import` for `framework` and/or `fixtures`
   - Replace inline helper calls with `framework.*` / `fixtures.*` equivalents
   - Remove now-redundant local helper definitions
4. After all test files are updated, delete any helper stubs left in root package files.
5. Run `go build ./...` and `go vet ./...` to confirm clean compile.
6. Create `.opencode/skills/create-e2e-test/` with `SKILL.md` and reference docs.
7. Update `AGENTS.md`.

## Open Questions

None — the scope is fully defined by the existing codebase analysis.
