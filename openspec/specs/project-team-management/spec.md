# project-team-management

## Purpose

Defines the API endpoints and CLI commands for managing project team membership, including listing members, removing members, and sending invitations.

## Requirements

### Requirement: List project members API
The system SHALL provide a `GET /v1/projects/:projectId/members` endpoint. Any authenticated project member MAY call it. The response SHALL include each member's user ID, email, display name, role, joined date, and optionally usage stats (token last-used-at, number of API requests in the last 30 days) when `?stats=true` is provided.

#### Scenario: List members without stats
- **WHEN** a project member calls `GET /v1/projects/:id/members`
- **THEN** the server returns an array of members with id, email, displayName, role, joinedAt

#### Scenario: List members with stats
- **WHEN** a project member calls `GET /v1/projects/:id/members?stats=true`
- **THEN** each member entry additionally includes `lastActiveAt` and `requestCount30d`

### Requirement: Remove project member API
The system SHALL provide a `DELETE /v1/projects/:projectId/members/:userId` endpoint. Only `project_admin` members MAY remove other members. An admin MAY NOT remove themselves if they are the sole admin. The endpoint SHALL delete the corresponding `kb.project_memberships` row and revoke all API tokens for that user in that project.

#### Scenario: Admin removes a member
- **WHEN** a `project_admin` calls `DELETE /v1/projects/:id/members/:userId`
- **THEN** the membership record is deleted and the member's project tokens are revoked; server returns HTTP 204

#### Scenario: Last admin cannot remove themselves
- **WHEN** the sole `project_admin` attempts to remove themselves
- **THEN** the server returns HTTP 409 with a message explaining at least one admin must remain

#### Scenario: Non-admin cannot remove members
- **WHEN** a `project_user` or `project_viewer` calls the remove endpoint
- **THEN** the server returns HTTP 403 Forbidden

### Requirement: CLI team list command
The CLI SHALL provide `memory projects team list [project-name-or-id]` that displays all members of the active or specified project. Output SHALL show: numbered index, display name, email, role, and joined date. The `--stats` flag SHALL additionally show each member's last active date and 30-day request count. The command SHALL support `--json` for machine-readable output.

#### Scenario: List team in text mode
- **WHEN** user runs `memory projects team list`
- **THEN** CLI prints a numbered list with name, email, role, and joined date for each member

#### Scenario: List team with stats
- **WHEN** user runs `memory projects team list --stats`
- **THEN** CLI prints the same list plus last-active and 30-day request count columns

#### Scenario: List team as JSON
- **WHEN** user runs `memory projects team list --json`
- **THEN** CLI outputs a JSON array of member objects

### Requirement: CLI team invite command
The CLI SHALL provide `memory projects team invite <email> [project-name-or-id]` with a `--role` flag (default: `project_viewer`; accepted values: `project_viewer`, `project_user`). On success, the CLI SHALL print a confirmation message including the invitee's email and role. The command SHALL be restricted to users with `project_admin` role.

#### Scenario: Invite a viewer
- **WHEN** user runs `memory projects team invite alice@example.com --role project_viewer`
- **THEN** CLI calls the invitation API and prints "Invitation sent to alice@example.com (viewer)"

#### Scenario: Invite with default role
- **WHEN** user runs `memory projects team invite bob@example.com` without --role
- **THEN** CLI defaults to `project_viewer` and prints confirmation

#### Scenario: Non-admin invite attempt
- **WHEN** a non-admin runs the invite command
- **THEN** CLI prints an error from the API indicating insufficient permissions

### Requirement: CLI team remove command
The CLI SHALL provide `memory projects team remove <email> [project-name-or-id]`. The command SHALL look up the member by email, confirm the action with a prompt (unless `--yes` is passed), and call the remove API. On success, the CLI SHALL print a confirmation.

#### Scenario: Remove a member with confirmation
- **WHEN** user runs `memory projects team remove alice@example.com`
- **THEN** CLI prompts "Remove alice@example.com from project? [y/N]" and on confirmation removes the member

#### Scenario: Remove with --yes flag
- **WHEN** user runs `memory projects team remove alice@example.com --yes`
- **THEN** CLI skips confirmation and removes the member immediately

#### Scenario: Member not found
- **WHEN** the given email is not a member of the project
- **THEN** CLI prints an error "alice@example.com is not a member of this project"
