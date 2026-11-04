#!/usr/bin/env bash
# Assert every Go test package under apps/server is referenced by at least one
# `go test` / `go list` invocation in .github/workflows/server.yml.
#
# Guards the #923 class of bug: a package with *_test.go that no CI job ever
# executes. Such a package's tests silently rot (they never run, so they never
# fail) — the same way the DB-backed suites were invisible until #778 and
# tests/integration until #911. #919 deliberately deferred this guard because
# it would have flagged cmd/migrate and cmd/swiftbridge; both are now covered.
#
# The workflow's test jobs name packages as `./<dir>/...` globs on `go test` /
# `go list` command lines. This script reads those globs, expands them with the
# Go toolchain, and reports any test package that falls outside the union.
#
# Cheap + dependency-light: stdlib bash + the Go toolchain already installed on
# CI. No third-party tooling.
#
# Wired into the server.yml `lint` job (which already has Go 1.26 and the module
# cache), so it needs no extra setup. It intentionally does NOT interpolate any
# GitHub Actions expression into a run: payload — see check-injection.py (#834).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

workflow=".github/workflows/server.yml"
server_dir="apps/server"

if [ ! -f "$workflow" ]; then
  echo "ERROR: $workflow not found" >&2
  exit 1
fi

# 1. Package globs from the workflow's `go test` / `go list` invocations.
#    A bare `./...` under `golangci-lint run` type-checks packages but does NOT
#    execute tests, so only `go test`/`go list` lines count as coverage. Comment
#    lines are dropped so prose cannot mint a phantom glob (that would be the
#    unsafe, overly-lenient direction).
globs="$(grep -E '\bgo[[:space:]]+(test|list)[[:space:]]' "$workflow" \
  | grep -v '^[[:space:]]*#' \
  | grep -oE '\./[A-Za-z0-9_./-]*\.\.\.' \
  | sort -u)"

if [ -z "$globs" ]; then
  echo "ERROR: no ./...-style package globs found in $workflow" >&2
  exit 1
fi

covered="$(mktemp)"
all_tests="$(mktemp)"
trap 'rm -f "$covered" "$all_tests"' EXIT

# 2. Expand every glob into concrete import paths via the Go toolchain.
cd "$server_dir"
while IFS= read -r glob; do
  go list "$glob" 2>/dev/null >>"$covered" || true
done <<<"$globs"

# 3. Every package that carries tests (in-package or external).
go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... \
  | grep -v '^[[:space:]]*$' >"$all_tests"

uncovered="$(comm -23 <(sort -u "$all_tests") <(sort -u "$covered"))"

if [ -n "$uncovered" ]; then
  echo "go test coverage check FAILED: these test packages are not referenced by any CI test job:" >&2
  printf '%s\n' "$uncovered" >&2
  echo "Add them to a 'go test' / 'go list' package glob in $workflow." >&2
  exit 1
fi

echo "go test coverage check OK: all $(wc -l <"$all_tests") test packages referenced by a CI job"
