## Context

The current superadmin system (`apps/server/domain/superadmin/`) tracks privilege via `core.superadmins` table with a binary active/revoked model. Authorization is enforced per-handler via `requireSuperadmin()` helper, which checks `IsSuperadmin(userID)` repository method. Superadmin endpoints expose read operations (list users, orgs, projects, job queues) and write operations (soft-delete users/orgs/projects, manage jobs, bulk operations).

Operational need: support engineers need read-only access for troubleshooting without risk of data modification. Current binary model forces all-or-nothing access.

## Goals / Non-Goals

**Goals:**
- Two superadmin roles: `superadmin_full` (existing behavior) and `superadmin_readonly` (new)
- Read-only role can call GET endpoints and list operations
- Full role required for DELETE, POST, bulk operations
- Backward compatible migration (existing grants become `superadmin_full`)
- Role visible in `/api/superadmin/me` response

**Non-Goals:**
- Fine-grained permissions (project-specific, resource-specific) — use existing project/org roles for that
- UI changes (frontend receives role, can hide buttons, but enforcement is server-side)
- Audit logging of superadmin actions (separate concern)

## Decisions

### 1. Store role as string column, not separate table

**Decision:** Add `role VARCHAR(50) NOT NULL DEFAULT 'superadmin_full'` to `core.superadmins` table with a check constraint.

**Rationale:**
- Simple: one row per user, easy to query (`WHERE user_id = ? AND revoked_at IS NULL`)
- Low cardinality: only two values, no need for normalization
- Check constraint prevents invalid values at DB level
- Default to `superadmin_full` ensures existing code works during migration

**Alternatives considered:**
- Separate `core.superadmin_roles` table: overkill for two values, adds join complexity
- Boolean flag `is_readonly`: less extensible if more roles added later

### 2. Enforce authorization in handlers, not middleware

**Decision:** Update `requireSuperadmin()` helper to return role; handlers check role explicitly for write operations.

**Rationale:**
- Keeps authorization logic near business logic (easier to audit)
- Flexible: some endpoints may allow readonly role (e.g., GET /users), others require full
- Avoids route-based middleware complexity (routes don't cleanly map to read/write)

**Alternatives considered:**
- Echo middleware on `/api/superadmin/*` routes: brittle, hard to differentiate GET vs DELETE on same route
- Permission strings in handler annotations: over-engineered for two roles

Example pattern:
```go
func (h *Handler) DeleteUser(c echo.Context) error {
    role, err := h.requireSuperadminRole(c, "superadmin_full")
    if err != nil {
        return err
    }
    // ... proceed with deletion
}
```

### 3. Role selection at grant time (CLI/API)

**Decision:** When granting superadmin access (currently manual DB insert), specify role. Default to `superadmin_full` if not specified.

**Rationale:**
- Explicit role choice reduces accidental privilege escalation
- Backward compatible: omitting role uses default

Implementation deferred to tasks (check if CLI handles grants or if purely manual).

### 4. `/api/superadmin/me` returns role

**Decision:** Change response from `{isSuperadmin: true}` to `{isSuperadmin: true, role: "superadmin_full"}`.

**Rationale:**
- Frontend can show role badge or hide write actions for readonly users
- Minimal API change (additive, not breaking)

## Risks / Trade-offs

**Risk: Forgetting to check role in new superadmin endpoints**
→ Mitigation: Add test helper that validates all POST/DELETE routes require `superadmin_full`. Document pattern in `apps/server/domain/superadmin/AGENT.md`.

**Trade-off: Every write handler checks role explicitly**
→ Slightly verbose (each handler calls `requireSuperadminRole(c, "superadmin_full")`), but explicit is safer than inferring from HTTP method.

**Risk: Migration doesn't backfill correctly**
→ Mitigation: Migration uses `UPDATE core.superadmins SET role = 'superadmin_full' WHERE role IS NULL` before adding NOT NULL constraint.

## Migration Plan

1. Add `role` column as nullable initially
2. Backfill existing rows to `superadmin_full`
3. Alter column to `NOT NULL DEFAULT 'superadmin_full'`
4. Add check constraint `role IN ('superadmin_full', 'superadmin_readonly')`
5. Deploy server (hot reload picks up entity/handler changes)
6. Rollback: column can remain if needed, defaults to full access (no behavior change for existing users)

No downtime required (additive column, backward compatible default).

## Open Questions

- Does CLI currently have `memory superadmin grant <user-id>` command, or are grants purely manual DB inserts? → Investigate before task creation
- Should readonly role allow viewing email job template data (potentially sensitive)? → Default to yes (readonly sees everything), can restrict later if needed
