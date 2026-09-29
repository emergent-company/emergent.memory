## ADDED Requirements

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
