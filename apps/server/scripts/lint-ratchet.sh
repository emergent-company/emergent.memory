#!/usr/bin/env bash
# Ratchet gate for architectural debt patterns.
#
# Fails CI/pre-commit when any of the tracked duplication patterns grows past
# its recorded baseline. Lower the BASELINE_* values as you pay down the debt —
# never raise them.
#
# Run from apps/server/ (the script resolves its own directory).

set -euo pipefail

cd "$(dirname "$0")/.." # resolve to apps/server/

# ── Counting ─────────────────────────────────────────────────────────────────
# A counter must be able to reach zero — that is the point of paying debt down —
# but `grep` exits 1 when it matches nothing and 2+ when it could not read its
# input at all (a missing path, an unreadable file, a bad pattern). A `|| true`
# treats both as zero, so the gate reads a tree it never inspected as clean.
# grep_matches separates them: exit 1 prints nothing (a real zero), exit 2+
# aborts the gate with a non-zero status.
grep_matches() {
  local out status=0
  out=$(grep "$@" 2>/dev/null) || status=$?
  if [ "$status" -gt 1 ]; then
    echo "RATCHET ERROR: grep exited $status, cannot inspect the tree: grep $*" >&2
    exit 2
  fi
  [ -z "$out" ] || printf '%s\n' "$out"
}

# count_matches prints the number of lines `grep "$@"` matches, including zero.
count_matches() {
  grep_matches "$@" | wc -l | tr -d ' '
}

# count_raw_echo_httperrors prints non-test `echo.NewHTTPError` occurrences in
# tree. Full-line comments are excluded so documenting the rule does not trip
# it: `//`, the opening `/*`, and a block comment's continuation, which gofmt
# writes as `* text` or `*/`. The continuation needs the space (or the closing
# slash) — a code line can legitimately start with a dereference such as
# `*ptr = echo.NewHTTPError(...)`, and dropping every leading `*` would hide it.
# The filter is a single awk pass over the matches, so a failing grep still
# aborts through grep_matches instead of being hidden by the pipeline.
count_raw_echo_httperrors() {
  grep_matches -rn "echo\.NewHTTPError" --include="*.go" --exclude="*_test.go" "$1" \
    | awk '!/^[^:]+:[0-9]+:[[:space:]]*(\/\/|\/\*|\*[ \/])/ { n++ } END { print n + 0 }'
}

# --self-test exercises the counters' three outcomes on a scratch tree: a match
# counts, no match is zero, and an unreadable path aborts instead of reading as a
# clean zero. It needs no checkout, so CI can run it before the real gate.
if [ "${1:-}" = "--self-test" ]; then
  scratch=$(mktemp -d)
  trap 'rm -rf "$scratch"' EXIT
  printf '%s\n' \
    'package scratch' \
    '' \
    '// echo.NewHTTPError is banned here.' \
    '/* echo.NewHTTPError in a block comment */' \
    '/*' \
    ' * echo.NewHTTPError in a block continuation' \
    ' */' \
    'var first = echo.NewHTTPError(500)' \
    '*ptr = echo.NewHTTPError(502)' \
    >"$scratch/assets.go"

  self_fail() {
    echo "self-test FAIL: $1" >&2
    exit 1
  }

  got=$(count_matches "echo\.NewHTTPError" "$scratch/assets.go")
  [ "$got" = 5 ] || self_fail "expected 5 matching lines, got $got"

  got=$(count_raw_echo_httperrors "$scratch")
  [ "$got" = 2 ] || self_fail "expected 2 counted lines (comments excluded), got $got"

  got=$(count_matches "no\.such\.pattern" "$scratch/assets.go")
  [ "$got" = 0 ] || self_fail "expected 0 matches, got $got"

  status=0
  count_matches x "$scratch/missing.go" >/dev/null 2>&1 || status=$?
  [ "$status" = 2 ] || self_fail "expected exit 2 for an unreadable path, got $status"

  echo "ratchet self-test ok"
  exit 0
fi

