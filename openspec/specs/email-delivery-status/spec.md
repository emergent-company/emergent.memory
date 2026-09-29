# email-delivery-status Specification

## Purpose
Define how Mailgun delivery events (bounces, complaints, failures, deliveries)
reach the server, are verified, are reconciled to the originating email job, and
are exposed to operators.

## Requirements

### Requirement: Mailgun webhook endpoint

The server SHALL expose a public `POST /api/webhooks/mailgun` endpoint that
accepts Mailgun v3 delivery-event payloads (a single `event-data` object or a
JSON array of them). The endpoint SHALL NOT require session authentication; the
Mailgun signature is the authentication.

#### Scenario: Endpoint is public
- **WHEN** an unauthenticated client POSTs a validly signed event payload
- **THEN** the server processes it and responds `200`

### Requirement: Signature verification fails closed

The server SHALL verify every webhook request with
`HMAC-SHA256(MAILGUN_SIGNING_KEY, timestamp + token)` and compare the hex digest
to `signature.signature` in constant time. The server SHALL reject the request
with `401` when the signing key is unconfigured, when any signature field is
missing, or when the digest does not match. No request may be ingested without a
valid signature.

#### Scenario: Valid signature accepted
- **WHEN** the request carries a correct HMAC of `timestamp + token` under the configured signing key
- **THEN** the server accepts and processes the event

#### Scenario: Invalid signature rejected
- **WHEN** the digest does not match, or the body was altered after signing
- **THEN** the server responds `401` and ingests nothing

#### Scenario: Missing signature rejected
- **WHEN** the signature object, timestamp, token, or signing key is absent
- **THEN** the server responds `401` and ingests nothing

### Requirement: Delivery events are reconciled to the email job

The server SHALL map each event to its originating `kb.email_jobs` row by
matching the event `message.headers.message-id` against `mailgun_message_id`,
normalising surrounding angle brackets. It SHALL record every accepted event in
`kb.email_logs` and SHALL update `delivery_status`, `delivery_status_at`, and
`delivery_status_synced_at` on the matched job for mapped event types.

#### Scenario: Bounce updates the job
- **WHEN** a signed `failed` event with `severity=permanent` arrives for a message id that matches a sent job
- **THEN** that job's `delivery_status` becomes `bounced`, `delivery_status_at` is the event time, `delivery_status_synced_at` is set, and a `kb.email_logs` row is written

#### Scenario: Complaint updates the job
- **WHEN** a signed `complained` event arrives for a matched job
- **THEN** that job's `delivery_status` becomes `complained`

#### Scenario: Unmatched event is still audited
- **WHEN** a signed event arrives whose message id matches no job
- **THEN** the event is recorded in `kb.email_logs` with a null job id and no job is updated

#### Scenario: Unmapped event type
- **WHEN** a signed event has a type with no delivery-status mapping
- **THEN** it is recorded in `kb.email_logs` and no `delivery_status` is written

### Requirement: Ingestion is idempotent

The server SHALL deduplicate events by Mailgun event id (falling back to a
deterministic synthetic id when Mailgun omits one) via a unique index on
`kb.email_logs.mailgun_event_id`. Re-delivering the same event SHALL NOT write a
second `kb.email_logs` row and SHALL NOT re-apply its job update; the server
SHALL still respond `200` so Mailgun stops retrying.

#### Scenario: Retry does not double-apply
- **WHEN** the same signed event (same id) is delivered twice
- **THEN** exactly one `kb.email_logs` row exists for that event id and the job's delivery state reflects a single application

#### Scenario: Out-of-order event does not regress state
- **WHEN** an older event is ingested after a newer one for the same job
- **THEN** the job retains the newer `delivery_status` and `delivery_status_at`

### Requirement: Delivery state is surfaced to operators

The server SHALL expose the reconciled delivery state through the existing
superadmin email-jobs API (`GET /api/superadmin/email-jobs`), which SHALL report
non-null `deliveryStatus` and `deliveryStatusAt` for jobs that have received a
mapped event.

