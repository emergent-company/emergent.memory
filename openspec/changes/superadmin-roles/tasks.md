## 1. Database Migration

- [ ] 1.1 Create new Goose migration file in `apps/server/migrations/` for adding role column
- [ ] 1.2 Add `role` column to `core.superadmins` table as VARCHAR(50) nullable initially
- [ ] 1.3 Backfill all existing rows with `role = 'superadmin_full'`
- [ ] 1.4 Alter column to add NOT NULL constraint with default 'superadmin_full'
- [ ] 1.5 Add check constraint `role IN ('superadmin_full', 'superadmin_readonly')`
- [ ] 1.6 Test migration locally (up and down)

## 2. Update Superadmin Entity

- [ ] 2.1 Add `Role` field to `Superadmin` struct in `apps/server/domain/superadmin/entity.go`
- [ ] 2.2 Add bun struct tag for role column mapping

## 3. Update Superadmin Repository

- [ ] 3.1 Update `IsSuperadmin()` method signature to return `(role string, isSuperadmin bool, err error)` in `apps/server/domain/superadmin/repository.go`
- [ ] 3.2 Modify query to select role column when checking superadmin status
- [ ] 3.3 Return empty string for role when user is not superadmin

## 4. Update Superadmin DTOs

- [ ] 4.1 Add `Role` field to `SuperadminMeResponse` struct in `apps/server/domain/superadmin/dto.go`
- [ ] 4.2 Ensure JSON serialization includes role field

## 5. Update Handler Authorization

- [ ] 5.1 Create new `requireSuperadminRole(c echo.Context, requiredRole string) (userID string, err error)` helper in `apps/server/domain/superadmin/handler.go`
- [ ] 5.2 Update existing `requireSuperadmin()` helper to call `requireSuperadminRole` with empty role (allows both)
- [ ] 5.3 Update `GetMe()` handler to return role in response
- [ ] 5.4 Update all write handlers (DeleteUser, DeleteOrganization, DeleteProject) to use `requireSuperadminRole(c, "superadmin_full")`
- [ ] 5.5 Update bulk job operation handlers (DeleteEmbeddingJobs, CleanupOrphanEmbeddingJobs, DeleteExtractionJobs, CancelExtractionJobs, DeleteDocumentParsingJobs, RetryDocumentParsingJobs, DeleteSyncJobs, CancelSyncJobs) to require `superadmin_full` role
- [ ] 5.6 Verify all GET handlers allow both roles (no changes needed, but verify logic)

## 6. Testing

- [ ] 6.1 Write test for `requireSuperadminRole()` helper with full role
- [ ] 6.2 Write test for `requireSuperadminRole()` helper with readonly role denied write access
- [ ] 6.3 Write test for `GetMe()` returning role for full superadmin
- [ ] 6.4 Write test for `GetMe()` returning role for readonly superadmin
- [ ] 6.5 Write test for write handler rejecting readonly superadmin (403)
- [ ] 6.6 Write test for read handler allowing readonly superadmin (200)
- [ ] 6.7 Manual test: Create readonly superadmin in DB, verify API restrictions

## 7. Documentation

- [ ] 7.1 Update `apps/server/domain/superadmin/AGENT.md` (if exists) or create it with role enforcement pattern
- [ ] 7.2 Document migration rollback procedure
- [ ] 7.3 Add comment in handler.go explaining which endpoints require full vs readonly

## 8. Verification

- [ ] 8.1 Run migration on local dev database
- [ ] 8.2 Verify hot reload picks up changes (check logs/server/server.log)
- [ ] 8.3 Test `/api/superadmin/me` endpoint returns role
- [ ] 8.4 Test readonly superadmin can GET /api/superadmin/users
- [ ] 8.5 Test readonly superadmin blocked from DELETE /api/superadmin/users/:id (403)
- [ ] 8.6 Test full superadmin can perform all operations
