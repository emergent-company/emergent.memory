---
name: runlog-clear
description: Clear the runs database and all per-run log files so fresh test runs produce clean post-fix data. Use when you want to discard stale/pre-fix run history before triggering new test runs.
metadata:
  author: emergent
  version: "1.0"
---

Use `runlog clear` to wipe the run log database and all per-run log files under `logs/`. This is the right step after a round of fixes when the existing DB contains only pre-fix runs (e.g. runs that failed silently with no `failure` events before `rl.Failf` instrumentation was added).

**Input**: None required. Optionally pass `--db <path>` if the DB is not at the default `logs/runs.db`.

---

## Steps

### 1. Ensure the runlog binary is installed

The `runlog` CLI lives in the standalone module `github.com/emergent-company/runlog`.
Install it with:

```bash
go install github.com/emergent-company/runlog/cmd/runlog@latest
```

### 2. Check what exists before clearing

Get a count of current runs so you can confirm the clear worked:

```bash
runlog runs --since 8760h 2>&1 | tail -5
ls logs/ | wc -l
```

### 3. Run the clear

```bash
runlog clear
```

Expected output:
```
removed: logs/runs.db
removed: N subdirectories, M files from logs/
```

Or if the DB was already absent:
```
skipped: logs/runs.db (not found)
nothing else to remove in logs/
```

### 4. Confirm the slate is clean

```bash
runlog runs --since 8760h 2>&1
ls logs/ 2>&1
```

Expected:
- `runs` reports `no runs found in the last 8760h0m0s`
- `logs/` is empty (or the directory itself doesn't exist)

---

## Behaviour notes

- **Safety guard**: sibling-file cleanup only runs when the parent directory of `runs.db` is named `logs` or `test-logs`. If `--db` points to a non-standard path (e.g. `/tmp/foo.db`), only the DB file itself is removed — other files in `/tmp` are left untouched.
- **Non-destructive on missing files**: if `runs.db` doesn't exist, the command prints a `skipped` message and exits cleanly (exit 0).
- **Does not open the DB**: `clear` bypasses `OpenDB` entirely, so it works even when the DB is locked or corrupt.
- **Custom DB path**: `runlog clear --db /path/to/logs/runs.db` — sibling cleanup still applies when the parent dir is named `logs` or `test-logs`.

---

## When to use

- After a batch of `t.Fatalf` → `rl.Failf` fixes, to ensure new runs are distinguishable from pre-fix runs in the TUI and `runlog inspect`.
- After a major refactor that invalidates historical run data.
- Before a demo or benchmark run where a clean baseline is needed.

## When NOT to use

- When you want to keep historical data for comparison (e.g. A/B experiment analysis).
- When a test is currently in-flight — `clear` will delete its partial log data mid-run.

---

## Constitution

All e2e tests in this repository must follow the **E2E Test Constitution**:

→ `.opencode/skills/create-e2e-test/reference/constitution.md`
