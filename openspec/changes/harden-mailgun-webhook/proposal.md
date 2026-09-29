## Why

The Mailgun v3 webhook signature is `HMAC-SHA256(signing_key, timestamp + token)`
— it binds only the timestamp and token, **not the request body**. Signature
verification (#1219) is fail-closed, but without a freshness check and a
single-use token store a captured viable envelope can be replayed, and the
captured `timestamp+token` can be re-paired with a **forged body** to poison a
message's delivery status (arbitrary bounce/complaint/delivered events).

The endpoint is also public with no rate limiting, so an unauthenticated flood
forces HMAC verification and DB work per request, and the `email_jobs` mapping
query (`btrim(mailgun_message_id, '<>') = ?`) cannot use the plain index on
`mailgun_message_id`.

## What Changes

- **Replay hardening (#1222):** reject a signature whose `timestamp` is outside
  a configurable tolerance window (default 5 minutes, `MAILGUN_WEBHOOK_TOLERANCE`),
  and record each signature `token` as single-use in a short-TTL in-process
  store so a replayed token is rejected. The existing fail-closed HMAC
  verification is unchanged in strength and runs first.
- **Rate limiting (#1223):** add a per-client-IP limiter plus a global backstop
  on `POST /api/webhooks/mailgun` (`MAILGUN_WEBHOOK_RATE_PER_MIN` /
  `_RATE_BURST` / `_GLOBAL_RATE_PER_MIN` / `_GLOBAL_RATE_BURST`). The limiter
  runs before signature verification so a flood is shed cheaply; the upstream
  WAF/proxy remains the coarse first layer.
- **Expression index (#1221):** add
  `idx_email_jobs_mailgun_message_id_btrim` on
  `kb.email_jobs (btrim(mailgun_message_id, '<>'))` matching the mapping query
  character-for-character, built `CONCURRENTLY` under `NO TRANSACTION`.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `email-delivery-status`: add replay-hardening (timestamp window + single-use
  token) and rate-limiting requirements to the public webhook, and require the
  job-mapping lookup to be served by the expression index.

## Impact

**Files (`apps/server/`):**
- `domain/email/mailgun_webhook.go` — timestamp window + token replay guard +
  rate-limit middleware; route wiring.
- `domain/email/config.go` — new webhook config fields + defaults.
- `domain/email/mailgun_webhook_test.go`, `domain/email/delivery_store_test.go`
  — replay/staleness/rate-limit tests.
- `internal/config/config.go` — env-backed knobs.
- `migrations/00203_email_jobs_mailgun_message_id_btrim_index.sql` — expression
  index.
- `.env.example` — documented placeholders.

## Non-Goals

- No change to signature verification strength or the DB event-id idempotency
  guarantee; the token store is a lightweight in-process replay defense, not a
  durable cross-instance log.
- No body-hash binding into the dedupe key: the HMAC does not authenticate the
  body, so hashing it adds no cryptographic assurance beyond the single-use
  token and freshness window.
- No distributed/shared-cache rate limiter; the upstream WAF owns coarse
  protection.
