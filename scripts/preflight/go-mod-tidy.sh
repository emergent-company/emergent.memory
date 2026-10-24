#!/usr/bin/env bash
# Committed Go module files must be `go mod tidy`-clean (issue #2193).
#
# apps/server/go.sum was missing 40 module `/go.mod` hash lines and stayed that
# way for months (#2083): a warm module cache hides it, because go.sum
# verification only happens at download time, while the first cold consumer —
# a new workstation, a fresh CI container, `docker build` — fails outright with
#
#     github.com/labstack/echo/v4@v4.15.2: missing go.sum entry for go.mod file
#
# `go mod download all` and `go list -deps ./...` both pass on a tree with that
# hash removed, so a build-shaped check cannot see it; `go mod tidy -diff` can.
# #2057 had already hidden one instance behind a second `go mod download` inside
# apps/server/Dockerfile (removed in #2190) instead of noticing that the
# committed file was incomplete.
#
# `go mod tidy -diff` (Go >= 1.23) prints exactly what a tidy would change
# without writing it, and exits 1 when there is anything to change — go.mod and
# go.sum both. It is the same invariant the generator-side guard enforces for
# Swagger artifacts (scripts/preflight/swagger-drift.sh, #2082).
#
# Cost: ~0.2 s per module on a warm module cache, which is what the
# branch-protection-required `ci` job has by the time this runs (the earlier
# `go run` guards in that job already resolved the server module). The first run
# on a cold runner downloads each module graph once — measured 25 s for
# apps/connector.linux and 71 s for apps/server — a one-off per runner, and the
# reason this lives in the required `ci` job rather than the path-filtered,
# advisory server.yml: only `ci` can block a merge.
#
# Run from anywhere; resolves the repo root itself. Deliberately NOT listed in
# scripts/preflight/all.sh, which the same `ci` job already runs — one step, one
# run per PR.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

# Modules that must stay tidy: every module in the repo that is clean today, so
# this is a ratchet — a module can be added, never dropped. Excluded, each
# failing tidy for a reason that is not an incomplete go.sum file:
#   * apps/cli — its relative `replace ... => ../../apps/server/pkg/sdk` makes
#     tidy re-classify golang.org/x/net as indirect and drop the SDK's go.sum
#     lines. One tidy commit fixes it; until then it stays out (#2193).
#   * blueprints/code-memory-blueprint/tools/* — all 19 rewrite the `go`
#     directive from 1.24 to 1.25.0, because the SDK they replace requires the
#     newer one. A stale directive, not a hash gap, and not fixed by add-and-go.
# e2e and its four stubs are outside go.work but tidy-clean, so they are in.
modules=(
  apps/server
  apps/server/pkg/sdk
  apps/web-ui/gateway
  apps/connector.linux
  e2e
  e2e/stubs/github
  e2e/stubs/mailgun
  e2e/stubs/mcp-registry
  e2e/stubs/web
)

diff_log="$(mktemp)"
trap 'rm -f "$diff_log"' EXIT

failed=()   # tidy drift: go.mod/go.sum do not match the graph
broken=()   # the command itself failed — network, missing toolchain, bad flag

for module in "${modules[@]}"; do
  if [ ! -f "$module/go.mod" ]; then
    echo "go module tidy guard: no go.mod at ${module} — update the module list in $0" >&2
    exit 1
  fi

  status=0
  (cd "$module" && go mod tidy -diff) >"$diff_log" 2>&1 || status=$?

  if [ "$status" -eq 0 ]; then
    continue
  fi

  if [ "$status" -ne 1 ]; then
    # Fail closed, but do not mislabel an infrastructure failure as drift.
    broken+=("$module")
    echo "== ${module}: 'go mod tidy -diff' errored (exit ${status}) — this is not a tidy diff" >&2
    head -n 40 "$diff_log" >&2 || true
    echo "" >&2
    continue
  fi

  failed+=("$module")
  echo "== ${module}: go.mod/go.sum are not tidy" >&2
  # A full graph diff can run to hundreds of lines; the fix is the same either
  # way, so bound it.
  head -n 80 "$diff_log" >&2 || true
  if [ "$(wc -l <"$diff_log")" -gt 80 ]; then
    echo "... (diff truncated at 80 lines)" >&2
  fi
  echo "" >&2
done

if [ "${#failed[@]}" -eq 0 ] && [ "${#broken[@]}" -eq 0 ]; then
  echo "go module files tidy (${#modules[@]} modules: ${modules[*]})"
  exit 0
fi

if [ "${#failed[@]}" -ne 0 ]; then
  echo "Committed Go module files do not match the module graph in: ${failed[*]}" >&2
  echo "" >&2
  echo "Fix each one by writing the tidy result and committing it:" >&2
  echo "" >&2
  for module in "${failed[@]}"; do
    echo "    (cd ${module} && go mod tidy)" >&2
  done
  echo "" >&2
  echo "Do not run tidy on a warm cache only and assume it is a no-op: a missing" >&2
  echo "/go.mod hash is invisible until something downloads that module, which is" >&2
  echo "why this guard is here (#2083, #2193)." >&2
fi

if [ "${#broken[@]}" -ne 0 ]; then
  echo "The guard could not evaluate: ${broken[*]}" >&2
  echo "Check the Go toolchain and network for those modules; the messages above" >&2
  echo "are from the failing command, not from a tidy diff." >&2
fi

exit 1
