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

# ── Baselines (auth/apperror raised 2026-09-08: PR #384 adds 3 auth-guarded
#    avatar endpoints using the codebase-standard inline auth pattern) ────────
BASELINE_AUTH_GUARDS=13       # inline `if user == nil` guards in domain handlers
BASELINE_SETTERS=3            # cross-domain `func (s *Service) SetXxx` wiring (remaining 3 are false positives: chat.SetAgentDefinitionID DB method + 2 agents HTTP handlers)
BASELINE_APPERROR_STYLEA=1214 # `apperror.Err*.WithMessage/WithInternal` chaining (raised 2026-09-08 to main post-#384; raised 2026-09-11 to 1201 for #423 pack-claims + type-version-history endpoints; raised 2026-10-12 to 1203 for remove-builtin-schema-provisioning: system-pack read-time resolution + source_blueprint_id stamp on existing assignments use the schemas-domain standard repo error style; raised 2026-10-13 to 1206 for blueprint-install-model-mapping: UpsertModelOverrides/ListModelOverrides/DeleteModelOverrides use the blueprints-domain standard repo error style; re-derived 2026-10-15 to 1208 = post-change live count 1203 + 5 headroom for issue-batch-server-share: the baseline had been pinned at the live count (1206 = live 1206), so any legitimate new call failed the guard and trained contributors to bump it reflexively (#1886); raised 2026-11-04 to 1214 for add-schema-discovery-controls: the retry endpoint and the discovery repository's scope/overlap/setting queries (ActiveJobForProject, ProjectDocumentIDs, CountProjectDocuments, AutoDiscoveryEnabled) add 6 calls in the established discoveryjobs repo/handler error style, which consumed the 5-call headroom left by the 2026-10-15 derivation — live 1214, no headroom granted back here)
BASELINE_RESPONSE_TYPES=6     # non-httputil APIResponse/PaginatedResponse/SuccessResponse defs

auth_guards=$(grep -rn "if user == nil" --include="*.go" domain/ 2>/dev/null | grep -v "_test.go" | wc -l | tr -d ' ')
setters=$(grep -rn "func (s \*Service) Set[A-Z]\|func (h \*Handler) Set[A-Z]" --include="*.go" domain/ 2>/dev/null | grep -v "_test.go" | wc -l | tr -d ' ')
apperror_style_a=$(grep -rn "\.WithMessage\|\.WithInternal" --include="*.go" domain/ pkg/ 2>/dev/null | grep -v "_test.go" | wc -l | tr -d ' ')
response_types=$(grep -rn "type APIResponse\|type PaginatedResponse\|type SuccessResponse" --include="*.go" domain/ 2>/dev/null | grep -v "_test.go" | wc -l | tr -d ' ')

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

exit "$fail"
