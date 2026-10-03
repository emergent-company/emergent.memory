## ADDED Requirements

### Requirement: Invitation email delivery log

For each invitation returned by `GET /api/projects/:projectId/invites`, the system SHALL include the invitation's full email send history as a `deliveryLog` array, newest send first. Each entry SHALL describe one invite-scoped `kb.email_jobs` row (`source_type = 'invite'` and `source_id` equal to the invitation id), carrying:

- `jobId` and `createdAt` — the identity and enqueue time of that send;
- `status` — the email job's processing status (`pending`, `processing`, `sent`, `failed`, `dead_letter`);
- `deliveryStatus` / `deliveryStatusAt` — the job's latest Mailgun delivery state, when one has arrived;
- `lastError` — the job's recorded failure, when any;
- `events` — the Mailgun delivery events recorded against that job in `kb.email_logs`, oldest first, each with its `type`, `createdAt`, and the reason Mailgun supplied as `detail` when present (e.g. a bounce or complaint reason).

An invitation with no invite-scoped email job SHALL return an empty `deliveryLog` array (never null). The history SHALL be loaded in batched queries for the whole project, not one round trip per invitation. The delivery log SHALL NOT alter the invitation's own `status`.

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
