## Context

Mailgun's v3 HMAC is computed over `timestamp + token` only, so it provides
authentication of those two values but not integrity of the event body. #1219
made verification fail-closed and deduped exact retries by event id. The
remaining documented mitigations are a timestamp tolerance window and
single-use tokens (#1222), rate limiting the public route (#1223), and an
expression index for the job-mapping lookup (#1221).

## Decisions

### Timestamp window

Reject when `|now - signature.timestamp| > tolerance`. The tolerance is
`MAILGUN_WEBHOOK_TOLERANCE` (default `5m`); non-numeric or missing timestamps
were already rejected by the HMAC path, and a numeric-but-stale value is now
rejected the same way (HTTP 401) so the two failure modes are not
distinguishable to a caller. Clock skew is bounded by taking the absolute
difference, so a slightly-future timestamp (Mailgun sends `time.Now()`) is
accepted.

### Single-use token store: in-process, not the DB

Tokens are recorded in a small in-process, TTL-bounded map keyed by the token
value, with the entry expiring at the end of the tolerance window. Rationale:

- The webhook is public; #1223 specifically bounds per-request work, so adding a
  DB write on every request to a *different* table would work against that goal.
- The deployment runs a single server process (compose service); a replay
  usually lands on the same process, and the durable event-id idempotency in
  `kb.email_logs` still guarantees the event is applied once even if a replay
  reaches a second process.
- The store is bounded (max entries, lazy expiry sweep, oldest-first eviction)
  so an unauthenticated flood cannot grow it without bound; the rate limiter
  caps the fill rate further.

This is a deliberate trade-off, documented in the spec: the token store is a
best-effort replay defense, not a durable cross-instance log. A shared cache
would be the upgrade if the server is ever horizontally scaled.

### Body-hash binding — not adopted

The HMAC does not authenticate the body, so a "body swap" cannot be detected by
hashing the body into the replay key; the single-use token is what makes a
*replayed* envelope unusable. An attacker who races the legitimate Mailgun
delivery and sends a forged body first is not preventable under Mailgun's
signing scheme, so no body-hash binding is added.

### Rate limiting: per-IP + global

`c.RealIP()` is used for the per-IP key, matching the rest of the server. Because
`X-Forwarded-For` is only trustworthy behind a proxy that overwrites it, a global
backstop limiter is added alongside the per-IP one so a spoofed header cannot
fully bypass the cap. The upstream WAF/proxy stays the coarse first layer; the
in-process limiters are defence in depth and bound per-request cost.

### Expression index (#1221)

The mapping query is
`SELECT id FROM kb.email_jobs WHERE btrim(mailgun_message_id, '<>') = ? LIMIT 1`.
The index is created with exactly `(btrim(mailgun_message_id, '<>'))` so the
planner can use it. It is built `CONCURRENTLY` under `NO TRANSACTION` with
`IF NOT EXISTS` and a matching `DROP INDEX CONCURRENTLY IF EXISTS` down, per the
`00200`/`00202` convention. At current email volume the table scan is cheap, but
the index is low-cost, write-overhead-light on a low-write table, and removes the
scan as volume grows. If email volume had stayed negligible the index would be
premature; the hardening PR is the natural place to add it.
