## Why

Platform-wide authority is currently expressed as scopes that org-level roles can satisfy, so an org-scoped admin can obtain platform-admin capabilities. `CanGrantAdminAll` (`apps/server/pkg/auth/entitlement.go`) authorizes `admin:all` token minting for `superadmin_full` OR any-org `org_admin`. Because `ScopeImplies["admin:all"]` expands to the platform `admin` / `admin:read` / `admin:write` family, an `org_admin` in any org can mint an account-level `admin:all` token and thereby reach cross-tenant platform surfaces (e.g. `/api/traces/*`) and platform admin tooling.

This reverses the earlier decision recorded in #812 §4.3 (documented in `openspec/changes/unify-scope-authority/design.md` D4 and `tasks.md` §4.3), which deliberately admitted any-org `org_admin`. Org-scoped authority must not buy platform-scoped power.

## What Changes

- `apps/server/pkg/auth/entitlement.go` — `CanGrantAdminAll` narrows to active `superadmin_full` only (`core.superadmins`, `revoked_at IS NULL AND role = 'superadmin_full'`). The `org_admin` arm is removed.
- `apps/server/domain/apitoken/service.go` — the `admin:all` denial message now states `superadmin_full` (no longer "org admin or superadmin").
- `apps/server/domain/apitoken/repository_test.go` — the `org_admin can mint admin:all` case flips to assert denial.
- Spec correction: `openspec/specs/scope-authority/spec.md` "Organization-scoped entitlement decisions" requirement and its "Organization administrator is authorized to mint an admin:all token" scenario, plus the recorded design decision in `openspec/changes/unify-scope-authority/design.md` D4 and `tasks.md` §4.3.

## Capabilities

### Modified Capabilities

- `scope-authority`: `admin:all` token minting is `superadmin_full`-only; an `org_admin` membership no longer authorizes it.

## Impact

- **`apps/server/pkg/auth/entitlement.go`** — the single chokepoint; the `org_admin` OR-arm is removed.
- **`apps/server/domain/apitoken/service.go`** — denial message text only.
- **`apps/server/domain/apitoken/repository_test.go`** — one test case flips to denial.
- **Specs** — `openspec/specs/scope-authority/spec.md` (on archive); `openspec/changes/unify-scope-authority/{design,tasks}.md` corrected in place.

## Out of Scope

- No retro-revocation or migration of already-minted `admin:all` tokens — this is a mint-time fix only.
- No audit/hunt of residual scope-only platform surfaces (e.g. `/api/traces/*` gated on `RequireScopes("admin:read")`); any noticed are observations only.
