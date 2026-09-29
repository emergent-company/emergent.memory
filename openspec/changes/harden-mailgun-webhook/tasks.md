## 1. Replay hardening (#1222)

- [x] 1.1 Add `MailgunWebhookTolerance time.Duration` (`MAILGUN_WEBHOOK_TOLERANCE`, default `5m`) to `internal/config.EmailConfig`, mirror it on `domain/email.Config`, and document it in `.env.example`.
- [x] 1.2 Fail-first tests: stale timestamp rejected 401; fresh timestamp accepted; replayed token rejected 401; same token re-paired with a swapped body rejected 401 and leaves the job state unchanged. Add a unit test for the token store (consume twice, expiry).
- [x] 1.3 Implement the timestamp tolerance check and the in-process TTL single-use token store in `domain/email/mailgun_webhook.go`; keep the HMAC check first and fail-closed.
- [x] 1.4 DB-backed end-to-end: fresh signed delivery applies once; replaying the same envelope is rejected; a body swap on the replayed token is rejected.
- [x] 1.5 Verify `go build ./...`, `task lint`, `REQUIRE_DB=1 go test ./domain/email/...` on a throwaway Postgres.

## 2. Rate limiting (#1223)

- [x] 2.1 Add `MAILGUN_WEBHOOK_RATE_PER_MIN` / `MAILGUN_WEBHOOK_RATE_BURST` / `MAILGUN_WEBHOOK_GLOBAL_RATE_PER_MIN` / `MAILGUN_WEBHOOK_GLOBAL_RATE_BURST` to config; document in `.env.example`.
- [x] 2.2 Fail-first tests: per-IP limiter returns 429 after the burst; normal (under-limit) traffic is unaffected (401, not 429); global backstop triggers across differing IPs.
- [x] 2.3 Implement the per-IP + global limiter and a route middleware in `domain/email/mailgun_webhook.go`; register with the route before the handler.
- [x] 2.4 Document the WAF/proxy layering in the code and spec.

## 3. Expression index (#1221)

- [x] 3.1 Add `migrations/00203_email_jobs_mailgun_message_id_btrim_index.sql` with `CREATE INDEX CONCURRENTLY IF NOT EXISTS ... (btrim(mailgun_message_id, '<>'))`, `-- +goose NO TRANSACTION`, and a `DROP INDEX CONCURRENTLY IF EXISTS` down.
- [x] 3.2 Verify the expression matches `delivery_store.go` character-for-character.
- [x] 3.3 Run the migration-number and migration-order guards; re-derive the version vs `origin/main` and open PRs immediately before pushing.

## 4. Spec + verification

- [x] 4.1 Delta spec adds replay-hardening, rate-limiting, and expression-index requirements to `email-delivery-status`.
- [x] 4.2 `openspec validate harden-mailgun-webhook --strict` passes.
- [x] 4.3 `go build ./...`, `task lint`, `gofmt -l` clean, `REQUIRE_DB=1` email tests pass.
- [x] 4.4 Confirm the 4 known pre-existing `openspec validate --all --strict` failures are unchanged.
