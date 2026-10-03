## ADDED Requirements

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
