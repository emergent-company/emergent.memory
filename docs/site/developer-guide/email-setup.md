# Email Setup

Memory sends transactional emails for events such as org invitations and workspace notifications. Email is delivered via [Mailgun](https://mailgun.com) using a background job queue with automatic retries and delivery tracking.

## Overview

The email subsystem is entirely background-based — there are no REST endpoints. Other parts of the server (e.g. the invitations domain) enqueue email jobs, and a polling worker processes them asynchronously.

```
Application code
      │ enqueue EmailJob
      ▼
kb.email_jobs (Postgres queue)
      │
      ▼
Email worker (polls every ~5s)
      │ renders Handlebars template
      ▼
Mailgun API
      │
      ▼
Recipient inbox
```

---

## Configuration

Set the following environment variables on the server:

| Variable | Required | Description |
|---|---|---|
| `EMAIL_ENABLED` | Yes | Set to `true` to enable email sending |
| `MAILGUN_DOMAIN` | Yes | Your Mailgun sending domain, e.g. `mg.example.com` |
| `MAILGUN_API_KEY` | Yes | Mailgun private API key |
| `EMAIL_FROM_ADDRESS` | Yes | Sender email address, e.g. `noreply@example.com` |
| `EMAIL_FROM_NAME` | No | Sender display name, e.g. `Memory Platform` |
| `EMAIL_TEMPLATE_DIR` | No | Path to Handlebars email templates (defaults to `templates/email`) |

When `MAILGUN_DOMAIN` or `MAILGUN_API_KEY` are absent or empty, the email system falls back to a no-op sender that logs all emails without sending them. This is the default in development.

---

## Verifying email is configured

Check the server startup logs for:

```
INFO  email: configured (domain=mg.example.com, from=noreply@example.com)
```

Or the no-op fallback:

```
WARN  email: not configured — using no-op sender (emails will be logged only)
```

---

## Email job lifecycle

Each job transitions through these statuses:

| Status | Description |
|---|---|
| `pending` | Waiting to be processed |
| `processing` | Worker has picked it up |
| `sent` | Successfully delivered to Mailgun |
| `failed` | Send failed; will retry |
| `dead_letter` | Max attempts reached; will not retry |

Failed jobs use exponential backoff starting from 60 seconds. After 3 attempts (configurable), the job moves to `dead_letter`.

---

## Delivery tracking

After sending, Mailgun delivery events are polled and stored:

| Delivery status | Description |
|---|---|
| `pending` | Email accepted by Mailgun, not yet delivered |
| `delivered` | Delivered to recipient mail server |
| `opened` | Recipient opened the email |
| `clicked` | Recipient clicked a link |
| `bounced` | Hard bounce (invalid address) |
| `soft_bounced` | Temporary delivery failure |
| `complained` | Marked as spam by recipient |
| `unsubscribed` | Recipient unsubscribed |
| `failed` | Delivery definitively failed |

---

## Email templates

Templates are Handlebars (`.hbs`) files in the template directory. Each email type maps to a template name. Template variables are passed as `templateData` when the job is enqueued by application code.

Example template structure:

```
templates/email/
  invite.hbs
  workspace-ready.hbs
  password-reset.hbs
```

---

## Email job entity reference

**`EmailJob`** — table `kb.email_jobs`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `templateName` | string | Handlebars template to render |
| `toEmail` | string | Recipient email address |
| `toName` | string | Recipient display name (optional) |
| `subject` | string | Email subject line |
| `templateData` | object | Key-value data passed to the template |
| `status` | string | See lifecycle table above |
| `attempts` | int | Number of send attempts |
| `maxAttempts` | int | Max retries (default 3) |
| `lastError` | string | Last failure message |
| `mailgunMessageId` | string | Mailgun message ID on success |
| `deliveryStatus` | string | Mailgun delivery event status |
| `sourceType` | string | Originating domain label, e.g. `invite` |
| `sourceId` | UUID | Originating record ID |
| `createdAt` | timestamp | |
| `processedAt` | timestamp | When the job was finalized |
| `nextRetryAt` | timestamp | Earliest time to retry |

---

## Worker configuration

The email worker polls for pending jobs every 5 seconds and processes up to 10 jobs per batch. These values are tuneable via server configuration but cannot be changed at runtime without a restart.

| Setting | Default | Description |
|---|---|---|
| `workerIntervalMs` | 5000 | Polling interval in milliseconds |
| `workerBatchSize` | 10 | Jobs dequeued per poll tick |
| `maxRetries` | 3 | Max send attempts per job |
| `retryDelaySec` | 60 | Base retry delay (exponential backoff) |
