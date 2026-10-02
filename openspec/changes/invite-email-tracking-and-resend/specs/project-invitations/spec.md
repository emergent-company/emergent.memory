## Purpose

Defines the invitation flow for adding new members to an organization or project, including invitation creation, email delivery, listing, acceptance, persistence, decline, revocation, and resending.

## MODIFIED Requirements

### Requirement: List invitations for a project
The system SHALL provide a `GET /api/projects/:projectId/invites` endpoint (authentication required) that returns all invitations for a project, ordered by creation time. The system SHALL authorize the caller against the `:projectId` path parameter (the shared project membership pair): a caller who is not a member of the project's owning organization SHALL receive HTTP 403, and a caller addressing an unknown project SHALL receive HTTP 404.

Each returned invitation SHALL include the delivery state of its most recent invitation email, resolved from `kb.email_jobs` where `source_type = 'invite'` and `source_id` equals the invitation id: `deliveryStatus` (the job's `delivery_status`, or null when no mapped event has arrived) and `deliveryStatusAt` (the job's `delivery_status_at`). An invitation with no matching email job SHALL report a null `deliveryStatus`. The delivery state SHALL NOT alter the invitation's own `status`.

#### Scenario: Project invitations listed
- **WHEN** an authenticated user requests invitations for a project
- **THEN** the server returns each invitation with its email, role, status, creation time, and its email `deliveryStatus`

#### Scenario: Delivery state reflects the invite email job
- **WHEN** an invitation's most recent email job has `delivery_status = 'opened'`
- **THEN** that invitation is returned with `deliveryStatus = 'opened'` and a populated `deliveryStatusAt`

#### Scenario: No email job yields null delivery state
- **WHEN** an invitation has no matching `source_type='invite'` email job, or its job has received no mapped delivery event
- **THEN** the invitation is returned with a null `deliveryStatus`

## ADDED Requirements

### Requirement: Resend an invitation
The system SHALL provide a `POST /api/invites/:id/resend` endpoint (authentication required) that re-enqueues the invitation email for a pending invitation. The resend SHALL keep the invitation's existing token, set `expires_at` to 7 days from the time of resend, and enqueue a `project-invitation` email job with `source_type='invite'` and `source_id` set to the invitation id, using the same template data as creation.

The system SHALL require the caller to be an `org_admin` of the invitation's organization (or an active `superadmin_full`) before resending — resending is an org-tier write, matching revocation. A caller who is not an `org_admin` SHALL receive HTTP 403. An unknown invitation, or an invitation that is not pending, SHALL return HTTP 404 and SHALL NOT enqueue an email. A failure to enqueue the email SHALL NOT fail the request (the invitation remains valid), matching creation.

Resend SHALL be server-side idempotent over a fixed guard window: if the invitation already has a **live** invite-scoped email job (`source_type='invite'`, `source_id` equal to the invitation id) created within the guard window — one that is still in flight or already delivered (job status not `failed`/`dead_letter`, and delivery status not `bounced`/`soft_bounced`/`complained`/`failed`) — the request SHALL be a no-op: it SHALL NOT enqueue another email job, SHALL return HTTP 200 with the existing invitation, and SHALL NOT error. A recent job in a terminal FAILURE state SHALL NOT suppress the resend, so an admin recovering a bounced, soft-bounced, or complained invitation SHALL be able to resend immediately; the guard window SHALL prevent accidental duplicate sends for the success case but SHALL NOT lock out error recovery. The guard window SHALL be a single named constant, `inviteResendGuardWindow` (60 seconds). The guard SHALL be keyed per invitation: a recent resend of one invitation SHALL NOT block resending a different invitation. This server-enforced guard SHALL be the guarantee against duplicate invitation emails; a client-side confirmation dialog SHALL NOT be relied upon. The guard SHALL be atomic under concurrency: concurrent resends for the same invitation SHALL be serialised so that exactly one email job is enqueued regardless of interleaving (not merely a best-effort check-then-act).

Every resend response SHALL report what the request actually did, so a caller (and the UI) can distinguish a real send from a no-op instead of assuming every HTTP 200 sent an email. The response body SHALL carry an `outcome` field: `sent` when a fresh email job was enqueued, `noop` when the guard suppressed the request, and `enqueue_failed` when a send was attempted but the enqueue failed (the request still succeeds and the invitation remains valid). The `outcome` SHALL be the truthful signal; HTTP 200 alone SHALL NOT be read as "an email was sent".

#### Scenario: Pending invitation resent
- **WHEN** an `org_admin` of the invitation's organization resends a pending invitation with no live invite-scoped email job
- **THEN** a `project-invitation` email job is enqueued for the invitation, the invitation keeps its token, its expiry is extended to 7 days from now, the server returns success, and the response `outcome` is `sent`

#### Scenario: Repeat resend within the guard window is a no-op
- **WHEN** an `org_admin` resends a pending invitation that already has a live (in-flight or delivered) invite-scoped email job created within the last 60 seconds
- **THEN** no second email job is enqueued, the existing invitation is returned with HTTP 200, no error is surfaced, and the response `outcome` is `noop`

#### Scenario: Resend after a bounced invite works within the guard window
- **WHEN** an `org_admin` resends a pending invitation whose most recent invite-scoped email job is a terminal failure (job `failed`/`dead_letter`, or a `bounced`/`soft_bounced`/`complained`/`failed` delivery status) created within the last 60 seconds
- **THEN** a fresh `project-invitation` email job is enqueued and the response `outcome` is `sent` (the guard does not lock out bounce recovery)

#### Scenario: Resend past the guard window enqueues again
- **WHEN** an `org_admin` resends a pending invitation whose only invite-scoped email job was created more than 60 seconds ago
- **THEN** a fresh `project-invitation` email job is enqueued and the response `outcome` is `sent`

#### Scenario: Resend outcome distinguishes a real send from a no-op
- **WHEN** a resend is suppressed by the guard
- **THEN** the response reports `outcome = "noop"`, so the caller does not present the suppressed request as a successful send

#### Scenario: Guard is keyed per invitation
- **WHEN** an `org_admin` resends invitation A and then immediately resends a different invitation B
- **THEN** invitation B's resend is not blocked by invitation A's recent job, and an email job is enqueued for B

#### Scenario: Concurrent resends enqueue exactly one job
- **WHEN** multiple resends for the same pending invitation race concurrently
- **THEN** exactly one `project-invitation` email job is enqueued for that invitation, and every request returns success

#### Scenario: Non-admin cannot resend
- **WHEN** a plain member of the invitation's organization attempts to resend a pending invitation
- **THEN** the server responds with HTTP 403 and no email job is enqueued

#### Scenario: Already-processed invitation cannot be resent
- **WHEN** a user attempts to resend an invitation whose status is not `pending`
- **THEN** the server responds with HTTP 404, the invitation is unchanged, and no email job is enqueued

#### Scenario: Unknown invitation cannot be resent
- **WHEN** a user attempts to resend a non-existent invitation
- **THEN** the server responds with HTTP 404 and no email job is enqueued
