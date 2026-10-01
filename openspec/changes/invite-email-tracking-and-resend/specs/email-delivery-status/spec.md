## Purpose

Define how Mailgun delivery events reach the server, are verified, reconciled to the originating email job, are reflected for mapped event types (including opens and clicks), and are enabled on outbound mail so those events fire.

## MODIFIED Requirements

### Requirement: Delivery events are reconciled to the email job

The server SHALL map each event to its originating `kb.email_jobs` row by
matching the event `message.headers.message-id` against `mailgun_message_id`,
normalising surrounding angle brackets. It SHALL record every accepted event in
`kb.email_logs` and SHALL update `delivery_status`, `delivery_status_at`, and
`delivery_status_synced_at` on the matched job for mapped event types. The mapped
event types SHALL include `delivered`, `opened`, `clicked`, `complained`,
`unsubscribed`, `bounced`, `failed` (with `severity=temporary` mapped to
`soft_bounced` and any other severity mapped to `bounced`), and `rejected` or
`dropped` (both mapped to `failed`).

#### Scenario: Bounce updates the job
- **WHEN** a signed `failed` event with `severity=permanent` arrives for a message id that matches a sent job
- **THEN** that job's `delivery_status` becomes `bounced`, `delivery_status_at` is the event time, `delivery_status_synced_at` is set, and a `kb.email_logs` row is written

#### Scenario: Open updates the job
- **WHEN** a signed `opened` event arrives for a matched job
- **THEN** that job's `delivery_status` becomes `opened`

#### Scenario: Click updates the job
- **WHEN** a signed `clicked` event arrives for a matched job
- **THEN** that job's `delivery_status` becomes `clicked`

#### Scenario: Complaint updates the job
- **WHEN** a signed `complained` event arrives for a matched job
- **THEN** that job's `delivery_status` becomes `complained`

#### Scenario: Unmatched event is still audited
- **WHEN** a signed event arrives whose message id matches no job
- **THEN** the event is recorded in `kb.email_logs` with a null job id and no job is updated

#### Scenario: Unmapped event type
- **WHEN** a signed event has a type with no delivery-status mapping
- **THEN** it is recorded in `kb.email_logs` and no `delivery_status` is written

## ADDED Requirements

### Requirement: Open and click tracking are requested on outbound mail

The server SHALL request open and click tracking on every message it sends through the Mailgun sender, so that `opened` and `clicked` events are emitted for recipients whose tracking configuration permits. Tracking SHALL be requested per message (via the Mailgun `o:tracking`, `o:tracking-opens`, and `o:tracking-clicks` parameters or the SDK equivalent), so the behaviour does not depend on domain-level configuration alone. Enabling tracking SHALL NOT change whether the message is delivered.

#### Scenario: Outbound message requests tracking
- **WHEN** the Mailgun sender sends a message
- **THEN** the request enables open tracking and click tracking for that message

#### Scenario: Tracking does not gate delivery
- **WHEN** Mailgun accepts a message sent with tracking enabled
- **THEN** the send succeeds and the job records a Mailgun message id as before
