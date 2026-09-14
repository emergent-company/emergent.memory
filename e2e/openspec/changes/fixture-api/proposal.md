## Why

Every CLI test currently opens with 7–8 lines of ritual setup — allocating a home dir, asserting the server is ready, naming a project, creating it, registering cleanup — before reaching the first meaningful assertion. This boilerplate is copy-pasted across 50+ test files, is easy to get wrong (forgotten cleanup, wrong order), and makes tests harder to read and reason about. A declarative fixture API eliminates this overhead and lets tests say *what they need*, not *how to build it*.

## What Changes

- **New `Use(t, ...Option) *Fixture` function** in `github.com/emergent-company/runlog` — the single entry point that wires setup and teardown automatically.
- **New option functions**: `WithProject(prefix)`, `WithSchema(file)`, `WithDocument(file)`, `WithBinary(bin)` — composable, dependency-ordered, teardown via `t.Cleanup`.
- **`Fixture.CLI(args...) CLIResult`** — runs a CLI command scoped to `fx.Home` and auto-injects `--project fx.ProjectID` for project-scoped subcommands.
- **`Fixture.TempFile(name, content string) string`** — writes a temp file and returns its absolute path; no more `os.WriteFile` ceremony in tests.
- **Coexists with existing API** — `NewTest`/`newRunLog` pattern is untouched; new API is additive.

## Capabilities

### New Capabilities

- `fixture-api`: Declarative test fixture API (`Use`, option functions, `Fixture` type, `CLI`, `TempFile`) implemented in `github.com/emergent-company/runlog`.

### Modified Capabilities

- `e2e-framework`: The framework gains the `Use`/`Fixture` API as a new surface; no existing requirements change but new requirements are added for the fixture entry-point and option composition model.

## Impact

- **`/root/runlog/fixture.go`** — new file; all new types and functions land here.
- **`/root/runlog/`** — no existing files modified; backward compatible.
- **`/root/emergent.memory.e2e/tests/cli/`** — one or two test files migrated as working demonstrations; remaining tests migrate incrementally.
- No new external dependencies; standard library only.
- `DeleteProjectOnCleanup` / `CreateProject` already handle daemon registration — `WithProject` composes on top of them unchanged.
