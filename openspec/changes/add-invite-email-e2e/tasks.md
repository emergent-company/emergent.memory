## 1. Server — SMTP transport

- [ ] 1.1 Add `EmailConfig` fields (`EMAIL_TRANSPORT`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`) in `apps/server/internal/config/config.go`
- [ ] 1.2 Mirror the fields on `domain/email.Config` + `NewConfig` in `apps/server/domain/email/config.go`
- [ ] 1.3 Add `SMTPSender` in `apps/server/domain/email/smtp.go` (stdlib `net/smtp`): MIME `multipart/alternative`, TLS modes, optional auth, generated `Message-ID`
- [ ] 1.4 Add transport branch in `NewSender` (`apps/server/domain/email/module.go`): `smtp` → `SMTPSender`, default → existing Mailgun/no-op
- [ ] 1.5 Unit tests: `NewSender` selection (smtp / mailgun / disabled) and MIME builder output

## 2. e2e — Mailpit + test

- [ ] 2.1 Add `mailpit` service to `e2e/docker-compose.yml` (SMTP 1025 + HTTP API 8025, published)
- [ ] 2.2 Point `test-emergent-server` email at Mailpit (`EMAIL_ENABLED`, `EMAIL_TRANSPORT=smtp`, `SMTP_HOST`, `SMTP_PORT`, faster worker interval)
- [ ] 2.3 Add `MAILPIT_BASE_URL` env to the client container
- [ ] 2.4 Add `e2e/tests/api/invite_email_test.go`: create invite → poll Mailpit for the message → assert subject/from/to + accept URL matches the invite token

## 3. Verification

- [ ] 3.1 `go build ./...` + `go test ./...` in `apps/server/`
- [ ] 3.2 `go build ./...` + `go vet ./...` in `e2e/`
- [ ] 3.3 Run the invite-email e2e test against the compose stack (docker available)
- [ ] 3.4 `openspec validate add-invite-email-e2e`
