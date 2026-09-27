## Purpose

Document the authorization posture of the `/api/embeddings` control group (issue #940). The group mixes project-scoped reads consumed by the embeddings status page with deployment-wide operator controls that must be restricted to platform admins.

## ADDED Requirements

### Requirement: Authorize the embedding control endpoints

The `/api/embeddings` control group SHALL enforce a distinct authorization posture per endpoint, each fail-closed:

- `GET /status` and `GET /progress` SHALL be project-scoped reads. The caller SHALL be a member of the addressed project's organization, enforced by the shared `RequireProjectTokenScope` + `RequireProjectMember` pair. `progress` SHALL additionally scope its queue counts to the caller's own project, never returning deployment-wide aggregates to a non-admin.
- `POST /pause`, `POST /resume`, `PATCH /config`, `DELETE /queue`, and `POST /reset-schedule` SHALL require an active `superadmin_full` grant (a `core.superadmins` row with `role = 'superadmin_full'` and `revoked_at IS NULL`).
- `GET /diagnose` SHALL require the same active `superadmin_full` grant.
- `GET /progress` with no project context SHALL return the deployment-wide view only when the caller holds an active `superadmin_full` grant; a caller with neither project membership nor a `superadmin_full` grant SHALL receive 403.

The operator writes and diagnostics SHALL NOT be satisfied by a scope check: an `admin:all` token is mintable by any `org_admin` (`pkg/auth.CanGrantAdminAll`), so a scope gate (`admin:write`/`admin:read`) would admit a non-platform-admin caller. The admitted set for deployment-wide controls is `superadmin_full` only — `superadmin_readonly`, `org_admin`, and any `admin`/`admin:all` token are refused.

#### Scenario: Operator write denied for an org_admin's admin:all token

- **WHEN** an `org_admin` mints an `admin:all` token and invokes `pause`, `resume`, `config`, `queue`, or `reset-schedule`
- **THEN** the server responds 403

#### Scenario: Operator write allowed for superadmin_full

- **WHEN** an active `superadmin_full` principal invokes `pause`, `resume`, `config`, `queue`, or `reset-schedule`
- **THEN** the operation succeeds

#### Scenario: Diagnostic read denied for non-superadmin

- **WHEN** an authenticated caller without a `superadmin_full` grant invokes `diagnose`
- **THEN** the server responds 403

#### Scenario: Project member sees only their own progress

- **WHEN** a project member requests `progress` with their project context
- **THEN** the queue counts are scoped to that project, not the deployment-wide aggregate

#### Scenario: Progress without project context is superadmin-only

- **WHEN** an authenticated caller without a `superadmin_full` grant requests `progress` without a project context
- **THEN** the server responds 403
