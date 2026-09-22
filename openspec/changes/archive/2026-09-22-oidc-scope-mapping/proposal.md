## Why

`validateToken` (`apps/server/pkg/auth/middleware.go:541`) resolves an OIDC user's Memory scopes two different ways, and neither reflects the user's real project membership:

- The **introspection** path (RFC 7662, used when `ZITADEL_CLIENT_JWT` is configured) copies Zitadel's OAuth `scope` string verbatim. A normal OIDC login requests `openid profile email offline_access`; none of those are Memory scopes, so `tools/list` filters out almost every MCP tool and `tools/call` rejects them.
- The **userinfo** fallback grants `GetAllScopes()` unconditionally to any authenticated user of the issuer — every OIDC user gets every scope, forever.

Operators are therefore forced to choose between "OIDC users cannot use the product" (introspection) and "every OIDC user gets every scope" (userinfo). Neither is acceptable for a multi-user deployment.

This change makes introspection viable as the default by (a) deriving scopes from the membership role for the project a request is scoped to, (b) providing a configurable default scope set for OIDC users who have no explicit Memory grant, and (c) failing closed everywhere it cannot prove a grant. It also fixes the project-membership role-string inconsistency found in the same investigation.

## What Changes

- **Role-derived scopes (authoritative subset):** a `project_viewer` membership restricts an OIDC session to the read-only scope set already defined for viewers (`data:read`, `schema:read`, `agents:read`, `projects:read`).
- **Configurable default scope set:** new `ZITADEL_OIDC_DEFAULT_SCOPES` env var (comma-separated, default empty). Applied to OIDC users whose token carries no explicit Memory scope and whose project role has no defined mapping.
- **Configurable userinfo all-or-nothing grant:** new `ZITADEL_USERINFO_GRANT_ALL_SCOPES` env var (default `true`). The all-grant is honoured **only when introspection is not configured**, so enabling introspection (`ZITADEL_CLIENT_JWT`) automatically disables it and a single-user pilot is not broken.
- **Fail-closed resolution:** explicit Memory scopes in the token are used verbatim; otherwise the mapped role is used; otherwise the configured default set; otherwise nothing. A role-resolution error or unknown issuer yields no scopes. Out of the box (empty default) an OIDC user gets no more scopes than today's introspection path.
- **Derived scopes are never cached** — only raw OIDC claims are cached, so role changes take effect on the next request.
- **Role-string consistency:** `standalone/bootstrap.go` no longer writes `role='owner'` into `kb.project_memberships`; it writes the canonical `project_admin`. A data migration normalises existing rows. `owner` remains valid for `kb.organization_memberships`.

## Capabilities

### New Capabilities

- `oidc-scope-mapping`: resolving an OIDC session's Memory scopes from project membership plus a configurable default set, with fail-closed semantics.

### Modified Capabilities

- `project-viewer-role`: the viewer read-only scope set now also applies to OIDC sessions whose request is scoped (via `X-Project-ID`) to a project where the user holds `project_viewer`; and project membership role strings are constrained to the canonical set.

## Impact

- **`apps/server/pkg/auth/`**: new `scope_mapping.go`; `middleware.go` resolution wiring; `authenticate` passes the request's project header into `validateToken`.
- **`apps/server/internal/config/config.go`**: two new `ZitadelConfig` fields.
- **`apps/server/domain/standalone/bootstrap.go`**: canonical project role.
- **`apps/server/migrations/`**: one data migration normalising legacy `owner` project roles.
- **No breaking changes when `ZITADEL_OIDC_DEFAULT_SCOPES` is unset and `ZITADEL_USERINFO_GRANT_ALL_SCOPES` keeps its default:** the pilot's working path is preserved, and the introspection path never widens.

## Out of Scope (reported, not implemented)

The exact scope set for `project_admin` and `project_user` OIDC sessions is a product/security policy decision. Until it is made, those roles receive no role-derived scopes and fall through to `ZITADEL_OIDC_DEFAULT_SCOPES` (empty by default). See `design.md` for the options and recommendation.
