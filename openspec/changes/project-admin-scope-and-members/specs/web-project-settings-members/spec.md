## Purpose

Provides a web UI Members section under Project Settings for viewing and managing a project's membership — listing members, inviting, removing, and changing roles — gated to project admins, with last-admin protection surfaced, and with member visibility following the existing list-members contract (any project member or an `org_admin` of the owning organization may see member email).

## ADDED Requirements

### Requirement: Navigate to project members

The gateway sidebar SHALL include a Members entry in the Settings group, and the Settings sub-navigation SHALL include a Members item, both opening the project members page at `/settings/members`.

#### Scenario: Sidebar and settings-nav entries present

- **WHEN** the sidebar and the Settings sub-navigation load
- **THEN** a Members link is shown in the Settings group and in the Settings sub-navigation, opening `/settings/members`

### Requirement: List project members

The members page SHALL list the active project's members with their display name, email, and role, for any caller who can read the project's membership (any project member or an `org_admin` of the owning organization) — the same visibility the existing `GET /api/projects/:id/members` contract already provides. Management controls are gated separately (see "Member management authority").

#### Scenario: Members listed with role

- **WHEN** the members page loads
- **THEN** each member is listed with their display name and role, and (for members) their email

### Requirement: Member management authority

The members page SHALL gate the invite, remove, and role-change actions to a caller who is a `project_admin` of the project or an `org_admin` of its owning organization. Such actions SHALL NOT be rendered at all for a non-admin caller (hidden, not disabled). The server SHALL independently enforce the same authority.

#### Scenario: Admin actions rendered for admins

- **GIVEN** the caller is a `project_admin` or `org_admin`
- **WHEN** the members page loads
- **THEN** the invite, remove, and role-change controls are rendered

#### Scenario: Admin actions hidden for non-admins

- **GIVEN** the caller is a `project_user` or `project_viewer` and not an `org_admin`
- **WHEN** the members page loads
- **THEN** the invite, remove, and role-change controls are not rendered

### Requirement: Change a member's role

The members page SHALL allow an authorized caller to change a member's role in place via `PATCH /api/projects/:id/members/:userId`, choosing from `project_admin`, `project_user`, and `project_viewer`.

#### Scenario: Role changed

- **WHEN** an authorized caller submits a role change for a member
- **THEN** the member's role is updated and the page reflects the new role

#### Scenario: Role change refused for non-admin

- **WHEN** a non-admin caller attempts a role change
- **THEN** the server responds with HTTP 403 and no role is changed

### Requirement: Remove a member

The members page SHALL allow an authorized caller to remove a member via `DELETE /api/projects/:id/members/:userId`.

#### Scenario: Member removed

- **WHEN** an authorized caller confirms a member removal
- **THEN** the member is removed from the project and their project tokens are revoked

### Requirement: Invite a member

The members page SHALL allow an authorized caller to invite a new member with a project-scoped role (`project_admin`, `project_user`, or `project_viewer`), reusing the existing invitation flow.

#### Scenario: Member invited

- **WHEN** an authorized caller invites an email address with a project role
- **THEN** a project invitation is created for that role

### Requirement: Last-admin protection surfaced

When a role change or removal would demote or remove the sole remaining `project_admin`, the members page SHALL surface the server's refusal clearly, and the change SHALL NOT be applied.

#### Scenario: Sole admin demotion refused

- **GIVEN** a project has exactly one `project_admin`
- **WHEN** an authorized caller attempts to demote that admin or remove them
- **THEN** the server responds with HTTP 403 `last-admin` and the members page shows the refusal

### Requirement: Token scope picker offers project:admin to admins only

The token scope picker (create and edit) SHALL offer `project:admin` only to a caller who is a `project_admin` of the active project or an `org_admin` of its owning organization. For other callers `project:admin` SHALL NOT be offered, and the server's `403 project-admin-scope-denied` SHALL be surfaced as a readable error if it is nonetheless attempted.

#### Scenario: project:admin offered to admins

- **GIVEN** the caller is a `project_admin` or `org_admin` of the active project
- **WHEN** the token scope picker loads
- **THEN** `project:admin` is among the selectable scopes

#### Scenario: project:admin hidden from non-admins

- **GIVEN** the caller is a `project_user` or `project_viewer` and not an `org_admin`
- **WHEN** the token scope picker loads
- **THEN** `project:admin` is not offered