# ── Baselines (auth/apperror raised 2026-09-08: PR #384 adds 3 auth-guarded
#    avatar endpoints using the codebase-standard inline auth pattern) ────────
BASELINE_AUTH_GUARDS=13       # inline `if user == nil` guards in domain handlers
BASELINE_SETTERS=3            # cross-domain `func (s *Service) SetXxx` wiring (remaining 3 are false positives: chat.SetAgentDefinitionID DB method + 2 agents HTTP handlers)
BASELINE_APPERROR_STYLEA=1225 # `apperror.Err*.WithMessage/WithInternal` chaining (raised 2026-09-08 to main post-#384; raised 2026-09-11 to 1201 for #423 pack-claims + type-version-history endpoints; raised 2026-10-12 to 1203 for remove-builtin-schema-provisioning: system-pack read-time resolution + source_blueprint_id stamp on existing assignments use the schemas-domain standard repo error style; raised 2026-10-13 to 1206 for blueprint-install-model-mapping: UpsertModelOverrides/ListModelOverrides/DeleteModelOverrides use the blueprints-domain standard repo error style; re-derived 2026-10-15 to 1208 = post-change live count 1203 + 5 headroom for issue-batch-server-share: the baseline had been pinned at the live count (1206 = live 1206), so any legitimate new call failed the guard and trained contributors to bump it reflexively (#1886); raised 2026-11-04 to 1215 for add-schema-discovery-controls: the retry endpoint and the discovery repository's scope/overlap/setting queries (ActiveJobForProject, ProjectDocumentIDs, CountProjectDocuments, AutoDiscoveryEnabled, CancelStalledJob) add 7 calls in the established discoveryjobs repo/handler error style, which consumed the 5-call headroom left by the 2026-10-15 derivation — live 1215, no headroom granted back here; raised 2026-11-04 to 1216 for the same change's manual-finalize fence: FinalizeDiscovery refuses to complete a job a cancel already ended (the pack it built would install as the run's result), which is one conflict message reused by both the pre-check and the fenced write — live 1216, still no headroom; raised 2026-11-15 to 1220 for market-install-foundation (two repository error wraps); raised 2026-11-15 to 1223 for usage-agent-sessions: the per-agent breakdown and the run log add three repository reads that wrap their scan errors in this package's standard style (GetProjectUsageByAgent, GetProjectUsageRuns, CountProjectUsageRuns) — live 1223, no headroom granted back; raised 2026-11-13 to 1218 for add-usage-budget-alerts: ResetBudget binds its request in the projects handler convention, and the two new repo writes (projects.ResetBudgetWindowAnchor, provider.GetProjectWindowSpend) wrap Exec/RowsAffected/scan errors in their package's standard style — the 2 calls of headroom left by 2026-11-04 were consumed, live 1218, no headroom granted back; raised 2026-11-15 to 1225 for run-pack-definition-guard (MCP schema-create path, #829): ValidatePackDefinitions moves CreatePack's existing 400 wrap into the shared helper the MCP tool now calls (net zero), and the MCP path adds two wraps of its own — the advertised-but-unenforced required object_type_schemas check, and claimDefinitionEntry rejecting a non-object definition instead of silently skipping it — both in this domain's standard handler error style; live 1225, no headroom granted back)
BASELINE_RESPONSE_TYPES=6     # non-httputil APIResponse/PaginatedResponse/SuccessResponse defs
# Raw `echo.NewHTTPError` in non-test server code. Pinned (2026-11-16, #843) to
# the sites that are legitimately outside the apperror envelope: the converter
# itself (pkg/apperror/error.go), pre-router auth (pkg/auth/middleware.go) and
# test scaffolding (internal/testutil). A domain handler that reaches for echo
# directly should use apperror.New so the client gets a code and a sanitised
# message — dev-rule-go-apperror-only.
BASELINE_ECHO_HTTPERROR=6

auth_guards=$(count_matches -rn "if user == nil" --include="*.go" --exclude="*_test.go" domain/)
setters=$(count_matches -rn "func (s \*Service) Set[A-Z]\|func (h \*Handler) Set[A-Z]" --include="*.go" --exclude="*_test.go" domain/)
apperror_style_a=$(count_matches -rn "\.WithMessage\|\.WithInternal" --include="*.go" --exclude="*_test.go" domain/ pkg/)
response_types=$(count_matches -rn "type APIResponse\|type PaginatedResponse\|type SuccessResponse" --include="*.go" --exclude="*_test.go" domain/)
# Full-line comments are excluded so documenting the rule does not trip it.
echo_http_errors=$(count_raw_echo_httperrors .)

fail=0

check() {
  local name="$1" actual="$2" baseline="$3"
  if [ "$actual" -gt "$baseline" ]; then
    echo "RATCHET FAIL: $name grew from $baseline to $actual"
    fail=1
  else
    echo "ratchet ok: $name = $actual (baseline $baseline)"
  fi
}

check "auth guards"            "$auth_guards"      "$BASELINE_AUTH_GUARDS"
check "cross-domain setters"   "$setters"          "$BASELINE_SETTERS"
check "apperror Style A"       "$apperror_style_a" "$BASELINE_APPERROR_STYLEA"
check "response type dupes"    "$response_types"   "$BASELINE_RESPONSE_TYPES"
check "raw echo.NewHTTPError"  "$echo_http_errors" "$BASELINE_ECHO_HTTPERROR"

exit "$fail"
