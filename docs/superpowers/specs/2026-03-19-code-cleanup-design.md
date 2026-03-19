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

1. For each file, grep the codebase to confirm no other `.go` file imports its package or references its exported symbols.
2. Delete all confirmed-unused files.
3. Commit as a single `chore` commit: `chore: remove stray development scripts from repo root`

### Success Criteria

- No stray `patch_*.go`, `fix_*.go`, `old_*.go`, `new_*.go`, or `test_*.go` files remain at the repo root.
- `go build ./...` passes after deletion.
- No test failures introduced.

---

## Section 2 — Remove Committed Coverage Artifacts

### Problem

Generated coverage output files (e.g., `adk_cov.out`, `agents_coverage.out`, and similar `.out`/`.coverprofile` files) are tracked by git. These are build artifacts and should not be in version control.

### Plan

1. Find all coverage artifact files (`*.out`, `*.coverprofile`) committed to the repo.
2. Delete them.
3. Add patterns to `.gitignore` to prevent re-committing:
   ```
   *.out
   *.coverprofile
   coverage/
   ```
4. Commit: `chore: remove coverage artifacts and update .gitignore`

### Success Criteria

- No `.out` or `.coverprofile` files tracked by git.
- `.gitignore` updated to prevent recurrence.
- `git status` is clean after running tests.

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
- Sets high thresholds for `cyclop` (cyclomatic complexity ≤ 30) and `funlen` (function length ≤ 200 lines) to avoid immediately flagging the existing large files while still catching egregious new additions
- Excludes generated files and migration SQL files from linting
- Documents the intent inline with comments

Commit: `chore: add golangci-lint baseline configuration`

### Success Criteria

- `.golangci.yml` exists at `apps/server/.golangci.yml`
- `task lint` passes with zero new failures on the existing codebase
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
- `task test` must pass after stray file deletion
- `task lint` must pass (zero failures) after adding `.golangci.yml`
