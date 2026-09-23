## Context

`pkg/auth/scope_mapping.go` is the extension point: `roleToScopes` maps a `kb.project_memberships` role to the Memory scopes granted for the `X-Project-ID` project, and `resolveOIDCScopes` is the single resolution point (explicit token scopes → mapped role → configured default → empty). Before this change only `project_viewer` was mapped; `project_admin` and `project_user` fell through to `ZITADEL_OIDC_DEFAULT_SCOPES`.

Two operator decisions on #736 bound the design: conservative write-inclusive sets (A) and strict fail-closed unmapped roles (B).

## Goals / Non-Goals

- **Goal:** give `project_admin` and `project_user` real, minimal, nested scope sets; make unmapped roles fail closed rather than inherit the operator default; keep the widening explicit and tested.
- **Non-Goal:** a live-Zitadel e2e (issue item 3, decided separately). Non-Goal: any change to API-token scopes or to `GetAllScopes()`.

## Decisions

### Role sets are constructed nested, not written out three times

The three sets are built with `withScopes(viewerReadOnlyScopes, ...)` so the `admin ⊇ user ⊇ viewer` invariant holds by construction and is also asserted by a test (both raw and after expansion). Each getter returns a copy, so no caller can mutate package state. The names are exported constants (`RoleProjectViewer/User/Admin`) already used by migration 00165 and the projects domain.

### The umbrella expansion is a deliberate, pinned widening — not a hidden one

Role write scopes are umbrella scopes. `expandScopes` is non-recursive, so the effective grant per role is:

- viewer → the read-only set plus the implied `*:read`, `search`, `journal:read`, `chat:use`, `skills:read` (17 scopes)
- user → viewer plus `data:write` and the write scopes it directly implies, including `schema:write` and `journal:write` (29 scopes); note `schema:write` here does **not** additionally pull in `schema:migrate`, because expansion is single-pass
- admin → user plus `agents:write`, `chat:admin`, `skills:write`, `schema:migrate` (33 scopes)

Because this is a widening of what a project role can do, it is pinned by `TestRoleScopeUmbrellaExpansionIsPinned`, which asserts the exact expanded set per role. Any future edit to `scopeImplies` that widens a role must update that test and be re-reviewed. The same test asserts no expansion reaches `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, or `account:*`.

**Blocker check (performed):** the accepted expansion reaches only the scopes the decision covers. `data:write` → the related writes + `journal:write` + `schema:write`; `agents:write` → `chat:admin` + `skills:write`; `schema:write` → `schema:migrate`. No path reaches an excluded `admin*`/`org:*`/`account:*` scope, so there is no blocker to report.

### "No membership" and "unrecognised role" are different, and the default only covers the former

`dbProjectRole` returns `""` when no membership row exists, and the stored role string otherwise. `resolveOIDCScopes` now only consults `defaultOIDCScopes()` when the lookup yields `""`. A non-empty but unrecognised role logs a warning and returns `nil`. This preserves the shipped "default is the account-level grant for non-members" semantics while removing the silent unmapped-role inheritance.

### Fail-closed ordering is unchanged

Explicit token scopes still win verbatim; a lookup error still returns `nil` before any default is considered. The change only narrows the default branch.

## Risks / Trade-offs

- **Widening risk:** an admin now has `schema:write` (→ `schema:migrate`) and `chat:admin`. Accepted by decision A and bounded by the exclusion list; the pinned test makes it explicit.
- **Non-recursive expansion asymmetry:** `project_user` gains `schema:write` (via `data:write`) but not `schema:migrate` (only the explicit `schema:write` input expands to it). This is the current `expandScopes` contract, left unchanged; the pinned test documents the exact behaviour rather than changing the engine in this PR.
- **Behaviour change for existing operators:** a deployment relying on `ZITADEL_OIDC_DEFAULT_SCOPES` to cover unmapped roles will see those roles drop to zero scopes. This is the intended fail-closed policy (decision B) and affects only non-canonical role strings.

## Migration Plan

None. No schema or config change. The role sets are static constants; resolution is per request and never cached, so the change takes effect immediately on deploy.
