## Context

The `framework/` package currently provides low-level building blocks: `MustRunCLIInDirWithHome` runs a CLI command, `RunLog.CLI` logs it, `ParseProjectID` extracts an ID, `RunLog.Section` labels a block. Test authors manually compose these into every test — wiring execution to logging to assertion to parsing. This works, but produces tests that are 50-60% boilerplate.

The framework hardcodes `"memory"` as the CLI binary name in `cli.go`. The `RunLog` struct, `RunDB` (SQLite), and TUI are generic — they don't care what CLI is being tested. But the test authoring layer is tightly coupled to the memory CLI's auth model, project lifecycle, and output formats.

To ship runlog as a standalone product, we need to split the framework into two layers:
1. **Generic test authoring API** — `TestContext`, `Step`, `CLIResult`, `HTTPResult` — usable by any Go project testing any CLI
2. **Memory-specific helpers** — project creation, auth setup, blueprint install — specific to this repo

The generic layer is the product. The memory-specific layer is a consumer of it.

### Current test anatomy (agent-definitions CRUD)

```
Lines 1-3:   runlog setup (NewRunLog, Cleanup, Describe)         — preamble
Lines 4-5:   TempDir, requireServerReady                          — preamble
Lines 6-9:   createProject, deleteProjectOnCleanup, Printf        — preamble
Lines 10-14: CLI create, rl.CLI log, parseID, empty check, Printf — action+log+parse
Lines 15-19: CLI get, rl.CLI log, strings.Contains, Errorf        — action+log+assert
Lines 20-24: CLI delete, rl.CLI log, ToLower, Contains, Errorf    — action+log+assert
```

9 lines of preamble, 15 lines of action+log+assert that could be 9. The ratio gets worse in CRUD tests with more entities.

## Goals / Non-Goals

**Goals:**
- Reduce per-test boilerplate from ~8 lines of preamble to 1 (`NewTest`)
- Eliminate manual `rl.CLI()` calls — step actions auto-log to RunLog
- Eliminate inline `strings.Contains` + `t.Errorf` — chainable assertions self-report
- Make CLI binary name configurable so external projects can use the step API
- Keep the new API as a layer on top of existing `RunLog` and `RunDB` — no DB schema changes, no TUI changes
- Maintain backward compatibility — existing tests compile and run unchanged

**Non-Goals:**
- Not a DSL, YAML format, or Gherkin layer — tests remain plain Go functions
- Not replacing `go test` as the runner — runlog observes, it doesn't orchestrate
- Not splitting into a separate Go module yet — the new types live in `framework/` alongside existing code. Module extraction is a future step.
- Not migrating all existing tests — new tests use the new API, migration is optional and incremental
- Not adding new TUI views or DB schema for step-level data — steps emit existing event kinds (`cli`, `log`, `section`)

## Decisions

### 1. TestContext wraps RunLog, not replaces it

**Decision**: `TestContext` holds a `*RunLog` field and delegates all event recording to it. It does NOT create a parallel recording path.

**Rationale**: The RunLog → RunDB → TUI pipeline is stable and well-tested. Adding a new recording path would mean two schemas, two renderers, two sets of bugs. By wrapping RunLog, every step action emits the same events the TUI already knows how to render.

**Alternative considered**: New `StepDB` table with step-level schema (parent step → child events). Rejected because it requires DB migration, TUI changes, and breaks the "no schema changes" goal. The current Section/Group model already provides hierarchical grouping.

### 2. Step as a closure boundary, not a defer pattern

**Decision**: `tc.Step(name, func(s *Step) { ... })` — the step function receives a `*Step` scoped to that block.

**Rationale**: The closure makes the step boundary explicit — when the function returns, the step is done. This enables automatic timing (start when fn is called, stop when it returns) and error boundary behavior (a panic in the step can be caught and recorded). The defer pattern (`s := tc.Step("name"); defer s.Done()`) is more flexible but makes it easy to forget `Done()` and harder to scope the step cleanly.

