## Why

The e2e test suite has grown to ~8,500 lines across 10 files in a single flat `dockertests` package, with duplicated helpers, massive test files (one at 3,160 lines), and ~80 lines of copy-pasted project setup boilerplate repeated in every orchestrator test. This makes tests hard to read, maintain, and extend — and there is no skill to guide contributors in writing new tests consistently.

## What Changes

- Extract all shared helpers from test files into a new `framework/` sub-package (`package e2eframework`) with well-defined responsibilities per file
- Extract `bookstore_fixture.go` into a `fixtures/` sub-package (`package e2efixtures`)
- Remove duplicated `doJSON` / `readBody` helpers from `brave_search_test.go` and `install_test.go`
- Replace inline project-setup boilerplate in all orchestrator tests with `framework.CreateProject` / `framework.DeleteProjectOnCleanup`
- Consolidate `pollAgentUntilSuccess` (ai_news) and `dumpAgentRunDetails` (orchestrator) into `framework/agents.go`
- Move Gantt / token-usage helpers from `helpers_test.go` into `framework/runlog.go`
- Move CLI runner helpers (`mustRunCLI*`) into `framework/cli.go`
- Move server utilities (`serverURL`, `skipIfServerDown`, `e2eTestToken`, `filteredEnv`) into `framework/server.go`
- Move `loadDotEnv` / blueprint env helpers into `framework/env.go`
- Move parse utilities (`parseProjectID`, `parseAgentID`, `compactRunsOutput`, `allRunsTerminal`) into `framework/parse.go`
- Create `.opencode/skills/create-e2e-test/` skill with `SKILL.md` and reference docs so contributors can generate new test files consistently
- Update `AGENTS.md` to document the new package layout

## Capabilities

### New Capabilities

- `e2e-framework`: A reusable `e2eframework` Go package under `framework/` exposing HTTP client helpers, server utilities, project lifecycle helpers, agent polling, graph query helpers, CLI runners, run-log/Gantt utilities, env loading, and parse utilities — importable by all test files
- `e2e-fixtures`: A `e2efixtures` Go package under `fixtures/` housing reusable test fixtures (starting with `bookstore_fixture.go`)
- `create-e2e-test-skill`: An OpenCode skill at `.opencode/skills/create-e2e-test/` that guides contributors through creating new e2e test files using the framework package

### Modified Capabilities

## Impact

- All 10 existing `*_test.go` files updated to import `framework/` and `fixtures/` instead of using inline helpers
- `go.mod` unchanged (no new external dependencies)
- `AGENTS.md` updated to reflect new package layout
- No test logic or assertions changed — pure structural refactor
