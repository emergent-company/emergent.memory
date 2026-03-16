## Context

When a new user authenticates via Zitadel OIDC, the auth middleware (`apps/server/pkg/auth/middleware.go`) calls `ensureUserProfile` which creates a `core.user_profiles` row via `EnsureProfile`. Currently, this is the only action taken — no org or project is created. The user hits `GET /user/orgs-and-projects` which returns `[]`, and the frontend must prompt them to manually create an org and project before they can do anything.

The Zitadel introspection and userinfo responses contain `given_name`, `family_name`, and `name` OIDC standard claims, but these are discarded at the `TokenClaims` boundary — the struct only carries `Sub`, `Email`, `Scopes`, and `ExpiresAt`.

The org creation path (`orgs/service.go:Create`) and project creation path (`projects/service.go:Create`) are already well-structured with transactional membership creation, validation, and (for projects) eager agent provisioning. The standalone bootstrap (`domain/standalone/bootstrap.go`) demonstrates a similar pattern of auto-creating org+project but uses raw SQL and a different code path.

## Goals / Non-Goals

**Goals:**
- Propagate OIDC name claims (`given_name`, `family_name`, `name`) through the auth pipeline into the user profile
- Automatically create a default org and project when a new user profile is created for the first time
- Make the provisioning invisible to existing users — only triggers on first-ever profile creation
- Reuse existing org/project service methods rather than duplicating creation logic

**Non-Goals:**
- Changing the standalone bootstrap flow — it has its own separate path
- Adding any new HTTP endpoints — this is purely server-side, triggered by the auth middleware
- Allowing users to customize org/project names during signup — they can rename later
- Handling Zitadel service accounts or API token auth — only OIDC user logins
- Syncing name changes from Zitadel on subsequent logins (only first-time population)

## Decisions

### 1. Where to trigger auto-provisioning: inside `EnsureProfile`

**Decision**: Add provisioning logic that fires after a new profile is created within the `EnsureProfile` method (or a method it calls), not in the middleware.

**Rationale**: `EnsureProfile` already knows whether it created a new profile or found an existing one. It returns the profile, and we can detect the "just created" case. Putting the logic here keeps the middleware thin and ensures provisioning happens regardless of which auth path triggered profile creation.

**Alternative considered**: A separate middleware step after `ensureUserProfile` — rejected because it would require tracking "was this a new user?" across the middleware boundary, adding complexity.

### 2. How to construct the org name

**Decision**: Use `"<FirstName> <LastName>'s Org"` when both names are available. Fall back to `"<DisplayName>'s Org"` → `"<email-local-part>'s Org"` → `"My Organization"`.

**Rationale**: Using the user's actual name makes the org feel personal and recognizable. The fallback chain handles cases where OIDC claims are incomplete (some identity providers may not provide names).

### 3. Default project name

**Decision**: Use `"<FirstName>'s Project"` as the default project name, with the same fallback chain as the org name: `"<FirstName>'s Project"` → `"<DisplayName>'s Project"` → `"<email-local-part>'s Project"` → `"My Project"`.

**Rationale**: Consistent with the org naming pattern. Feels personal ("Tom's Project") rather than generic ("My First Project").

### 4. Reuse existing service methods for creation

**Decision**: Call `orgs.Service.Create()` and `projects.Service.Create()` directly rather than writing raw SQL.

**Rationale**: These methods handle transaction management, membership creation, validation, and (for projects) eager agent provisioning. Duplicating this logic would be fragile and miss future enhancements. This requires passing these services as dependencies to the auth package.

### 5. Propagate names via `TokenClaims` and `UserProfileInfo`

**Decision**: Add `GivenName`, `FamilyName`, and `Name` fields to `IntrospectionResult` and `TokenClaims`. Map them through to `UserProfileInfo` in `ensureUserProfile`.

**Rationale**: This is the minimal change to pass names through the existing pipeline. The structs already exist and the data is already parsed from Zitadel — we just need to stop discarding it.

### 6. Only propagate names on first profile creation, not on every login

**Decision**: `EnsureProfile` will set `first_name`, `last_name`, and `display_name` only when creating a new profile. Existing profiles will not have their names overwritten by OIDC claims on subsequent logins.

**Rationale**: Users can manually update their profile names via `PUT /api/user/profile`. Overwriting on every login would undo those changes. The OIDC claims serve as initial seed data only.

### 7. Dependency injection approach

**Decision**: The auth middleware's `EnsureProfile` path needs access to the orgs and projects services. Pass an `AutoProvisionService` interface to the auth middleware that encapsulates the org+project creation logic. The implementation lives in a new thin service that wraps orgs and projects service calls.

**Rationale**: Avoids circular dependency between auth and domain packages. The interface keeps the auth package decoupled from domain internals.

## Risks / Trade-offs

**[Risk] Race condition on concurrent first requests** → The `EnsureProfile` already uses `ON CONFLICT DO UPDATE` for profile creation. The auto-provisioning should only trigger when the profile was actually inserted (not when it hit the conflict path). We can detect this by checking whether the profile's `created_at` was just set, or by using a return value from the insert.

**[Risk] Org name uniqueness conflict** → Org names have a unique constraint. If `"John Smith's Org"` already exists (different user with same name), creation will fail. → Mitigation: Catch the conflict error and append a numeric suffix (e.g., `"John Smith's Org 2"`) or fallback to email-based name.

**[Risk] Partial provisioning failure** → If org creation succeeds but project creation fails, the user has an org but no project. → Mitigation: Wrap both operations in a single transaction, or accept partial state since the user can manually create a project. Given the project creation is straightforward and unlikely to fail if org creation succeeded, partial state is acceptable.

**[Trade-off] Auth package coupling** → The auth middleware now indirectly depends on orgs/projects services. The interface boundary mitigates this, but it's still a new dependency chain. This is acceptable because auto-provisioning is a core onboarding concern.

**[Trade-off] Non-customizable defaults** → Users can't pick their org/project name during signup. They must rename after. This is simpler and we can add a setup wizard later if needed.
