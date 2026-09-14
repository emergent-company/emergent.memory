## Context

The `github.com/emergent-company/runlog` framework already has two test entry-point styles:

1. **Raw pattern** (90% of tests): `newRunLog(t)` + manual `t.TempDir()` + `requireServerReady` + `createProject` + `deleteProjectOnCleanup` — 7–8 lines of identical ritual in every test body.
2. **TestContext pattern** (~5 tests): `NewTest(t, TestOpts{})` + `tc.Step(...)` + `s.CLI(...)` — structured but still requires wrapping every CLI call in a named Step.

The raw pattern is the majority pattern and has proven too verbose. The `TestContext` pattern is better but `Step` adds indirection without proportional value for most tests — the naming/gantt structure is useful for long multi-step tests, not for 3-line assertion tests.

What's missing is a thin **fixture layer**: declare dependencies, get a ready context, run CLI calls directly.

## Goals / Non-Goals

**Goals:**
- Single `Use(t, ...Option)` entry point replacing 7–8 lines of ritual
- Composable option functions: `WithProject`, `WithSchema`, `WithDocument`, `WithBinary`
- `Fixture.CLI(args...)` runs the memory binary scoped to `fx.Home` — no argument injection needed, project/token/server are already in the config file
- `Fixture.TempFile(name, content)` writes a file to a temp dir and returns its path
- All teardown via `t.Cleanup` (LIFO, automatic, no `defer`)
- Coexist with `NewTest`/`TestContext` — no removal of existing API

**Non-Goals:**
- Replacing `TestContext.Step` / `RunLog` / Gantt — those remain for structured long-running tests
- Fixture dependency injection framework (no DAG, no scope nesting)
- Schema/document upload actually calling API for now — `WithSchema`/`WithDocument` may be stubs in v1 if the API shape is complex; `WithProject` is the priority
- Test migration — only 1–2 demo tests are converted in this change

## Decisions

### Decision: `Use` is a thin wrapper over existing primitives, not a rewrite

`Use` calls `NewRunLog`, `t.TempDir`, `RequireServerReady`, `SetupCLIAuth`, `CreateProject`, `DeleteProjectOnCleanup` — all existing functions. No new infrastructure; just the entry point.

**Alternative considered**: Embed `*TestContext` inside `Fixture`. Rejected — `TestContext` requires every CLI call to go through a `Step`, which means giving every mini-assertion a name. `Fixture` is flat by design.

### Decision: `Fixture.CLI` does NOT inject `--project` — the config file already carries it

`CreateProject` already calls `memory config set project_id <id>` in the isolated `home`. The CLI reads `project_id` from `~/.memory/config.yaml` for every command. The `--project` flag in CLI help reads "overrides config/env" — meaning the config value is the default. No injection is needed.

`fx.CLI` simply runs the binary with `HOME=fx.Home`. The project, token, and server URL are all already set in the config file by `Use` setup. Tests call `fx.CLI("documents", "list")` and it works with no extra arguments.

**Alternative considered**: Auto-inject `--project` for known subcommands. Rejected — the CLI already reads from config; injection would be redundant and create a maintained whitelist that drifts over time.

### Decision: `Option` is `func(*fixture)` (unexported receiver)

Options receive a pointer to the unexported `fixture` struct and mutate it before `Use` runs the setup phase. This is the standard Go functional-options pattern.

`Fixture` (exported) is the view the test holds. `fixture` (unexported) is the mutable builder. `Use` runs options, builds, returns `*Fixture`.

**Alternative considered**: Builder methods on `*Fixture` (`.WithProject(...).Use()`). Rejected — functional options are more composable and idiomatic for this use case.

### Decision: `WithSchema` and `WithDocument` upload via CLI in the setup phase

`WithSchema(file)` runs `memory schemas upload <file>` after project creation. `WithDocument(file)` runs `memory documents upload <file>`. Failures are fatal (`t.Fatal`). This keeps the implementation in the same CLI-first style as the tests themselves.

### Decision: `Fixture` holds a `*RunLog` for structured logging

Even without `Step`, logging CLI output to RunLog is valuable for the TUI inspector. `fx.CLI` logs each call via `rl.CLIStepErr`. The `RunLog` is created in `Use` and `rl.Close` is registered via `t.Cleanup`.

## Risks / Trade-offs

- **`WithSchema`/`WithDocument` require the project to exist first**: Option application order matters. Documented convention: `WithProject` must appear before `WithSchema`/`WithDocument`. Mitigation: `Use` validates this and panics with a clear message if violated.
- **`Fixture` does not embed `*TestContext`**: Tests using `Use` cannot access `tc.Step`. This is intentional, but if a test grows to need structured steps it must be refactored to `NewTest`. Trade-off: simplicity vs. upgrade path. Acceptable given the two APIs coexist.
