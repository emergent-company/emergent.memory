## Purpose

Document the authorization posture of the `/api/embeddings` control group (issue #940). The group mixes project-scoped reads consumed by the embeddings status page with deployment-wide operator controls that must be restricted to platform admins.

## ADDED Requirements

### Requirement: Authorize the embedding control endpoints

The `/api/embeddings` control group SHALL enforce a distinct authorization posture per endpoint, each fail-closed:

- `GET /status` and `GET /progress` SHALL be project-scoped reads. The caller SHALL be a member of the addressed project's organization, enforced by the shared `RequireProjectTokenScope` + `RequireProjectMember` pair. `progress` SHALL additionally scope its queue counts to the caller's own project, never returning deployment-wide aggregates to a non-admin.
- `POST /pause`, `POST /resume`, `PATCH /config`, `DELETE /queue`, and `POST /reset-schedule` SHALL require platform admin write authority (`admin:write`).
- `GET /diagnose` SHALL require platform admin read authority (`admin:read`).
- `GET /progress` with no project context SHALL return the deployment-wide view only when the caller holds `admin:read`; a caller with neither project membership nor admin authority SHALL receive 403.

#### Scenario: Operator write denied for non-admin

- **WHEN** an authenticated non-admin caller invokes `pause`, `resume`, `config`, `queue`, or `reset-schedule`
- **THEN** the server responds 403

#### Scenario: Operator write allowed for admin

- **WHEN** a platform admin invokes `pause`, `resume`, `config`, `queue`, or `reset-schedule`
- **THEN** the operation succeeds

#### Scenario: Diagnostic read denied for non-admin

- **WHEN** an authenticated non-admin caller invokes `diagnose`
- **THEN** the server responds 403

#### Scenario: Project member sees only their own progress

- **WHEN** a project member requests `progress` with their project context
- **THEN** the queue counts are scoped to that project, not the deployment-wide aggregate

#### Scenario: Progress without project context is admin-only

- **WHEN** an authenticated non-admin caller requests `progress` without a project context
- **THEN** the server responds 403
