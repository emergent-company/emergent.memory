# project-invitations

## Purpose

Defines the invitation flow for adding new members to a project, including invitation creation, email delivery, acceptance, persistence, and revocation.

## Requirements

### Requirement: Create project invitation
The system SHALL provide a `POST /v1/projects/:projectId/invitations` endpoint. Only members with `project_admin` role MAY create invitations. The request body SHALL include `email` (required) and `role` (required; one of `project_user`, `project_viewer`). The system SHALL generate a signed invitation token, store the invitation record, and enqueue an email job. If the email already has an active membership in the project, the server SHALL return HTTP 409.

#### Scenario: Admin invites a new viewer
- **WHEN** a `project_admin` sends `POST /v1/projects/:id/invitations` with `{"email":"alice@example.com","role":"project_viewer"}`
- **THEN** the server creates an invitation record, enqueues a `project-invitation` email job, and returns HTTP 201 with the invitation ID and expiry

#### Scenario: Non-admin cannot invite
- **WHEN** a `project_user` or `project_viewer` calls the invitation endpoint
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Duplicate active membership
- **WHEN** the invited email already has an active membership in the project
- **THEN** the server responds with HTTP 409 Conflict

### Requirement: Invitation email content
The system SHALL send a `project-invitation` email to the invited address. The email SHALL include: the inviting user's display name, the project name, the role being granted, a step-by-step guide to install the `memory` CLI, and a single-use accept URL valid for 7 days.

#### Scenario: Invitation email delivered
- **WHEN** an invitation is created
- **THEN** an email job with template `project-invitation` is enqueued and the email contains the accept URL and CLI install instructions

### Requirement: Accept invitation
The system SHALL provide a `GET /v1/invitations/:token/accept` endpoint (no authentication required). If the token is valid and not expired, the server SHALL provision an account for the email if none exists, add a project membership with the invited role, mark the invitation as accepted, and return a short-lived bootstrap token so the CLI can complete login.

#### Scenario: New user accepts invitation
- **WHEN** a new user follows the accept link with a valid token
- **THEN** a user account is created, a `kb.project_memberships` record is inserted with the invited role, the invitation is marked `accepted`, and the response includes a bootstrap auth token

#### Scenario: Existing user accepts invitation
- **WHEN** an existing user follows the accept link with a valid token
- **THEN** a `kb.project_memberships` record is inserted and the invitation is marked `accepted`

#### Scenario: Expired invitation token
- **WHEN** the accept link is followed after 7 days
- **THEN** the server responds with HTTP 410 Gone

#### Scenario: Already-used invitation token
- **WHEN** the accept link is followed a second time
- **THEN** the server responds with HTTP 409 Conflict

### Requirement: Invitation record persistence
The system SHALL store invitations in a `kb.project_invitations` table with fields: `id`, `project_id`, `invited_email`, `invited_role`, `token_hash`, `invited_by_user_id`, `status` (pending / accepted / expired / revoked), `expires_at`, `created_at`, `accepted_at`.

#### Scenario: Invitation record created on invite
- **WHEN** the invite endpoint is called successfully
- **THEN** a row is inserted into `kb.project_invitations` with `status = 'pending'`

### Requirement: Revoke invitation
The system SHALL provide a `DELETE /v1/projects/:projectId/invitations/:invitationId` endpoint. Only `project_admin` members MAY revoke invitations. Revoking a pending invitation SHALL set its status to `revoked`. Revoking a non-pending invitation SHALL return HTTP 409.

#### Scenario: Admin revokes pending invitation
- **WHEN** a `project_admin` deletes a pending invitation
- **THEN** the invitation `status` is set to `revoked` and the server returns HTTP 204

#### Scenario: Cannot revoke accepted invitation
- **WHEN** an admin tries to revoke an already-accepted invitation
- **THEN** the server returns HTTP 409 Conflict