#### Scenario: API reflects the reconciled status
- **WHEN** a job has received a mapped bounce event and is listed via the superadmin API
- **THEN** its `deliveryStatus` is `bounced` and `deliveryStatusAt` is populated

### Requirement: Webhook signatures expire outside a tolerance window

The server SHALL reject a webhook request whose `signature.timestamp` is more
than a configured tolerance (default 5 minutes) before or after the server's
current time. The check SHALL run after HMAC verification and before any event
is ingested, and SHALL respond `401`. The tolerance SHALL be configurable via
`MAILGUN_WEBHOOK_TOLERANCE` and the rejection SHALL be indistinguishable from an
invalid signature.

#### Scenario: Stale timestamp rejected

- **WHEN** a correctly signed envelope carries a `timestamp` older than the tolerance window
- **THEN** the server responds `401` and ingests nothing

#### Scenario: Fresh timestamp accepted

- **WHEN** a correctly signed envelope carries a `timestamp` within the tolerance window
- **THEN** the server proceeds to signature-token and ingestion checks

#### Scenario: Future-skewed timestamp bounded

- **WHEN** a correctly signed envelope carries a `timestamp` slightly ahead of the server clock but within the tolerance
- **THEN** the server accepts it

### Requirement: Webhook signature tokens are single-use

The server SHALL record each accepted signature `token` and reject any
subsequent request that reuses it within the tolerance window, responding `401`.
The record SHALL be short-lived (expiring at the end of the tolerance window) and
bounded so an unauthenticated flood cannot grow it without limit. The store SHALL
be an in-process best-effort replay defense; durable event-id idempotency in
`kb.email_logs` remains the guarantee that an event is applied at most once.

#### Scenario: Replayed token rejected

- **WHEN** a request reuses the `timestamp` and `token` of an already-processed envelope
- **THEN** the server responds `401` and ingests nothing

#### Scenario: Body swapped onto a replayed token rejected

- **WHEN** a captured viable `timestamp`/`token` pair is re-sent with a different body after its first use
- **THEN** the server responds `401`, and the matched job's delivery state is unchanged

#### Scenario: Token expires

- **WHEN** the tolerance window has elapsed
- **THEN** the token is no longer retained and the store does not grow unbounded

### Requirement: The public webhook is rate limited

The server SHALL apply per-client-IP and global rate limits to
`POST /api/webhooks/mailgun` before signature verification, responding `429` when
a limit is exceeded. Limits SHALL be configurable
(`MAILGUN_WEBHOOK_RATE_PER_MIN`, `MAILGUN_WEBHOOK_RATE_BURST`,
`MAILGUN_WEBHOOK_GLOBAL_RATE_PER_MIN`, `MAILGUN_WEBHOOK_GLOBAL_RATE_BURST`) with
modest defaults. Because `X-Forwarded-For` is only trustworthy behind a proxy,
the global limiter SHALL act as a backstop that a spoofed per-IP key cannot
bypass. The upstream WAF/proxy SHALL remain the coarse first layer of defence.

#### Scenario: Flood from one IP is shed

- **WHEN** one client IP exceeds its burst within the window
- **THEN** the server responds `429` without running signature verification or DB work for the excess

#### Scenario: Normal traffic is unaffected

- **WHEN** requests arrive within the configured limits
- **THEN** they are processed normally (no `429`)

#### Scenario: Global backstop

- **WHEN** requests arrive from many distinct client keys exceeding the global limit
- **THEN** the server responds `429` once the global limit is exceeded

### Requirement: Job mapping lookup is index-supported

The job-mapping query
`SELECT id FROM kb.email_jobs WHERE btrim(mailgun_message_id, '<>') = ? LIMIT 1`
SHALL be served by an expression index on
`kb.email_jobs (btrim(mailgun_message_id, '<>'))` matching the expression
exactly, built concurrently so adding it does not lock the table.

#### Scenario: Index matches the query expression

- **WHEN** the webhook resolves an event to its originating job
- **THEN** the query's `btrim(mailgun_message_id, '<>')` predicate can be served by `idx_email_jobs_mailgun_message_id_btrim`
