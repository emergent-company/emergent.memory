# project-invitations

## Purpose

Defines the invitation flow for adding new members to an organization or project, including invitation creation, email delivery, listing, acceptance, persistence, decline, and revocation, and the organization membership role an accepted invitation grants.

## Requirements

### Requirement: Create invitation
The system SHALL provide a `POST /api/invites` endpoint (authentication required). The request body SHALL include `orgId` (required), `email` (required, must contain an `@`), and `role` (required; one of `org_admin`, `project_admin`, `project_user`, `project_viewer`), and MAY include `projectId` to scope the invitation to a project. The system SHALL generate a random token, store the invitation in `kb.invites` with a 7-day expiry, and enqueue a `project-invitation` email job. If the role is not one of the allowed values, the server SHALL return HTTP 400. If a pending invitation already exists for the same email and scope, the server SHALL return HTTP 400.

The target project and organization are sourced from the request body (which the route membership middleware cannot inspect), so the system SHALL authorize both at the handler, never trusting a client-supplied value:

- When `projectId` is supplied, the system SHALL authorize the caller against the target project (the supplied `projectId` MUST agree with the caller's authenticated project context and the caller MUST be a member of the project's owning organization — HTTP 403 otherwise, HTTP 404 for an unknown project), and SHALL bind the body `orgId` to the project's server-resolved owning organization (`kb.projects.organization_id`); a contradictory `orgId` SHALL be rejected with HTTP 400.
- When `projectId` is omitted, the system SHALL require the caller to be a member of the supplied `orgId` (resolved against `kb.organization_memberships`); a non-member SHALL receive HTTP 403.

#### Scenario: Admin invites a new viewer
- **WHEN** an authenticated user sends `POST /api/invites` with `{"orgId":"<org>","email":"alice@example.com","role":"project_viewer"}`
- **THEN** the server stores a pending invitation, enqueues a `project-invitation` email job, and returns HTTP 201 with the invitation including its token and expiry

#### Scenario: Invalid role rejected
- **WHEN** the request `role` is not one of `org_admin`, `project_admin`, `project_user`, `project_viewer`
- **THEN** the server responds with HTTP 400 and stores no invitation

#### Scenario: Duplicate pending invitation
- **WHEN** a pending invitation already exists for the same email and scope
- **THEN** the server responds with HTTP 400 and stores no new invitation

#### Scenario: Contradictory orgId and projectId rejected
- **WHEN** a member supplies `projectId` for their own project but an `orgId` that is not that project's owning organization
- **THEN** the server responds with HTTP 400 and stores no invitation

#### Scenario: Non-member cannot create an org-level invitation
- **WHEN** a caller who is not a member of the supplied `orgId` (and no `projectId`) attempts to create an invitation
- **THEN** the server responds with HTTP 403 and stores no invitation

### Requirement: Role-grant authorization
The system SHALL gate an invitation whose `role` is `org_admin` on the caller's own authority: only a caller who is an `org_admin` of the target organization (or an active `superadmin_full`) MAY create such an invitation. A plain member MAY invite at or below their own level, but a role above the caller's authority SHALL be refused with HTTP 403 and no invitation SHALL be stored. The caller's authority SHALL be resolved server-side from `kb.organization_memberships` and `core.superadmins`, never from a request-controlled value.

#### Scenario: member cannot create an org_admin invitation
- **WHEN** a member of an organization attempts to create an invitation with `role: "org_admin"` for that organization
- **THEN** the server responds with HTTP 403 and stores no invitation

#### Scenario: org_admin creates an org_admin invitation
- **WHEN** an `org_admin` of an organization creates an invitation with `role: "org_admin"` for that organization
- **THEN** the server stores the invitation and returns HTTP 201

#### Scenario: member creates a member-granting invitation
- **WHEN** a member of an organization creates an invitation with a project-scoped role (`project_admin`, `project_user`, or `project_viewer`)
- **THEN** the server stores the invitation and returns HTTP 201

### Requirement: Project-scoped org_admin is rejected
`org_admin` is an organization-level role with no meaning in project scope. The system SHALL reject a project-scoped invitation (one that supplies `projectId`) whose `role` is `org_admin` with HTTP 400 and SHALL store no invitation, because granting it would write an out-of-vocabulary `org_admin` value into `kb.project_memberships.role`.

