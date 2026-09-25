## Purpose

Reconciles the `project-invitations` capability spec with the shipped implementation: the legacy `kb.project_invitations` table and `/v1/.../invitations` routes no longer exist; invitations live in `kb.invites` and are served under `/api/invites` and `/invites/accept`. Also captures the behaviour change that acceptance now grants the role the invitation carries (`org_admin` for `org_admin` invitations) instead of a hardcoded `member`.

## REMOVED Requirements

### Requirement: Create project invitation
**Reason**: The shipped endpoint is `POST /api/invites` (org- or project-scoped) with roles `org_admin`, `project_admin`, `project_user`, `project_viewer`, not the documented `POST /v1/projects/:projectId/invitations` restricted to `project_user`/`project_viewer`. Replaced by "Create invitation".

**Migration**: Callers of the invite endpoint use `POST /api/invites`; no data migration — the `kb.invites` table is unchanged.

### Requirement: Invitation email content
**Reason**: The documented content (a "step-by-step guide to install the memory CLI") is not what the shipped `project-invitation` template carries. Replaced by "Invitation email delivery".

**Migration**: None — the email template is unchanged; only the spec description is corrected.

### Requirement: Accept invitation
**Reason**: The documented `GET /v1/invitations/:token/accept` surface, account provisioning, and bootstrap-token return do not exist. Acceptance is served by `GET /invites/accept` and `POST /api/invites/accept`, requires the invitee's authenticated identity, and grants membership rather than provisioning an account. Replaced by "Accept an invitation".

**Migration**: None — no data migration; the accept flow is unchanged by this reconciliation.

### Requirement: Invitation record persistence
**Reason**: Invitations are stored in `kb.invites` (columns `organization_id`, `email`, `role`, `token`, `status`, `expires_at`, `accepted_at`, `revoked_at`, `created_at`, `invited_by_user_id`), not the documented `kb.project_invitations` table with `invited_email`/`invited_role`/`token_hash`. Replaced by "Invitation record storage".

**Migration**: None — `kb.invites` already exists; `kb.project_invitations` was never created.

### Requirement: Revoke invitation
**Reason**: The documented `DELETE /v1/projects/:projectId/invitations/:invitationId` route does not exist; revocation is served by `DELETE /api/invites/:id`. Replaced by "Revoke an invitation".

**Migration**: None — no data migration.

## ADDED Requirements

### Requirement: Create invitation
The system SHALL provide a `POST /api/invites` endpoint (authentication required). The request body SHALL include `orgId` (required), `email` (required, must contain an `@`), and `role` (required; one of `org_admin`, `project_admin`, `project_user`, `project_viewer`), and MAY include `projectId` to scope the invitation to a project. The system SHALL generate a random token, store the invitation in `kb.invites` with a 7-day expiry, and enqueue a `project-invitation` email job. If the role is not one of the allowed values, the server SHALL return HTTP 400. If a pending invitation already exists for the same email and scope, the server SHALL return HTTP 400.

#### Scenario: Admin invites a new viewer
- **WHEN** an authenticated user sends `POST /api/invites` with `{"orgId":"<org>","email":"alice@example.com","role":"project_viewer"}`
- **THEN** the server stores a pending invitation, enqueues a `project-invitation` email job, and returns HTTP 201 with the invitation including its token and expiry

#### Scenario: Invalid role rejected
- **WHEN** the request `role` is not one of `org_admin`, `project_admin`, `project_user`, `project_viewer`
- **THEN** the server responds with HTTP 400 and stores no invitation

#### Scenario: Duplicate pending invitation
- **WHEN** a pending invitation already exists for the same email and scope
- **THEN** the server responds with HTTP 400 and stores no new invitation

### Requirement: Invitation email delivery
The system SHALL send a `project-invitation` email to the invited address. The email SHALL include the inviting user's name, the project name, the role being granted, and a single-use accept URL valid for 7 days.

#### Scenario: Invitation email delivered
- **WHEN** an invitation is created
- **THEN** an email job with template `project-invitation` is enqueued and the email contains the accept URL and role label

