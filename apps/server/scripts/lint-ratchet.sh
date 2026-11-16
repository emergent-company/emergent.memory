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

# count_plain_text_error_bodies prints non-test `c.String(http.Status<4xx|5xx>...)`
# call sites in tree. c.String writes a bare text/plain body, which bypasses
# apperror.HTTPErrorHandler just as directly as a raw echo.NewHTTPError but is
# invisible to the counter above — the gap #863 found in the sources webhook
# ingress. Full-line comments are excluded like above, and a success-status use
# (`c.String(http.StatusOK, "OK")` in the health handler) is not an error body,
# so it is not counted.
#
# Deliberate limitation, so it is not mistaken for full coverage: the pattern is
# a single-line `c.String(http.Status<Name>, …)` call. A multi-line call, or a
# status held in a variable (`c.String(code, msg)`), is NOT matched — grep cannot
# classify a computed expression, and a broader `c.String\(` match would need a
# parser to avoid counting success bodies and would fail open on shapes it
# mis-reads. No such site exists today, so the baseline of 0 is honest; a
# reviewer still checks a computed-status c.String by hand. The excluded status
# list below is the 2xx/3xx set an error body would not use; a rarer 1xx/2xx
# code (e.g. 206 PartialContent) would read as a false positive, which is loud
# rather than silent, and is corrected by adding it here.
count_plain_text_error_bodies() {
  grep_matches -rn "c\.String(http\.Status" --include="*.go" --exclude="*_test.go" "$1" \
    | awk '
      /^[^:]+:[0-9]+:[[:space:]]*(\/\/|\/\*|\*[ \/])/ { next }
      {
        if (match($0, /c\.String\(http\.Status[A-Za-z]+/)) {
          st = substr($0, RSTART, RLENGTH)
          sub(/^c\.String\(http\./, "", st)
          if (st ~ /^Status(OK|Created|Accepted|NoContent|MultipleChoices|MovedPermanently|Found|NotModified|UseProxy|TemporaryRedirect|PermanentRedirect)$/) next
          n++
        }
      }
      END { print n + 0 }'
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
    '// c.String(http.StatusBadRequest, "commented out") is banned too.' \
    'c.String(http.StatusNotFound, "unknown app")' \
    'c.String(http.StatusOK, "OK")' \
    'c.String(http.StatusMultipleChoices, "choices")' \
    >"$scratch/assets.go"

  self_fail() {
    echo "self-test FAIL: $1" >&2
    exit 1
  }

  got=$(count_matches "echo\.NewHTTPError" "$scratch/assets.go")
  [ "$got" = 5 ] || self_fail "expected 5 matching lines, got $got"

  got=$(count_raw_echo_httperrors "$scratch")
  [ "$got" = 2 ] || self_fail "expected 2 counted lines (comments excluded), got $got"

  got=$(count_plain_text_error_bodies "$scratch")
  [ "$got" = 1 ] || self_fail "expected 1 plain-text error body (comment and 2xx/3xx excluded), got $got"

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
# Plain-text `c.String(http.Status<4xx|5xx>, ...)` error bodies in non-test
# server code. Zero: every client-visible error body is the apperror envelope.
# #863 converted the seven sources webhook-ingress sites and added this counter
# so the shape cannot come back unseen; the health handler's
# `c.String(http.StatusOK, "OK")` is a success body and is not counted.
# dev-rule-go-apperror-only.
BASELINE_PLAINTEXT_ERRORS=0
# Raw `errors.As(` call sites in tree (tests included), excluding the published
# SDK module. Pinned at 0 (2026-11-16, #853): every site in the server module is
# now `errors.AsType[T]`, which the compiler type-checks and which cannot be
# handed a nil target. The five `pkg/sdk` sites stay on `errors.As`: that module
# declares `go 1.25.0` for its external consumers, and the standard library
# gates AsType at 1.26. A new non-SDK `errors.As` is a regression.
BASELINE_GO_ERRORS_AS=0
auth_guards=$(count_matches -rn "if user == nil" --include="*.go" --exclude="*_test.go" domain/)
setters=$(count_matches -rn "func (s \*Service) Set[A-Z]\|func (h \*Handler) Set[A-Z]" --include="*.go" --exclude="*_test.go" domain/)
apperror_style_a=$(count_matches -rn "\.WithMessage\|\.WithInternal" --include="*.go" --exclude="*_test.go" domain/ pkg/)
response_types=$(count_matches -rn "type APIResponse\|type PaginatedResponse\|type SuccessResponse" --include="*.go" --exclude="*_test.go" domain/)
# Full-line comments are excluded so documenting the rule does not trip it.
echo_http_errors=$(count_raw_echo_httperrors .)
plaintext_errors=$(count_plain_text_error_bodies .)
errors_as=$(count_matches -rn "errors\.As(" --include="*.go" --exclude-dir=sdk .)

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
check "plain-text error body"  "$plaintext_errors" "$BASELINE_PLAINTEXT_ERRORS"
check "errors.As call sites"    "$errors_as"        "$BASELINE_GO_ERRORS_AS"

# --lint-counts pins the *total* golangci-lint finding count per linter, which
# is what #853 pays down and what must not creep back afterwards. The grep
# counters above are cheap enough for a pre-commit hook; a full golangci-lint
# pass is not, so CI runs this mode explicitly (server.yml, "Lint finding-count
# ratchet"). The lint step itself uses --new-from-rev, which only sees findings
# the change introduces — this is what stops the *baseline* from rising again.
# Lower these as the debt is paid; never raise them, a change that needs a
# higher number is the regression.
if [ "${1:-}" = "--lint-counts" ]; then
  BASELINE_LINT_ERRCHECK=0      # 61 after #843, 0 since #853 (test-file sweep)
  BASELINE_LINT_STATICCHECK=99  # untouched by #853; dominated by QF1001 + API naming
  BASELINE_LINT_UNUSED=0        # 16 after #843, 0 since #853 (dead helpers)

  if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "RATCHET ERROR: golangci-lint is not on PATH, cannot count findings" >&2
    exit 2
  fi

  lint_json=$(mktemp)
  trap 'rm -f "$lint_json"' EXIT
  lint_status=0
  golangci-lint run --allow-parallel-runners \
    --output.json.path="$lint_json" --output.text.path=/dev/null ./... >/dev/null 2>&1 \
    || lint_status=$?
  # golangci-lint exits 1 when it *reports* issues — the normal case here.
  # Anything above that is a configuration or tooling failure, and a count from
  # a run that never inspected the tree would read as clean, exactly the trap
  # grep_matches guards above.
  if [ "$lint_status" -gt 1 ]; then
    echo "RATCHET ERROR: golangci-lint exited $lint_status, cannot count findings" >&2
    exit 2
  fi

  lint_count() {
    local out status=0
    out=$(grep -o "\"FromLinter\":\"$1\"" "$lint_json") || status=$?
    if [ "$status" -gt 1 ]; then
      echo "RATCHET ERROR: cannot read golangci-lint output for $1" >&2
      exit 2
    fi
    printf '%s\n' "$out" | awk 'NF { n++ } END { print n + 0 }'
  }

  check "golangci errcheck"    "$(lint_count errcheck)"    "$BASELINE_LINT_ERRCHECK"
  check "golangci staticcheck" "$(lint_count staticcheck)" "$BASELINE_LINT_STATICCHECK"
  check "golangci unused"      "$(lint_count unused)"      "$BASELINE_LINT_UNUSED"
fi

exit "$fail"