#### Scenario: project-scoped org_admin invitation rejected
- **WHEN** a caller supplies `projectId` and `role: "org_admin"` to `POST /api/invites`
- **THEN** the server responds with HTTP 400 and stores no invitation

### Requirement: Invitation email delivery

The system SHALL send a `project-invitation` email to the invited address. The email SHALL include the inviting user's name, the project name, the role being granted, and a single-use accept URL valid for 7 days.

A missing template, a failed template render, or a `project-invitation` job whose template data lacks a usable accept URL SHALL fail the email job (`failed` or `dead_letter` with `last_error` recorded) and SHALL NOT be sent or marked `sent`. The accept URL SHALL be an absolute `http(s)` URL; a relative or empty accept URL SHALL fail the job. The system SHALL NOT deliver an invitation email without an accept link.

#### Scenario: Invitation email delivered

- **WHEN** an invitation is created
- **THEN** an email job with template `project-invitation` is enqueued and the email contains the accept URL and role label

#### Scenario: Missing template fails instead of sending a link-less invite

- **WHEN** a `project-invitation` job is processed and the template cannot be found
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`

#### Scenario: Render failure fails instead of sending a link-less invite

- **WHEN** a `project-invitation` job is processed and the template render returns an error
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`

#### Scenario: Missing or relative accept URL fails

