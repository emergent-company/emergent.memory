## 1. Server — Mailgun API base override

- [x] 1.1 Add `MAILGUN_API_BASE` (`MailgunAPIBase`, envDefault "") to `EmailConfig` in `apps/server/internal/config/config.go`
- [x] 1.2 Mirror `MailgunAPIBase` on `domain/email.Config` + `NewConfig` in `apps/server/domain/email/config.go`
- [x] 1.3 In `NewMailgunSender` (`apps/server/domain/email/mailgun.go`), apply `client.SetAPIBase(cfg.MailgunAPIBase)` when non-empty, with precedence over the `MailgunRegion=="eu"` default; unchanged when unset
- [x] 1.4 Unit test proving the override is applied, wins over the EU default, and that unset preserves the previous US/EU defaults (`apps/server/domain/email/mailgun_test.go`)

## 2. e2e — Mailgun stub

- [x] 2.1 Add `e2e/stubs/mailgun/` (stdlib-only `main.go`, `Dockerfile`, `go.mod`): `POST /v3/{domain}/messages` multipart capture (from/to/subject/text/html/Authorization presence) + `{"id","message"}` response, `GET /captured` JSON array, `GET /healthz`
- [x] 2.2 Unit test the stub handler (`e2e/stubs/mailgun/main_test.go`): healthz, capture + read-back, empty `/captured` is `[]`, unknown route 404, domain extraction

## 3. e2e — compose + test

- [x] 3.1 Add `mailgun-stub` service (built from `e2e/stubs/mailgun`) to `e2e/docker-compose.yml`, published on 8080, with healthcheck
- [x] 3.2 Parametrize `test-emergent-server` email env (`E2E_EMAIL_TRANSPORT` / `E2E_SMTP_HOST` / `E2E_MAILGUN_*`) so default resolves to today's SMTP/Mailpit values
- [x] 3.3 Pass `MAILGUN_STUB_URL` to the test client (empty by default so the new test skips under SMTP)
- [x] 3.4 Add `e2e/tests/api/invite_email_mailgun_test.go`: skip unless `MAILGUN_STUB_URL` set + stub reachable; create invite; poll `/captured`; assert recipient, non-empty from, subject mentions invite, accept URL with token, path `/v3/{domain}/messages`, Authorization present

## 4. CI

- [x] 4.1 Add a `mailgun` job to `.github/workflows/e2e.yml` running the stack with `E2E_EMAIL_TRANSPORT=mailgun`, `E2E_MAILGUN_DOMAIN=test.example.com`, `E2E_MAILGUN_API_KEY=test-key`, `E2E_MAILGUN_API_BASE=http://mailgun-stub:8080/v3`, `E2E_MAILGUN_REGION=us`, `E2E_MAILGUN_STUB_URL=http://mailgun-stub:8080`, and running only the new test
- [x] 4.2 Keep the existing `integration` and `api` jobs unchanged

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./... && go test ./domain/email/... ./internal/config/...`
- [x] 5.2 `cd e2e && go build ./... && go vet ./...`
- [x] 5.3 `cd e2e/stubs/mailgun && go test ./...`
- [x] 5.4 `docker compose -f e2e/docker-compose.yml config` shows the default SMTP values and the corrected Mailgun override resolution
- [x] 5.5 `openspec validate add-mailgun-transport-e2e`
