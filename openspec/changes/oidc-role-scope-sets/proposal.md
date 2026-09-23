## Why

Issue #736 is the follow-up to #667/#730, which made OIDC scope resolution fail-closed but deliberately left the two write-capable project roles unmapped: only `project_viewer` had an in-code scope set, so `project_admin` and `project_user` membership conferred no Memory scopes at all on the introspection path. An operator deploying a multi-user project therefore had no way to give a directly-authenticated OIDC user write access without hand-tuning token scopes or turning on the userinfo all-grant.

The issue also left a policy gap: `ZITADEL_OIDC_DEFAULT_SCOPES` was applied to an *unmapped role* as well as to a user with no membership, so a legacy `owner` row or a typo silently inherited the operator default instead of failing closed.

This change implements the operator-confirmed decisions recorded on #736.

## What Changes

- **Decision A — role scope sets, conservative write-inclusive.** `project_admin` = viewer read-only set + `data:write` + `agents:write` + `schema:write`; `project_user` = viewer read-only set + `data:write`; `project_viewer` unchanged (read-only). The sets are nested by construction (`project_admin ⊇ project_user ⊇ project_viewer`) and exclude `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, and `account:*`.
- **Umbrella expansion accepted and pinned.** The write scopes are umbrella scopes: `schema:write` implies `schema:migrate`, `agents:write` implies `chat:admin`, and `data:write` implies the related writes plus `journal:write`. This widening is deliberate, so it is pinned by an exact-set-equality test per role (`TestRoleScopeUmbrellaExpansionIsPinned`) rather than left as a silent side effect.
- **Decision B — strict / fail-closed unmapped roles.** A membership row whose role is not one of the three canonical roles (legacy `owner`, a typo) resolves to an empty scope set. `ZITADEL_OIDC_DEFAULT_SCOPES` now applies only to a user with no project membership at all; it is no longer a fallback for unrecognised roles.
- **Exact-set tests.** All scope assertions use exact set equality (sorted comparison, duplicate-rejecting), never `len()` counts, and cover each role's exact set, the nesting invariant (raw and expanded), unmapped ⇒ empty, no-membership ⇒ configured default, lookup error ⇒ empty, and the pinned expansion.

## NOT in scope

Issue item 3 — a live-Zitadel e2e with real credentials in CI — is an infrastructure commitment the operator is deciding separately. It is **not** attempted here; the issue stays open.

## Capabilities

### Modified Capabilities

- `oidc-scope-mapping`: role derivation now maps all three canonical roles to explicit nested scope sets; the configurable default applies only to users with no project membership; unrecognised roles fail closed; the umbrella expansion of role scope sets is bounded and pinned.

## Impact

- `apps/server/pkg/auth/scope_mapping.go`: role scope sets (`roleUserScopes`, `roleAdminScopes`), `roleToScopes` maps all three canonical roles, `resolveOIDCScopes` distinguishes "no membership" from "unrecognised role".
- `apps/server/pkg/auth/scope_mapping_test.go`: exact-set tests for the sets, the nesting invariant, the fail-closed policy, and the pinned expansion.
- No config, schema, or API surface change. No new environment variable.
