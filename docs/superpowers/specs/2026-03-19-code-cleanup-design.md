# Code Cleanup — Low-Hanging Fruit

**Date:** 2026-03-19
**Status:** Approved
**Scope:** Pure cleanup with no logic or behavioral changes

---

## Goal

Remove noise and technical debt from the repository without touching any business logic. Three discrete, independently shippable changes that make the codebase immediately cleaner and establish guardrails for future work.

---

## Section 1 — Remove Stray Root-Level `.go` Files

### Problem

Approximately 35 `.go` files exist at or near the repo root with names like `patch.go`, `patch2.go`, `patch_imdb.go`, `fix_agent_id.go`, `old_bulk_insert.go`, `new_bulk_insert.go`, `old_insert.go`, `new_insert.go`, `fix_errors.go`, `fix_imports.go`, etc.

These appear to be one-time migration scripts, debugging utilities, or development experiments that were never cleaned up after use.

### Plan

1. Scope is the **immediate repo root directory only** (`/root/emergent.memory/*.go`). Run `ls *.go` to enumerate candidates. Read the `package` declaration of each file — files declaring `package main` with only a `main()` or `init()` function and no callers are targets; files clearly part of a structured package are not.
2. Target patterns: `patch*.go`, `fix_*.go`, `old_*.go`, `new_*.go`. Do NOT delete `*_test.go` files.
3. Delete all matched files.
4. Run `go build ./...` from `apps/server/` — this is the authoritative verification. The compiler will fail if any deleted file's symbols were referenced elsewhere. If a file causes a build failure, restore it and note it by name in the commit message as "retained: <filename> (build dependency)".
5. Commit: `chore: remove stray development scripts from repo root`

### Success Criteria

- No stray one-off script files (matching the patterns above) remain at the repo root.
- `go build ./...` passes after deletion (verifies no import references missed).
- `task test` passes after deletion.

---

## Section 2 — Remove Committed Coverage Artifacts

### Problem

Generated coverage output files (e.g., `adk_cov.out`, `agents_coverage.out`, and similar `.out`/`.coverprofile` files) are tracked by git. These are build artifacts and should not be in version control.

### Plan

1. Run `git ls-files | grep -E '\.(out|coverprofile|prof|test)$'` to get the full list of all committed build artifacts.
2. Delete all found files.
3. Add patterns to the **root `.gitignore`** (applies repo-wide) to prevent re-committing:
   ```
   *.out
   *.coverprofile
   *.prof
   coverage/
   ```
   (`*.test` is a Go test binary pattern — add it only if binary test files are found tracked by git.)
4. Commit: `chore: remove coverage artifacts and update .gitignore`

### Success Criteria

- No `.out` or `.coverprofile` files tracked by git.
- `.gitignore` updated to prevent recurrence.
- `git status` is clean after running `task test`.

---

## Section 3 — Add `.golangci.yml` Baseline

### Problem

`golangci-lint` runs via `task lint` but there is no explicit configuration file. This means:
- Linting behavior is implicit and can change across tool versions.
- There are no enforced quality gates (complexity, function length, unused code).
- New contributors don't know what the lint standard is.

### Plan

Add `apps/server/.golangci.yml` with a practical baseline that:
- Enables core linters: `govet`, `staticcheck`, `errcheck`, `unused`, `gofmt`
- Sets high thresholds for `cyclop` (cyclomatic complexity ≤ 30) and `funlen` (function length ≤ 200 lines) to avoid immediately flagging existing large files while still catching egregious new additions
- Excludes generated files and migration SQL files from linting
- Documents the intent inline with comments

**Threshold validation (ordered steps):**
1. Write an initial `.golangci.yml` with the linters and suggested thresholds.
2. Run `golangci-lint run ./...` from `apps/server/` to see current failures.
3. Raise thresholds until zero failures. The goal is a clean baseline that matches current state, not a clean codebase — we are not fixing existing violations in this change.
4. Document chosen thresholds with inline comments: e.g. `# funlen: 200 — set high to match existing large files; reduce gradually over time`.

If a linter cannot be tuned to zero failures without effectively disabling it, remove it from this config and note it in the commit message as "deferred: <linter>". The linters that flag `panic()` and TODO comments (`godot`, `revive`) are explicitly excluded from this baseline — those are deferred.

**Module scope:** The config lives at `apps/server/.golangci.yml` and covers only the server Go module (the primary module). The CLI tool at `tools/cli/` is a separate module and is out of scope for this change.

Commit: `chore: add golangci-lint baseline configuration`

### Success Criteria

- `.golangci.yml` exists at `apps/server/.golangci.yml`
- `task lint` (or `golangci-lint run ./...`) passes with zero failures on the existing codebase
- Configuration is commented to explain threshold choices

---

## Sequencing

These three changes are independent and can be done in any order. Recommended sequence:

1. Coverage artifacts (smallest, zero risk)
2. Stray `.go` files (requires grep verification per file)
3. `.golangci.yml` (requires lint pass validation)

---

## Out of Scope

- Replacing `panic()` calls with error returns (deferred to a separate change)
- Resolving TODO/FIXME comments (deferred)
- Refactoring large files like `mcp/service.go` (separate initiative)
- Any logic changes

---

## Testing

- `go build ./...` must pass after each change
- `task test` (unit tests) must pass after stray file deletion
- Integration and E2E tests are out of scope for this cleanup change — these require a running database and are impractical as a local gate for pure deletions
- `task lint` must pass (zero failures) after adding `.golangci.yml`
