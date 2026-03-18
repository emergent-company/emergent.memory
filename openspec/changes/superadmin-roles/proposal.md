## Why

The current superadmin system uses a single binary flag (active/revoked in `core.superadmins` table). Operators need two distinct privilege levels: full-access superadmins who can modify platform data, and read-only superadmins who can inspect/troubleshoot without risk of accidental changes. This enables safer operational access for support engineers and reduces the blast radius of credential compromise.

## What Changes

- Add `role` column to `core.superadmins` table with two values: `superadmin_full` and `superadmin_readonly`
- Update authorization checks in superadmin handlers to enforce read-only restrictions
- Modify `/api/superadmin/me` endpoint to return role information
- Add migration to backfill existing superadmin grants as `superadmin_full`
- Update CLI commands to support role selection when granting superadmin access

## Capabilities

### New Capabilities
- `superadmin-role-enforcement`: Authorization middleware enforces read vs write permissions based on superadmin role

### Modified Capabilities
- None (superadmin endpoints exist but don't have requirement-level specs)

## Impact

**Affected code:**
- `apps/server/domain/superadmin/` - entity, repository, handler
- `apps/server/migrations/` - new migration for role column
- Potentially CLI commands if they handle superadmin grants (not yet investigated)

**Database:**
- New column `core.superadmins.role` with check constraint
- Existing rows backfilled to `superadmin_full`

**APIs:**
- `/api/superadmin/me` response includes role
- All mutating `/api/superadmin/*` endpoints check for `superadmin_full` role
- Read-only endpoints allow both `superadmin_full` and `superadmin_readonly`
