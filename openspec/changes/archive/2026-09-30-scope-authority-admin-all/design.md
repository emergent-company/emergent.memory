## Context

`CanGrantAdminAll` (`apps/server/pkg/auth/entitlement.go`) is the single app-side decision check for `admin:all` token minting. It currently authorizes `superadmin_full` OR any-org `org_admin`. Because `admin:all` is an umbrella scope that expands to the platform `admin` / `admin:read` / `admin:write` family, an `org_admin` in any org can mint an account-level `admin:all` token and reach cross-tenant platform surfaces — org-scoped authority buying platform-scoped power.

## Decisions

### D1 — `admin:all` minting is `superadmin_full`-only

`CanGrantAdminAll` narrows to active `superadmin_full` only (`core.superadmins`, `revoked_at IS NULL AND role = 'superadmin_full'`). The `org_admin` arm is removed. This reverses #812 §4.3, which deliberately admitted any-org `org_admin`.

- A `superadmin_readonly` grant does not qualify (unchanged, #810/#812 Q9).
- A bare `project_admin` never qualifies (unchanged).
- An `org_admin` in any org no longer qualifies (reversal).
- No project tier is consulted.

This is a mint-time narrowing only: already-minted `admin:all` tokens are not revoked or migrated by this change.

## Out of scope

- Retro-revocation / migration of existing `admin:all` tokens.
- Auditing residual scope-only platform surfaces (e.g. `/api/traces/*` gated on `RequireScopes("admin:read")`).
