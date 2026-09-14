---
name: runlog-verify-e2e-changes
description: Compile and smoke-test changes to the e2e suite (tests/, framework/). Use after making any code change to verify correctness before committing.
metadata:
  author: emergent
  version: "1.1"
---

After making changes to the e2e test suite, always compile and smoke-test using the runlog CLI. This catches issues that would only surface during a live test run.

**Input**: Optional — specific test name or package to focus on (e.g. `TestAINewsBlueprint_InstallAndRun`). Without input, verify all changed packages.

**Important**: Run this skill after **every individual fix**, not just at the end. Catch regressions early.

---

## Steps

### 1. Build all packages

```bash
cd /root/emergent.memory.e2e
go build ./...
```

If the build fails, fix all errors before proceeding. Do not move to step 2 until the build is clean.

### 2. Vet changed packages

Run `go vet` on every package that was modified:

```bash
go vet ./framework/
go vet ./tests/blueprints/      # note: test-only package, use: go test -run '^$' ./tests/blueprints/
```

For test-only packages (no non-test `.go` files), use:
```bash
go test -run '^$' ./tests/blueprints/   # compile-checks the test package without running any test
```

Fix all vet warnings before proceeding.

### 3. Ensure the runlog binary is installed

The `runlog` TUI/CLI binary lives in the standalone module `github.com/emergent-company/runlog`.
Install it (or update to latest) with:

```bash
go install github.com/emergent-company/runlog/cmd/runlog@latest
```

Verify it's on PATH:

```bash
runlog version
```

### 4. Smoke-test runlog CLI against the live DB

Use the DB at `logs/runs.db` (auto-resolved default). Run all subcommands that were added or changed:

```bash
# List recent runs — basic sanity
runlog runs --since 2h

# Experiments table
runlog experiments

# Tests table (last 7 days to ensure rows appear)
runlog tests --since 168h | head -20

# Tests for a specific known test (use one with recent runs from 'runs' output)
runlog tests --since 168h <TestName>

# Inspect a recent run (pick a FAIL run ID from 'runs' output to verify failure detail)
runlog inspect <run-id>
```

For `inspect`, prefer a **FAIL** run — verify that:
- The `status: FAIL` line is present
- At least one `failure` event appears in the event list with a clear human-readable message
- The failing section is identifiable from the output (the `failure` event is a child of the section that failed)

### 5. Verify failure visibility (the key goal)

If the change involved replacing `t.Fatalf` with `rl.Failf`, confirm the fix works by inspecting a run that previously showed no failure reason:

```bash
runlog inspect <known-failing-run-id> 2>&1 | grep -A2 "failure"
```

Expected output — you should see something like:
```
  [   4.6s]  failure         brave_web_search not found in builtin tools after 10s
```

If the run predates the fix (no `failure` event), that's expected — trigger a new run and inspect it once it completes.

### 6. Report

State clearly:
- Build: PASS / FAIL (with error summary if FAIL)
- Vet: PASS / FAIL
- runlog smoke-test: PASS / FAIL
- Failure visibility: confirmed / not yet testable (no new failing runs since fix)

---

## Guardrails

- **Always fix build errors before reporting done** — a broken build is never acceptable
- **Always run `go test -run '^$'` for test-only packages** — `go build` won't catch test package errors
- **Use a FAIL run for inspect smoke-test** — a PASS run won't show whether failure logging works
- **Do not run the full test suite** unless the user explicitly asks — tests are slow (minutes to hours) and require live server credentials

---

## Constitution

All e2e tests in this repository must follow the **E2E Test Constitution**. When verifying changes, confirm they comply:

→ `.opencode/skills/create-e2e-test/reference/constitution.md`
