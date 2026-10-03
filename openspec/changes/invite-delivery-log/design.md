## Context

The invite-email data model is already complete (see
`invite-email-tracking-and-resend`): `kb.invites` is the invitation,
`kb.email_jobs` holds one row per send attempt (`source_type='invite'`,
`source_id=<invite.id>`), and `kb.email_logs` holds Mailgun delivery events
linked to a job via `kb.email_logs.email_job_id`, with the raw Mailgun
event-data JSON in `details` (including `reason` and `severity`). The existing
`ListByProject` LATERAL join resolves only the most recent job's
`delivery_status`.

## Goals / Non-Goals

**Goals**

- Expose the full per-invitation send history and its delivery events through
  the existing project invitations API, without a new endpoint or per-row round
  trip.
- Surface it in the members UI on hover and by keyboard, reusing the go-daisy
  popover component.
- Do not invent data: render exactly the jobs/events memory stores.

**Non-Goals**

- No resend/retry changes (owned by `invite-email-tracking-and-resend`).
- No new database columns or migrations.
- No streaming/live refresh of the log; it is loaded with the page.

## Decisions

### Decision: extend `ListByProject` rather than add an endpoint

The members page already fetches the project's invitations in one call. Adding a
second authenticated endpoint just for the hover content would add an auth
surface and a network round trip on hover for data that is small and already
scoped to the page's query. Instead `ListByProject` attaches `deliveryLog` per
invitation, loaded with two batched queries for the whole project (jobs, then
their `kb.email_logs` events) — still no per-invitation round trip. The
alternative (an htmx hover fetch) was rejected as more moving parts for no data
benefit.

### Decision: reuse the go-daisy popover with a hover trigger

`ui.Popover` with `PopoverTriggerHover` already provides positioning, clickaway,
Escape, exclusivity, and `aria-expanded` bookkeeping; the trigger is a real
`<button>`, so it is keyboard-focusable and the runtime's click handling opens
the content on Enter/Space. This satisfies the "hover content also reachable by
focus/keyboard" requirement without new JS. A hand-rolled tooltip was explicitly
ruled out by the task.

### Decision: newest send first, events oldest first

The log is a timeline: sends are listed newest first (matching the rest of the
list ordering), while each send's Mailgun events read oldest first so the story
of a bounce follows delivery order.

### Decision: report the processed time, not the enqueue time

Each send carries both `createdAt` (when the job was enqueued) and `processedAt`
(when `JobsService.MarkSent` / dead-letter finished it). The log renders
`processedAt` when set, falling back to `createdAt` for a still-pending send, so
a send delayed by queueing or retries is not reported as having gone out earlier
than it did.

### Decision: exempt invite sends from the email-job retention purge

The log is presented as durable history, but `EmbeddingJobPurgeTask` deletes
terminal `kb.email_jobs` after `EMBEDDING_JOB_RETENTION_DAYS` (default 7) and
`kb.email_logs` cascades on that delete — which would silently empty an older
invitation's log and render the misleading "No emails sent yet." state.
Invite-scoped jobs are therefore excluded from the generic purge (invite volume
is small and bounded by invitations); the retention window still applies to every
other email-job source type.

## Risks / Trade-offs

- **Payload size**: a project with many invitations and many resends returns
  more JSON. Acceptable: the members list is admin-scoped and paginated by
  project; the extra data is bounded by send count.
- **Invalid HTML nesting**: the popover trigger is a `<button>`; the row's
  identity markup uses only phrasing `<span>` children inside it to stay valid.