### Requirement: List invitations for the current user
The system SHALL provide a `GET /api/invites/pending` endpoint (authentication required) that returns the pending, unexpired invitations addressed to any of the current user's email addresses, each with its organization and optional project context and role.

#### Scenario: Pending invitations listed
- **WHEN** an authenticated user requests their pending invitations
- **THEN** the server returns the pending invitations for the user's email addresses with their organization, project, and role

### Requirement: List invitations for a project
The system SHALL provide a `GET /api/projects/:projectId/invites` endpoint (authentication required) that returns all invitations for a project, ordered by creation time.

#### Scenario: Project invitations listed
- **WHEN** an authenticated user requests invitations for a project
- **THEN** the server returns each invitation with its email, role, status, and creation time

### Requirement: Accept an invitation
The system SHALL accept a pending invitation via `GET /invites/accept?token=...` (which redirects unauthenticated users to login) or `POST /api/invites/accept` with a JSON body `{"token":"..."}`. On acceptance the system SHALL mark the invitation `accepted`, grant the invitee project membership with the invited role when the invitation is project-scoped, and grant organization membership with the role the invitation carries (`org_admin` for `org_admin` invitations, `member` for `project_admin`/`project_user`/`project_viewer` invitations). If the stored role is not one of the allowed invitation roles, the system SHALL fail closed and grant no membership. If the token is invalid, expired, or already processed, the system SHALL return HTTP 404.

#### Scenario: org_admin invitation grants org_admin membership
- **WHEN** a user accepts an `org_admin` invitation addressed to them
- **THEN** the invitee's `kb.organization_memberships` role is `org_admin` and the invitation is marked `accepted`

#### Scenario: project-scoped invitation grants member membership
- **WHEN** a user accepts a `project_admin`, `project_user`, or `project_viewer` invitation addressed to them
- **THEN** the invitee's `kb.organization_memberships` role is `member`, and a `kb.project_memberships` row is inserted with the invited role when the invitation is project-scoped

#### Scenario: unexpected stored role fails closed
- **WHEN** an invitation row carries a role outside the allowed set
- **THEN** the server returns an error and grants no membership

#### Scenario: Invalid or expired token
- **WHEN** the accept token does not match a pending, unexpired invitation
- **THEN** the server responds with HTTP 404 and grants no membership

### Requirement: Invitation record storage
The system SHALL store invitations in the `kb.invites` table with fields `id`, `organization_id`, `project_id`, `email`, `role`, `token`, `status` (`pending` / `accepted` / `declined` / `revoked`), `expires_at`, `accepted_at`, `revoked_at`, `created_at`, and `invited_by_user_id`.

#### Scenario: Invitation record created on invite
- **WHEN** the invite endpoint is called successfully
- **THEN** a row is inserted into `kb.invites` with `status = 'pending'`

### Requirement: Decline an invitation
The system SHALL provide a `POST /api/invites/:id/decline` endpoint (authentication required) that marks a pending invitation addressed to the current user as `declined`.

#### Scenario: Invitation declined
- **WHEN** the current user declines a pending invitation addressed to them
- **THEN** the invitation `status` is set to `declined`

#### Scenario: Invitation not for the user
- **WHEN** a user attempts to decline an invitation addressed to a different email
- **THEN** the server responds with HTTP 403 and leaves the invitation unchanged

### Requirement: Revoke an invitation
The system SHALL provide a `DELETE /api/invites/:id` endpoint (authentication required) that sets a pending invitation's status to `revoked`. Revoking a non-pending invitation SHALL return HTTP 404.

#### Scenario: Pending invitation revoked
- **WHEN** an authenticated user revokes a pending invitation
- **THEN** the invitation `status` is set to `revoked` and the server returns HTTP 204

#### Scenario: Already-processed invitation cannot be revoked
- **WHEN** a user attempts to revoke an invitation that is not pending
- **THEN** the server returns HTTP 404 and the invitation is unchanged
