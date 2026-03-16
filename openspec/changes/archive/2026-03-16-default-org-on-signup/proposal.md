## Why

When a new user signs up via Zitadel OIDC and first accesses the platform, they land in a state with zero organizations and zero projects. This forces them through a manual setup flow before they can do anything useful. We should automatically create a default org and project during first login to eliminate this friction and let users start working immediately.

Additionally, the OIDC token contains `given_name`, `family_name`, and `name` claims from Zitadel, but these are currently discarded at the middleware boundary — the `TokenClaims` struct and `ensureUserProfile` flow never propagate them to the user profile. This means the auto-created org can't use the user's name until we fix this pipeline.

## What Changes

- Propagate `given_name`, `family_name`, and `name` from the Zitadel introspection/userinfo response through `TokenClaims` and into `UserProfileInfo`, so `EnsureProfile` populates `first_name`, `last_name`, and `display_name` on the `core.user_profiles` row during first login
- After creating the user profile (when it's a new user, not an existing one), automatically create a default organization named `"<FirstName> <LastName>'s Org"` (falling back to email-based name if names aren't available)
- Automatically create a default project named `"<FirstName>'s Project"` within the new org (with same fallback chain as org: DisplayName → email-local-part → "My Project")
- Grant the user `org_admin` and `project_admin` roles on the auto-created org and project respectively
- Eagerly provision the `graph-query-agent` for the new project (same as manual project creation)

## Capabilities

### New Capabilities
- `auto-provision-on-signup`: Automatically create a default org and project when a new user profile is created during first login, using name from OIDC claims

### Modified Capabilities

## Impact

- **Auth middleware** (`apps/server/pkg/auth/`): `TokenClaims` struct gains name fields; `ensureUserProfile` passes names to `UserProfileInfo`; `IntrospectionResult` gains `GivenName`/`FamilyName` fields
- **User profile service** (`apps/server/pkg/auth/user_profile.go`): `EnsureProfile` must detect new-user creation and trigger org+project provisioning
- **Orgs domain** (`apps/server/domain/orgs/`): Service method needs to be callable internally (not just from HTTP handler) for auto-creation
- **Projects domain** (`apps/server/domain/projects/`): Same — internal creation path needed
- **`GetAccessTree`** (`apps/server/domain/useraccess/`): No changes needed — it will automatically pick up the new memberships
- **No frontend changes needed** — the frontend already calls `GET /user/orgs-and-projects` and will see the auto-created org+project
- **No breaking changes** — existing users with orgs are unaffected; this only triggers for brand-new profiles
