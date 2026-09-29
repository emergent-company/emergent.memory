## Why

`kb.email_jobs` already carries `delivery_status`, `delivery_status_at`, and
`delivery_status_synced_at`, and `mailgun_message_id` is persisted on every
successful send (`apps/server/domain/email/jobs.go` `MarkSent`). Nothing ever
populates the delivery columns: there is no Mailgun webhook handler, and
`MailgunSender.GetEventsForMessage` has no non-test caller. As a result bounces,
complaints, and delivery failures are invisible, and
`GET /api/superadmin/email-jobs` reports `deliveryStatus = null` for every row,
so operators cannot tell whether a message was delivered, bounced, or dropped
(issue #1215; related #941).

## What Changes

- Add a **public** Mailgun webhook endpoint
  `POST /api/webhooks/mailgun` that verifies the Mailgun HTTP signature
  (HMAC-SHA256 over `timestamp + token` with the signing key) and rejects
  unverified requests fail-closed.
- Map each delivery event back to its originating email job via
  `mailgun_message_id` (angle brackets normalised) and persist the event in
  `kb.email_logs` and the job's delivery state.
- Make ingestion **idempotent** with a unique index on
  `kb.email_logs.mailgun_event_id`, so Mailgun retries cannot double-apply.
- Surface the now-populated delivery state through the existing
  `GET /api/superadmin/email-jobs` DTO.
- Add the `MAILGUN_SIGNING_KEY` configuration (documented in `.env.example`).

## Capabilities

### New Capabilities

- `email-delivery-status`: Mailgun delivery-event ingestion (webhook + signature
  verification + idempotent persistence) and delivery-state surfacing.

### Modified Capabilities

<!-- none -->

## Impact

- `apps/server/domain/email`: new `mailgun_webhook.go` (handler + signature
  verifier) and `delivery_store.go` (idempotent ingestion), `mailgun_webhook_test.go`
  and `delivery_store_test.go`; `Config`/`module.go` wiring (additive).
- `apps/server/internal/config/config.go`: `EmailConfig.MailgunSigningKey`.
- `apps/server/migrations/00200_email_logs_event_dedup_index.sql`: unique
  partial index on `kb.email_logs (mailgun_event_id)`.
- `apps/server/route-authority.yaml`: the new public route.
- `.env.example`: `MAILGUN_SIGNING_KEY` placeholder.
- Out of scope / follow-up: a superadmin UI column for delivery state (no
  email-jobs UI page exists in `apps/web-ui` today) and a polling fallback
  worker for domains where webhooks cannot be configured (tracked in issue
  text as optional).