- **WHEN** a `project-invitation` job is processed and its template data has no accept URL, or an accept URL that is not an absolute `http(s)` URL
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`

### Requirement: List invitations for the current user
The system SHALL provide a `GET /api/invites/pending` endpoint (authentication required) that returns the pending, unexpired invitations addressed to any of the current user's email addresses, each with its organization and optional project context and role.

#### Scenario: Pending invitations listed
- **WHEN** an authenticated user requests their pending invitations
- **THEN** the server returns the pending invitations for the user's email addresses with their organization, project, and role

### Requirement: List invitations for a project
The system SHALL provide a `GET /api/projects/:projectId/invites` endpoint (authentication required) that returns all invitations for a project, ordered by creation time. The system SHALL authorize the caller against the `:projectId` path parameter (the shared project membership pair): a caller who is not a member of the project's owning organization SHALL receive HTTP 403, and a caller addressing an unknown project SHALL receive HTTP 404.

#### Scenario: Project invitations listed
- **WHEN** an authenticated user requests invitations for a project
- **THEN** the server returns each invitation with its email, role, status, and creation time

### Requirement: Accept an invitation
The system SHALL accept a pending invitation via `GET /invites/accept?token=...` (which redirects unauthenticated users to login) or `POST /api/invites/accept` with a JSON body `{"token":"..."}`. On acceptance the system SHALL mark the invitation `accepted`, grant the invitee project membership with the invited role when the invitation is project-scoped, and grant organization membership with the role the invitation carries (`org_admin` for `org_admin` invitations, `member` for `project_admin`/`project_user`/`project_viewer` invitations). If the stored role is not one of the allowed invitation roles, the system SHALL fail closed and grant no membership. An `org_admin` grant SHALL additionally be refused (fail closed, no membership) unless the invitation's recorded inviter holds `org_admin` (or `superadmin_full`) authority over the invitation's organization — a pre-existing `org_admin` invitation minted by a plain member, or with no recorded inviter, cannot be used to self-escalate. A project-scoped invitation (one with a `project_id`) whose role is `org_admin` SHALL be refused on acceptance (fail closed, no project or organization membership written), because `org_admin` is an organization-level grant with no meaning in project scope and would write an out-of-vocabulary `org_admin` value into `kb.project_memberships.role`. If the token is invalid, expired, or already processed, the system SHALL return HTTP 404.

#### Scenario: org_admin invitation grants org_admin membership
- **WHEN** a user accepts an `org_admin` invitation addressed to them, whose recorded inviter is an `org_admin` of the invitation's organization
- **THEN** the invitee's `kb.organization_memberships` role is `org_admin` and the invitation is marked `accepted`

#### Scenario: org_admin invitation minted by a non-admin is refused
- **WHEN** a user accepts an `org_admin` invitation whose recorded inviter is not an `org_admin` (or `superadmin_full`), or has no recorded inviter
- **THEN** the server responds with an error and grants no membership

#### Scenario: project-scoped org_admin invitation is refused on acceptance
- **WHEN** a user accepts a project-scoped invitation (one with a `project_id`) whose role is `org_admin`
- **THEN** the server responds with an error and grants no project or organization membership

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
The system SHALL provide a `DELETE /api/invites/:id` endpoint (authentication required) that sets a pending invitation's status to `revoked`. The system SHALL require the caller to be an `org_admin` of the invitation's organization (or an active `superadmin_full`) before revoking — revoking is an org-tier write, not a membership read. A caller who is not an `org_admin` SHALL receive HTTP 403 (a plain member, a caller from a different organization, and a bare authenticated user are all refused fail-closed). A missing invitation SHALL return HTTP 404. Revoking a non-pending invitation SHALL return HTTP 404.

#### Scenario: Pending invitation revoked
- **WHEN** an `org_admin` of the invitation's organization revokes a pending invitation
- **THEN** the invitation `status` is set to `revoked` and the server returns HTTP 204

#### Scenario: A non-admin member cannot revoke
- **WHEN** a plain member of the invitation's organization attempts to revoke a pending invitation
- **THEN** the server returns HTTP 403 and the invitation is unchanged

#### Scenario: A caller from another organization cannot revoke
- **WHEN** a caller who is not an `org_admin` of the invitation's organization attempts to revoke it
- **THEN** the server returns HTTP 403 and the invitation is unchanged

#### Scenario: Already-processed invitation cannot be revoked
- **WHEN** a user attempts to revoke an invitation that is not pending
- **THEN** the server returns HTTP 404 and the invitation is unchanged

### Requirement: Invitation email delivery log

For each invitation returned by `GET /api/projects/:projectId/invites`, the system SHALL include the invitation's full email send history as a `deliveryLog` array, newest send first. Each entry SHALL describe one invite-scoped `kb.email_jobs` row (`source_type = 'invite'` and `source_id` equal to the invitation id), carrying:

- `jobId` and `createdAt` — the identity and enqueue time of that send;
- `processedAt` — when the send finished processing (Mailgun accepted it, or the job gave up), when known, so the log can report the real send time rather than the queue time;
- `status` — the email job's processing status (`pending`, `processing`, `sent`, `failed`, `dead_letter`);
- `deliveryStatus` / `deliveryStatusAt` — the job's latest Mailgun delivery state, when one has arrived;
- `lastError` — the job's recorded failure, when any;
- `events` — the Mailgun delivery events recorded against that job in `kb.email_logs`, oldest first, each with its `type`, `createdAt`, and the reason Mailgun supplied as `detail` when present (e.g. a bounce or complaint reason).

An invitation with no invite-scoped email job SHALL return an empty `deliveryLog` array (never null). The history SHALL be loaded in batched queries for the whole project, not one round trip per invitation. The delivery log SHALL NOT alter the invitation's own `status`.

Because it is presented as a durable history, invite-scoped `kb.email_jobs` rows and their `kb.email_logs` events SHALL be exempt from the generic terminal email-job retention purge: a terminal invite send older than the retention window SHALL NOT be deleted, even though every other email-job source type still is. This keeps an invitation's `deliveryLog` complete for the invitation's lifetime instead of silently emptying after the retention window.

#### Scenario: Every send is listed with its delivery events
- **WHEN** an invitation has been sent once and then resent, and Mailgun events have been recorded for those sends
- **THEN** the invitation's `deliveryLog` contains one entry per send, newest first, each with its `status` and its recorded delivery events

#### Scenario: Bounce detail is surfaced
- **WHEN** a send's Mailgun `bounced` event carries a `reason`
- **THEN** that event appears in the send's `events` with `type = "bounced"` and the reason in `detail`

#### Scenario: No send yields an empty log
- **WHEN** an invitation has no matching `source_type='invite'` email job
- **THEN** the invitation is returned with an empty (non-null) `deliveryLog`

#### Scenario: Log is scoped to the project
- **WHEN** invitations in two different projects each have email jobs
- **THEN** each project's invitations report only their own sends

#### Scenario: History survives the generic email-job retention window
- **WHEN** a terminal invite-scoped email job is older than the configured email-job retention period
- **THEN** the job and its `kb.email_logs` events are retained, and the invitation's `deliveryLog` still reports that send
