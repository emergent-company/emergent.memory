## Why

Invitations already enqueue email jobs and send via Mailgun, but there is no end-to-end test that proves the invite email is actually delivered with a working accept link. Mailgun does not store message bodies by default, and the e2e stack has no way to receive or assert on email. We need a deterministic, offline way to verify the full "invite → email delivered → link correct" path in CI.

## What Changes

- Add an SMTP transport to the server email package (alongside Mailgun), selectable via `EMAIL_TRANSPORT`.
- Add a Mailpit container to the e2e docker-compose stack and point the test server's email at it.
- Add an e2e test that creates an invite, receives the real email in Mailpit, and asserts subject / from / to and that the accept URL matches the invitation token.

## Capabilities

### New Capabilities

- `email-transport`: SMTP email transport and transport selection alongside the existing Mailgun sender.

### Modified Capabilities

<!-- none -->

## Impact

- `apps/server/domain/email`: new `smtp.go` sender, `Transport` + SMTP fields on `Config`, transport branch in `NewSender`.
- `apps/server/internal/config/config.go`: `EmailConfig` gains `EMAIL_TRANSPORT`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`.
- `e2e/docker-compose.yml`: `mailpit` service + server email env vars.
- `e2e/tests/api/invite_email_test.go`: new test + Mailpit polling helper.
