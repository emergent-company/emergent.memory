## Context

Mailgun can deliver events to a webhook (`bounced`, `complained`, `failed`,
`delivered`, …). The server currently records only the Mailgun message id at
send time (`email_jobs.mailgun_message_id`) and never reconciles later events.
`domain/email` is a push worker: `Worker.processJob` renders a template and calls
`Sender.Send`, then `JobsService.MarkSent` writes status + message id.

The delivery columns and the `idx_email_jobs_needs_status_sync` index already
exist in the baseline schema (`00001_baseline.sql`), so no new job columns are
needed — only ingestion and a dedupe index.

## Goals / Non-Goals

**Goals:**
- A single public webhook that verifies Mailgun's HMAC signature and fails
  closed on anything unverified.
- Idempotent ingestion: a Mailgun retry (or duplicated delivery) must not apply
  the event twice or regress delivery state.
- Populate `delivery_status` / `delivery_status_at` / `delivery_status_synced_at`
  so the existing superadmin API reports real state.

**Non-Goals:**
- A polling fallback (`GetEventsForMessage`) — the issue lists it as optional
  and it duplicates the webhook's mapping logic; left as a follow-up.
- Tracking opens/clicks (they would churn the status); only terminal delivery
  outcomes are mapped.
- A UI. There is no email-jobs page in `apps/web-ui`; adding one is a separate,
  larger change (stated as a follow-up).

## Webhook contract

Endpoint: `POST /api/webhooks/mailgun` (public; the signature is the auth).

Payload (Mailgun v3):

```json
{
  "signature": { "timestamp": "1700000000", "token": "abc", "signature": "hex" },
  "event-data": {
    "event": "failed",
    "id": "event-unique-id",
    "timestamp": 1700000000.12,
    "severity": "permanent",
    "recipient": "user@example.com",
    "reason": "mailbox unavailable",
    "message": { "headers": { "message-id": "20260101...@domain" } }
  }
}
```

The endpoint also accepts a JSON array of events (Mailgun can batch), treating
each independently.

## Signature verification

Mailgun signs `HMAC-SHA256(signing_key, timestamp + token)` and sends the hex
digest in `signature.signature`. The handler:

1. Rejects when `MAILGUN_SIGNING_KEY` is unset (fail closed — no key, no trust).
2. Rejects when any of `timestamp`, `token`, `signature` is empty.
3. Recomputes the digest and compares with `hmac.Equal` (constant time).
4. Rejects with `401` on mismatch. No middleware/auth bypass exists.

A replay-window check on `timestamp` is deliberately **not** applied: Mailgun
retries are spread over hours, and Mailgun does not specify a tolerance, so a
window would drop legitimate retried events. Dedupe (below) makes replays
harmless because the event id is stable.

## Event → status mapping

| Mailgun `event` | `severity` | `delivery_status` |
|---|---|---|
| `delivered` | — | `delivered` |
| `failed` | `temporary` | `soft_bounced` |
| `failed` | `permanent` / empty | `bounced` |
| `bounced` (legacy) | — | `bounced` |
| `rejected` / `dropped` | — | `failed` |
| `complained` | — | `complained` |
| `unsubscribed` | — | `unsubscribed` |

Unmapped events are still recorded in `kb.email_logs` (audit) but do not change
`delivery_status`.

## Mapping event → email job

`mailgun_message_id` is stored exactly as returned by the Mailgun SDK — the send
response `id`, which carries surrounding angle brackets (`<…@domain>`), while the
webhook `message.headers.message-id` is bracket-less. The store matches on
`btrim(mailgun_message_id, '<>') = btrim(?, '<>')` (email volume is low, so
forgoing the plain `mailgun_message_id` index for correctness is acceptable).

## Idempotency

The dedupe key is the Mailgun event id (`event-data.id`), falling back to a
deterministic SHA-256 of `message-id|event|timestamp|recipient` when Mailgun
omits it. Migration `00200` adds a unique partial index
`kb.email_logs (mailgun_event_id) WHERE mailgun_event_id IS NOT NULL`.

Ingestion runs in one transaction:

1. `INSERT INTO kb.email_logs (…) ON CONFLICT (mailgun_event_id) DO NOTHING
   RETURNING id`. Zero rows ⇒ duplicate ⇒ roll back the no-op and return
   `applied=false` (handler still answers `200`, so Mailgun stops retrying).
2. On a fresh insert, update the matched job, but only when the incoming event
   is newer: `delivery_status_at IS NULL OR delivery_status_at <= to_timestamp(event.ts)`.
   This keeps out-of-order retries from regressing state.

## Surfacing

`GET /api/superadmin/email-jobs` already selects and serialises
`DeliveryStatus` / `DeliveryStatusAt`; populating the columns makes the existing
superadmin DTO report real state with no API-shape change.

## Risks / Trade-offs

- The webhook is only useful when `MAILGUN_SIGNING_KEY` is configured; without
  it every request is rejected (fail closed), which is the intended posture.
- No rate limiting on the public endpoint beyond signature verification — an
  unauthenticated flood is rejected at the HMAC check but still costs a hash;
  upstream rate limiting / WAF is the appropriate control.
- `config.go` and `module.go` in `domain/email` are shared with the concurrent
  #1214 lane; edits there are additive and kept to the minimum hunks.
