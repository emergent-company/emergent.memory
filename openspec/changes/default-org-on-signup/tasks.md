## 1. Propagate OIDC Name Claims Through Auth Pipeline

- [x] 1.1 Add `GivenName`, `FamilyName` fields to `IntrospectionResult` struct in `apps/server/pkg/auth/zitadel.go` and map them from `introspectionResponse`
- [x] 1.2 Add `GivenName`, `FamilyName`, `Name` fields to `TokenClaims` struct in `apps/server/pkg/auth/middleware.go`
- [x] 1.3 Populate `TokenClaims` name fields from `IntrospectionResult` in the introspection code path
- [x] 1.4 Populate `TokenClaims` name fields from `UserInfoResult` in the userinfo fallback code path
- [x] 1.5 Update `ensureUserProfile` in `middleware.go` to map `TokenClaims` name fields into `UserProfileInfo` (FirstName, LastName, DisplayName)

## 2. Update EnsureProfile to Populate Names on Creation

- [x] 2.1 Modify `EnsureProfile` in `apps/server/pkg/auth/user_profile.go` to set `first_name`, `last_name`, and `display_name` from `UserProfileInfo` only during initial profile creation (not on conflict/update path)
- [x] 2.2 Ensure the `ON CONFLICT DO UPDATE` clause does NOT overwrite existing name fields when they are already populated

## 3. Create AutoProvisionService Interface and Implementation

- [x] 3.1 Define `AutoProvisionService` interface in `apps/server/pkg/auth/` with a method like `ProvisionNewUser(ctx context.Context, userID string, profile *UserProfileInfo) error`
- [x] 3.2 Create implementation struct (e.g., in `apps/server/domain/autoprovision/` or a suitable location) that depends on `orgs.Service` and `projects.Service`
- [x] 3.3 Implement org name derivation logic: `"<FirstName> <LastName>'s Org"` → `"<DisplayName>'s Org"` → `"<email-local-part>'s Org"` → `"My Organization"`
- [x] 3.4 Implement org creation via `orgs.Service.Create()` with conflict handling (append numeric suffix on name collision)
- [x] 3.5 Implement project creation via `projects.Service.Create()` with derived name (`"<FirstName>'s Project"` with same fallback chain as org) in the new org
- [x] 3.6 Add error logging for partial failures (org succeeds but project fails) — do not roll back org on project failure

## 4. Wire Auto-Provisioning into Auth Middleware

- [x] 4.1 Add `AutoProvisionService` as an optional dependency on the auth `Middleware` struct
- [x] 4.2 Call `AutoProvisionService.ProvisionNewUser()` from `EnsureProfile` when a new profile is created (not on existing/reactivated profiles)
- [x] 4.3 Register the `AutoProvisionService` implementation in the fx dependency graph (`cmd/server/main.go` or appropriate module)

## 5. Testing

- [x] 5.1 Write unit tests for org name derivation logic (all fallback cases: full name, display name only, email only, no data)
- [x] 5.2 Write unit tests for `EnsureProfile` name propagation (new user gets names, existing user names not overwritten)
- [x] 5.3 Write integration test verifying end-to-end: new user auth → profile created with names → org created → project created with agent provisioned
- [x] 5.4 Write test for org name conflict resolution (duplicate name scenario)
- [x] 5.5 Write test for reactivated user (soft-deleted profile) — verify no auto-provisioning triggered
- [x] 5.6 Verify existing tests still pass (`task test` from repo root)
