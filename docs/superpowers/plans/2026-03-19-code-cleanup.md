# Code Cleanup — Low-Hanging Fruit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove 34 stray root-level `.go` files, add `.gitignore` coverage artifact protection, and add a `.golangci.yml` baseline config — all with zero logic changes.

**Architecture:** Three independent, sequenced tasks. Each produces a clean commit. No business logic is touched. Verification is compiler- and test-driven, not manual inspection.

**Tech Stack:** Go 1.24+, golangci-lint, git

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Delete (×34) | `/root/emergent.memory/*.go` | Stray dev scripts — all `package main` or bare fragments |
| Modify | `/root/emergent.memory/.gitignore` | Add coverage artifact patterns |
| Create | `/root/emergent.memory/apps/server/.golangci.yml` | Linting baseline config |

---

## Task 1: Remove Coverage Artifact Protection Gap

**Files:**
- Modify: `/root/emergent.memory/.gitignore`

- [ ] **Step 1: Check what coverage artifacts are tracked by git**

```bash
cd /root/emergent.memory && git ls-files | grep -E '\.(out|coverprofile|prof)$'
```

Expected: no output (these files should not be committed). If any output appears, those are committed artifacts that must be deleted with `git rm`.

- [ ] **Step 2: Check what coverage files exist on disk (untracked)**

```bash
find /root/emergent.memory/apps/server -name "*.out" -o -name "*.coverprofile" | sort
```

Expected: ~27 files like `enc_cov.out`, `agents_cover.out`, `coverage-e2e.out`, etc. — these are untracked and just need `.gitignore` protection.

- [ ] **Step 3: Add patterns to root `.gitignore`**

Open `/root/emergent.memory/.gitignore` and append at the end:

```
# Go test/coverage output artifacts (generated, never commit)
*.out
*.coverprofile
*.prof
coverage/
```

- [ ] **Step 4: Verify git now ignores the existing coverage files**

```bash
git status --short | grep "\.out"
```

Expected: no output (files are now properly ignored, not showing as untracked)

- [ ] **Step 5: Commit**

```bash
git add .gitignore
git commit -m "chore: ignore Go coverage and test output artifacts"
```

---

## Task 2: Delete Stray Root-Level `.go` Files

**Files:**
- Delete (×34): all `*.go` in `/root/emergent.memory/` root

The 34 files to delete are:

```
clean_syntax.go       fix_agent_id.go       fix_imports.go
fix_sdk_init.go       fix_seed_imdb.go      fix_setup_again.go
fix_sleep.go          fix_suite_setup_for_real.go
new_bulk_insert.go    new_insert.go
old_bulk_insert.go    old_insert.go
patch.go              patch2.go
patch_e2e_stats.go    patch_external_db.go  patch_imdb.go
patch_insert.go       patch_limit.go        patch_limits_wait.go
patch_remove_db.go    patch_remove_db2.go   patch_revert_insert.go
patch_sdk_http.go     patch_sdk_insert.go   patch_suite_setup.go
patch_sync.go         patch_throttle.go
run_manual_seed.go    setup_agent.go
test_db_rel_patch.go  test_queries_no_install.go
test_queries_no_sleep.go  test_queries_only.go
```

- [ ] **Step 1: Confirm the full list of stray files**

```bash
ls /root/emergent.memory/*.go
```

Expected: the 34 files listed above (and nothing else — no legitimate `main.go` or `*_test.go` should appear). If additional files appear beyond the list above, read their first few lines to check the package declaration before deleting.

- [ ] **Step 2: Delete all of them**

```bash
cd /root/emergent.memory && rm *.go
```

- [ ] **Step 3: Verify deletion**

```bash
ls /root/emergent.memory/*.go 2>&1
```

Expected: `ls: cannot access '*.go': No such file or directory`

- [ ] **Step 4: Build the server module to confirm nothing was broken**

```bash
cd /root/emergent.memory/apps/server && go build ./...
```

Expected: exits with code 0 and no output. If a build error appears referencing a deleted file's symbol, restore that specific file with `git restore <filename>` and note it in the commit message.

