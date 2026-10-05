# Tasks

## 1. Startup ping retry

- [x] Add `DB_PING_MAX_ATTEMPTS` / `DB_PING_INITIAL_BACKOFF` / `DB_PING_MAX_BACKOFF` to `DatabaseConfig`.
- [x] Extract `pingWithRetry` and use it in `NewPgxPool` before failing.
- [x] Unit-test retry success, exhaustion, attempt clamping and context-cancel abort.
- [x] Mutation-check the retry loop.

## 2. Bulk statement timeout

- [x] Add `DB_BULK_STATEMENT_TIMEOUT` to `DatabaseConfig`.
- [x] Add `WithStatementTimeout` / `statementTimeoutFromContext` and apply `set_config('statement_timeout', …, true)` in `BeginSafeTx`.
- [x] Attach the timeout to bulk endpoint worker contexts via `Repository.BulkContext`.
- [x] Unit-test context round-trip, `set_config` emission, and the no-op path.

## 3. Self-hosted + dead env

- [x] Add `restart: unless-stopped` to the self-hosted `server` service.
- [x] Remove every `DB_AUTOINIT` reference (compose, env examples, installer, docs).

## 4. Verification

- [x] `go build ./...` and `go test ./internal/database/... ./internal/config/... ./domain/graph/...`.
- [x] `bash apps/server/scripts/lint-ratchet.sh`.
- [x] `bash scripts/preflight/migration-number.sh`.
- [x] YAML parses for every compose file touched.
