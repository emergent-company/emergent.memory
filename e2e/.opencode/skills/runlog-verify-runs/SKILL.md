---
name: runlog-verify-runs
description: Verify test run quality by inspecting run logs for empty sections, missing CLI steps, and logging gaps. Use when the user wants to review recent test runs or audit logging quality.
metadata:
  author: emergent
  version: "1.0"
---

Inspect recent test runs via the `runlog` CLI and identify logging quality issues: empty sections, missing CLI steps, direct HTTP calls that should use CLI, and runs without structured logging.

**Input**: None required. Optionally the user may specify a run ID, test name, or time window.

---

## Steps

### 1. Ensure the runlog CLI is installed

The `runlog` CLI lives in the standalone module `github.com/emergent-company/runlog`.
Install it with:

```bash
go install github.com/emergent-company/runlog/cmd/runlog@latest
```

### 2. List recent runs

```bash
runlog runs --since 24h
```

If no runs exist, tell the user and suggest running tests first (`./test mcj-emergent`).

### 3. Inspect each run for quality issues

For every run in the listing (or the subset the user specified), run:

```bash
runlog inspect <run-id>
```

For each run, evaluate these quality criteria:

#### a. No empty sections

Every `section` event must have at least one child event. A section with 0 children means something was opened but nothing was logged inside it. Flag these.

#### b. CLI steps used everywhere possible

The principle: **if an operation can be done through the `memory` CLI, it should use `rl.CLI()` / `rl.CLIStep()` to log it as a `cli` event.**

Operations that SHOULD be CLI steps:
- `memory projects create` / `memory projects list` / `memory projects delete`
- `memory blueprints apply` / `memory blueprints list`
- `memory set-token`
- `memory install-memory-skills`
- `memory agents create` / `memory agents trigger` / `memory agents runs`
- `memory agents questions list-project` / `memory agents questions respond`
- `memory graph list` / `memory graph relationships`
- `memory tokens create`
- `memory status`
- `memory version`
- `memory ask` / `memory ask-project`
- Any other `memory` subcommand

Operations that legitimately use direct HTTP (no CLI equivalent):
- SSE streaming from `/api/ask` or `/api/projects/:id/ask` (CLI `memory ask` exists but the test may need raw SSE access)
- MCP JSON-RPC calls to tool servers
- Admin endpoints (`/api/admin/*`) that have no CLI equivalent
- Health checks (`/health`)
- Auth issuer endpoint (`/api/auth/issuer`)

#### c. Every test has a RunLog

Tests should create a RunLog via `newRunLog(t)`. Tests that only use `t.Logf` produce no structured logging and are invisible to the run log system.

#### d. Sections have meaningful names

Section names should describe what the section does (e.g., "Create project", "Install blueprint"), not just "Step 1".

#### e. No orphaned log messages

`rl.Printf()` calls outside of any section end up as top-level events. These should generally be inside a section.

### 4. Produce a findings report

Organize findings by test name. For each test with issues, list:

```
## <TestName> (run <id>)

- [ ] **Empty section**: "<section name>" at seq <N> has 0 children
- [ ] **Missing CLI step**: <description of HTTP call that should use CLI>
- [ ] **No RunLog**: test does not create structured logging
- [ ] **Orphaned events**: <N> top-level events outside any section
```

### 5. Cross-reference with test source code

For each finding, identify the exact file and line in the test source code where the fix should be made. Use:

- `tests/cli/` for CLI tests
- `tests/tools/` for tool tests
- `tests/blueprints/` for blueprint and orchestrator tests
- `tests/experiments/` for experiment tests
- `tests/production/` for production smoke tests
- `tests/blueprints/helpers_test.go` for shared helper functions

### 6. Fix identified issues

For each issue found:

- **Empty section**: Either add appropriate logging inside the section, or merge it with an adjacent section if the split is unnecessary.
- **Missing CLI step**: Replace the direct HTTP call with `MustRunCLIInDirWithHome` + `rl.CLI()`, or if the HTTP call is in a helper, add `rl.CLIStep()` logging around it.
- **No RunLog**: Add `rl := newRunLog(t)` and `defer rl.Close()` at the top of the test, then add sections and CLI steps.
- **Orphaned events**: Wrap them in an appropriate section.

### 7. Verify fixes

After making changes, run the verify-e2e-changes skill to ensure compilation succeeds:

```bash
cd /root/emergent.memory.e2e
go build ./...
go vet ./...
```

Then run one or two affected tests to verify the logs look correct:

```bash
./test mcj-emergent <TestName>
runlog inspect <new-run-id>
```

---

## Guardrails

- **Always start with `runlog inspect`** -- never guess about run quality; read the actual logged events first.
- **Never remove logging** -- the goal is to add missing logging, not reduce it.
- **Preserve test logic** -- when replacing HTTP calls with CLI calls, ensure the test still validates the same behavior. If the test needs to inspect HTTP response details that the CLI doesn't expose, keep the HTTP call but add an `rl.Event()` or `rl.CLIStep()` to log what happened.
- **CLI steps must include output** -- `rl.CLI(invocation, output)` requires both the command string and its stdout/stderr output. Never pass empty output.
- **Sections must not be empty** -- if you add a section, ensure at least one event (CLI, Printf, Event) is logged inside it before the next section or test end.
- **Do not change test assertions** -- logging improvements should not alter what the test validates.
- **Run `go build ./...` before committing** -- ensure all changes compile.

## Reference: RunLog API Quick Reference

| Method | Event kind | When to use |
|---|---|---|
| `rl.Section(name)` | `section` | Major test phase boundary |
| `rl.CLI(invocation, output)` | `cli` | After running a CLI command successfully |
| `rl.CLIErr(invocation, output, err)` | `cli` | After a CLI command that may fail |
| `rl.CLIStep(desc, invocation, output)` | `cli` | CLI command with a human-readable description |
| `rl.CLIStepErr(desc, invocation, output, err)` | `cli` | CLI command with description + error |
| `rl.Printf(format, args...)` | `log` | Informational message within a section |
| `rl.Event(kind, message, details)` | custom | Structured event (API response, state change, etc.) |
| `rl.Group(kind, title, fn)` | `group` | Collapsible sub-group within a section |
| `rl.Failf(format, args...)` | `failure` | Fatal test failure with logging |
| `rl.Describe(summary, bullets...)` | (metadata) | Test description shown in TUI header |
| `rl.Tag(tags...)` | `tag` | Key:value tags for filtering/grouping |

---

## Constitution

All quality checks in this skill derive from the **E2E Test Constitution**. Read and enforce every rule:

→ `constitution.md` (project root)