- [ ] **Step 5: Run unit tests**

```bash
cd /root/emergent.memory/apps/server && go test ./... -count=1 -short 2>&1 | tail -20
```

Expected: all packages pass or skip. No failures.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: remove stray development scripts from repo root"
```

If any file was restored due to a build failure, amend the message:
```
chore: remove stray development scripts from repo root

retained: <filename> (build dependency)
```

---

## Task 3: Add `.golangci.yml` Baseline

**Files:**
- Create: `/root/emergent.memory/apps/server/.golangci.yml`

- [ ] **Step 1: Check if golangci-lint is installed**

```bash
golangci-lint version
```

Expected: version output like `golangci-lint has version X.Y.Z`. If not installed, install via:
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

- [ ] **Step 2: Run linter with no config to measure baseline failures**

```bash
cd /root/emergent.memory/apps/server && golangci-lint run ./... 2>&1 | tail -30
```

Note the number and types of failures. This is the baseline — we need zero failures after adding config.

- [ ] **Step 3: Create `.golangci.yml` with initial settings**

Create `/root/emergent.memory/apps/server/.golangci.yml`:

```yaml
# golangci-lint baseline configuration for emergent.memory server module.
# Thresholds are set high to match the current codebase state.
# Reduce thresholds gradually as large files are refactored.
version: "2"

linters:
  default: none
  enable:
    - govet       # reports suspicious constructs
    - staticcheck # comprehensive static analysis
    - errcheck    # checks for unchecked errors
    - gofmt       # enforces standard formatting
    - unused      # finds unused code

linters-settings:
  cyclop:
    # max-complexity: 30  # set high — existing code has complex functions; reduce over time
    max-complexity: 30
  funlen:
    # lines: 200  # set high — existing large files; reduce over time
    lines: 200
    statements: 100

issues:
  exclude-rules:
    # Ignore generated files
    - path: ".*\\.gen\\.go"
      linters: [errcheck, unused]
    # Ignore migration files
    - path: "migrations/.*"
      linters: [all]
  max-issues-per-linter: 0
  max-same-issues: 0
```

- [ ] **Step 4: Run linter and check for failures**

```bash
cd /root/emergent.memory/apps/server && golangci-lint run ./... 2>&1 | tail -40
```

If there are failures, use this decision tree:

1. **`gofmt` failures** → Run `gofmt -w .` to auto-fix. This is safe and cosmetic only.
2. **`errcheck` / `staticcheck` / `unused` failures on specific packages** → Add targeted `exclude-rules` entries with `# pre-existing, deferred` comments.
3. **Failures across >10 files for a single linter** → The linter is too noisy for this codebase at this time. Remove it from `enable` and note it as `deferred: <linter>` in the commit message.
4. **Failures on <10 files** → Tune via `exclude-rules` or raise a threshold setting.

Goal: `golangci-lint run ./...` exits 0 with no output.

- [ ] **Step 5: Confirm zero failures**

```bash
cd /root/emergent.memory/apps/server && golangci-lint run ./...
echo "Exit code: $?"
```

Expected: no output, `Exit code: 0`

- [ ] **Step 6: Commit**

```bash
git add apps/server/.golangci.yml
git commit -m "chore: add golangci-lint baseline configuration"
```

If any linters were deferred:
```
chore: add golangci-lint baseline configuration

deferred: <linter-name> (cannot tune to zero failures without disabling)
```

---

## Verification Checklist

After all three tasks:

- [ ] `ls /root/emergent.memory/*.go` → no output
- [ ] `git ls-files | grep '\.out'` → no output
- [ ] `cat /root/emergent.memory/.gitignore | grep '\.out'` → shows the pattern
- [ ] `ls /root/emergent.memory/apps/server/.golangci.yml` → file exists
- [ ] `cd apps/server && go build ./...` → exits 0
- [ ] `cd apps/server && golangci-lint run ./...` → exits 0

---

## Notes

- Integration and E2E tests require a live database — they are not part of this cleanup validation
- The CLI module at `tools/cli/` is out of scope for the linting config
- These three tasks are independent — any one can be shipped without the others
