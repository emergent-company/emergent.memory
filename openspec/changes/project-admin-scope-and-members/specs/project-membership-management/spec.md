## Purpose

Defines the in-place project-member role-change contract: the `PATCH /api/projects/:id/members/:userId` endpoint, its authority, the role vocabulary, the last-admin guard, and the token revocation on downgrade.

## ADDED Requirements

### Requirement: Change project member role

The system SHALL provide a `PATCH /api/projects/:id/members/:userId` endpoint that changes an existing member's role in place (updating the `kb.project_memberships.role` row). The accepted role values SHALL be exactly `project_admin`, `project_user`, and `project_viewer`; any other value SHALL return HTTP 400 (`invalid-role`). The request body SHALL be `{ "role": "<role>" }`. A missing member or project SHALL return HTTP 404.

#### Scenario: Role changed in place

- **WHEN** an authorized caller submits `{"role": "project_user"}` for an existing member
- **THEN** the member's `kb.project_memberships.role` is updated to `project_user` and the server returns HTTP 200

#### Scenario: Invalid role rejected

- **WHEN** the request `role` is not one of `project_admin`, `project_user`, `project_viewer`
- **THEN** the server responds with HTTP 400 and no membership is changed

#### Scenario: Missing member or project

- **WHEN** the `:userId` or `:id` does not identify an existing member/project
- **THEN** the server responds with HTTP 404 and no membership is changed

### Requirement: Role-change authority

The system SHALL authorize `PATCH /api/projects/:id/members/:userId` to a caller who is a `project_admin` of the addressed project or an `org_admin` of its owning organization (resolved server-side from `kb.projects.organization_id`). A caller who is a `project_user` or `project_viewer`, or a caller from a different organization, SHALL receive HTTP 403. A foreign or unknown project SHALL receive HTTP 404.

#### Scenario: Project admin changes a role

- **GIVEN** a caller holds `project_admin` in project P
- **WHEN** the caller patches a member of P
- **THEN** the change is authorized

#### Scenario: Org admin of the owning org changes a role

- **GIVEN** a caller holds `org_admin` in the organization that owns project P
- **WHEN** the caller patches a member of P
- **THEN** the change is authorized

#### Scenario: Non-admin refused

- **GIVEN** a caller holds `project_user` or `project_viewer` in project P and no `org_admin` membership in P's owning org
- **WHEN** the caller patches a member of P
- **THEN** the server responds with HTTP 403 and no membership is changed

### Requirement: Last-admin protection

The system SHALL refuse a role change that demotes the sole remaining `project_admin` (a change from `project_admin` to `project_user` or `project_viewer` when no other `project_admin` remains in the project). The refusal SHALL be HTTP 403 with an error explaining at least one admin must remain. A project SHALL always retain at least one `project_admin`.

#### Scenario: Demoting the sole admin is refused

- **GIVEN** a project has exactly one `project_admin`
- **WHEN** an authorized caller changes that member's role to `project_user` or `project_viewer`
- **THEN** the server responds with HTTP 403 and the role is unchanged

#### Scenario: Demoting one of several admins is allowed

- **GIVEN** a project has two or more `project_admin` members
- **WHEN** an authorized caller changes one of their roles to `project_user`
- **THEN** the change succeeds and at least one `project_admin` remains

### Requirement: Token revocation on downgrade

The system SHALL revoke the member's project-scoped tokens when a role change is a downgrade — a rank decrease (`project_admin` → `project_user`/`project_viewer`, or `project_user` → `project_viewer`) — so stale privileges cannot outlive the role change. An upgrade or a no-op change SHALL revoke nothing. A token-revocation failure SHALL be non-fatal: the role change stands and the failure is logged.

#### Scenario: Downgrade revokes the member's project tokens

- **GIVEN** a member holds `project_admin` with active project-scoped tokens
- **WHEN** their role is changed to `project_user` or `project_viewer`
- **THEN** their project-scoped tokens are revoked and the role change stands

#### Scenario: Upgrade revokes nothing

- **GIVEN** a member holds `project_viewer` with active read-only project-scoped tokens
- **WHEN** their role is changed to `project_admin`
- **THEN** no tokens are revoked and the role change stands
