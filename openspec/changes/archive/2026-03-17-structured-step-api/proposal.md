## Why

Test authors currently do manual dual bookkeeping on every CLI call: execute the command, separately tell RunLog about it, then write inline `strings.Contains` + `t.Errorf` assertions. The framework provides building blocks (`MustRunCLIInDirWithHome`, `ParseProjectID`, `rl.CLI`, `rl.Section`) but the test author must wire them together every time — 8 lines of identical preamble per test, hand-formatted invocation strings for logging, and 80+ inline assertion patterns across the suite. This makes tests verbose, error-prone, and coupled to the current `memory` CLI. To release runlog as a standalone product usable by other Go projects, the test authoring layer needs to become a self-contained, opinionated API where steps automatically handle recording, and the CLI binary name is configurable rather than hardcoded.

## What Changes

- Introduce a `TestContext` type that bundles RunLog, isolated home dir, auth setup, and optional project lifecycle into a single `NewTest(t, opts)` call — replacing 8 lines of preamble per test
- Introduce a `Step` type with typed actions (`CLI`, `CLIExpectError`, `HTTP`, `WriteFile`, `Log`) that automatically record invocations, stdout/stderr/exit code, and duration to the RunLog DB — eliminating manual `rl.CLI()` calls
- Introduce a `CLIResult` type with chainable assertions (`.Contains()`, `.ParseID()`, `.JSONField()`, `.ExitCode()`) that self-report to RunLog — replacing inline `strings.Contains` + `t.Errorf`
- Introduce an `HTTPResult` type with chainable assertions (`.Status()`, `.JSONField()`, `.BodyContains()`) for API-level steps
- Make the CLI binary name configurable via `TestOpts.Binary` (default: `"memory"`) so external projects can use runlog with their own CLI
- Keep the existing `RunLog`, `RunDB`, and TUI layers unchanged — the new API wraps them, does not replace them

## Capabilities

### New Capabilities
- `test-context`: High-level test lifecycle manager (`NewTest`, `Done`, auto-preamble) with configurable binary name and optional project setup
- `step-actions`: Typed step actions (`CLI`, `CLIExpectError`, `HTTP`, `WriteFile`, `Log`) that auto-record to RunLog
- `step-assertions`: Chainable assertion API on `CLIResult` and `HTTPResult` that self-report pass/fail to RunLog

### Modified Capabilities
- `e2e-framework`: The framework package gains new exported types (`TestContext`, `Step`, `CLIResult`, `HTTPResult`, `TestOpts`) and the `cli.go` helpers need to support configurable binary names instead of hardcoded `"memory"`

## Impact

- **framework/ package**: New files for `TestContext`, `Step`, `CLIResult`, `HTTPResult`. Existing `cli.go` modified to accept binary name parameter.
- **Existing tests**: No breaking changes. Existing tests continue to work unchanged. New tests can use the new API. Migration is incremental.
- **External consumers**: Projects importing `github.com/emergent-company/emergent.memory.e2e/framework` gain access to the structured step API by setting `TestOpts.Binary` to their CLI name.
- **RunLog DB/TUI**: No schema changes. The new step types emit the same event kinds (`cli`, `log`, `section`) that the TUI already renders.