**Alternative considered**: `s := tc.Step("name"); defer s.Done()`. Simpler syntax, no indentation. Rejected because it doesn't provide a clean scope for the `*Step` — the step variable leaks into the rest of the function, and forgetting `Done()` produces silent bugs.

### 3. CLIResult is a value type with chainable methods that call t.Fatal on failure

**Decision**: `s.CLI(args...)` returns a `*CLIResult`. Each assertion method (`.Contains()`, `.ParseID()`) checks the condition and calls `rl.Failf` if it fails. Methods return `*CLIResult` for chaining.

**Rationale**: This matches the existing convention where `MustRunCLI` calls `t.Fatalf` on error. A failed assertion is a test-stopping event. The chaining is syntactic sugar — `s.CLI(...).Contains("x").ParseID(&id)` is the same as three separate calls, just more readable.

**Alternative considered**: Soft assertions that collect errors and report at step end. Rejected because it would change the test semantics (currently a failed Contains stops the test) and because the "first failure stops" model is standard in Go testing.

### 4. Configurable binary name via TestOpts, not a global

**Decision**: `TestOpts.Binary` sets the CLI binary name. Default is `"memory"` for backward compatibility in this repo. External projects set it to their binary.

**Rationale**: A global (e.g., `runlog.SetBinary("mycli")`) would create ordering dependencies in TestMain and make parallel test packages interact badly. Per-test-context configuration is explicit and safe.

**Alternative considered**: Module-level `init()` or `TestMain` global. Rejected for the ordering and parallelism reasons above.

### 5. HTTP steps use the same auth model as CLI steps

**Decision**: `s.HTTP(method, path, body)` reads auth from `TestContext` (which gets it from env vars via `SetupCLIAuth`). The `TestContext` exposes `.Server` and `.Token` for callers who need raw access.

**Rationale**: Most HTTP steps in the test suite are authenticated API calls to the same server the CLI talks to. Making auth automatic (like CLI steps) eliminates boilerplate.

**Alternative considered**: Require explicit auth on every HTTP call. Rejected as unnecessarily verbose for the 90% case.

### 6. New types live in `framework/` package, not a new sub-package

**Decision**: `TestContext`, `Step`, `CLIResult`, `HTTPResult` are added to the existing `framework/` package (`package e2eframework`).

**Rationale**: The framework package already contains RunLog, RunDB, CLI helpers, and HTTP helpers — exactly the things the new types compose. A separate package would create circular imports or require duplicating types. When runlog is extracted as a standalone module, these types move together.

**Alternative considered**: New `framework/step/` sub-package. Rejected because it would need to import `framework` for RunLog and CLI helpers, creating a one-way dependency that adds complexity without benefit.

## Risks / Trade-offs

**[Risk] API surface grows in framework/**
→ Mitigation: The new types are additive. Existing functions are not removed or changed (except `cli.go` gaining a binary-name parameter). The API surface is documented in the existing `create-e2e-test` skill reference.

**[Risk] Two styles of test in the same repo (old manual style + new step style)**
→ Mitigation: This is intentional. Old tests work unchanged. New tests use the new API. Optional migration can happen incrementally. The TUI sees both styles identically because they emit the same events.

**[Risk] Chainable assertions may feel unfamiliar to Go developers**
→ Mitigation: The chaining is optional. `r := s.CLI(...); r.Contains("x"); r.ParseID(&id)` works identically. The chainable style is sugar, not requirement.

**[Risk] TestContext auto-preamble hides what's happening (magic)**
→ Mitigation: `TestContext` exposes its internals (`tc.Home`, `tc.ProjectID`, `tc.RunLog`, `tc.Server`, `tc.Token`) so the test author can see and use the underlying state. The auto-preamble is convenience, not opacity.

**[Trade-off] Steps use closures, which add indentation**
→ This is the cost of explicit scope boundaries. The indentation is one level per step, and steps are typically 3-5 lines. The trade-off is worth it for automatic timing, error boundaries, and scope isolation.
