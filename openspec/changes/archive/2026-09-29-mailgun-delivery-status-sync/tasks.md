## 1. Config + persistence

- [x] 1.1 Add `MailgunSigningKey` (`MAILGUN_SIGNING_KEY`) to `internal/config.EmailConfig` and `domain/email.Config`
- [x] 1.2 Migration `00200_email_logs_event_dedup_index.sql`: unique partial index on `kb.email_logs (mailgun_event_id)` (`NO TRANSACTION` / `CONCURRENTLY`)
- [x] 1.3 `delivery_store.go`: idempotent `IngestDeliveryEvent` (email_logs insert + job status update in one tx)

## 2. Webhook

- [x] 2.1 `mailgun_webhook.go`: `VerifyMailgunSignature` (HMAC-SHA256 constant-time, fail-closed on missing key/fields)
- [x] 2.2 `MailgunWebhookHandler` parsing single/array `event-data`, mapping status, recording events
- [x] 2.3 `RegisterWebhookRoutes(e, h)` wired via `email.Module`; regenerate `route-authority.yaml`

## 3. Tests (TDD)

- [x] 3.1 Signature tests: valid accepted; invalid rejected; missing signature/token/timestamp/key rejected
- [x] 3.2 Handler tests: valid event → 200; bad signature → 401
- [x] 3.3 DB-backed idempotency test: same event id twice applies once; duplicate does not regress a newer status
- [x] 3.4 DB-backed mapping test: bounce/complaint → `delivery_status` + `email_logs` row

## 4. Verification

- [x] 4.1 `gofmt -l` clean, `go build ./...`, `task lint`
- [x] 4.2 DB-backed tests under a throwaway Postgres (`REQUIRE_DB=1`)
- [x] 4.3 `openspec validate --all --strict`; migration-number + migration-order guards
