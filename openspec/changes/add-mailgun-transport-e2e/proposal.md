## Why

Invitation emails are sent through the Mailgun transport in production, but the
existing e2e coverage only exercises the SMTP transport against Mailpit. Mailgun
cannot be reached from CI (real credentials, external network, no body storage),
so the Mailgun code path — transport selection, request construction, and the
accept link it carries — has no end-to-end proof.

## What Changes

- Add a `MAILGUN_API_BASE` override to the email config and apply it in
  `NewMailgunSender` with precedence over the `MAILGUN_REGION=eu` default, so the
  Mailgun SDK can be pointed at a local stub.
- Add a stdlib-only Mailgun stub service (`e2e/stubs/mailgun`) that captures
  `POST /v3/{domain}/messages` multipart requests and serves them back over
  `GET /captured`.
- Parametrize the e2e compose email env with `E2E_*` overrides so the same stack
  runs in Mailgun mode, while the no-override default stays exactly SMTP/Mailpit.
- Add `e2e/tests/api/invite_email_mailgun_test.go`: create an invite, wait for the
  captured Mailgun request, assert recipient / from / subject / accept URL /
  request path / Authorization presence.
- Add a CI job running the stack in Mailgun mode and executing the new test
  (existing SMTP job unchanged).

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `email-transport`: adds an override for the Mailgun API base URL (used by the
  e2e stub) on top of the existing transport selection.

## Impact

- `apps/server/internal/config/config.go`: `EmailConfig` gains `MAILGUN_API_BASE`.
- `apps/server/domain/email/config.go`: `Config` mirrors the field.
- `apps/server/domain/email/mailgun.go`: `NewMailgunSender` applies the override
  with precedence over the EU region default.
- `e2e/stubs/mailgun/`: new stub service.
- `e2e/docker-compose.yml`: new `mailgun-stub` service + `E2E_*` email overrides.
- `e2e/tests/api/invite_email_mailgun_test.go`: new Mailgun-transport e2e test.
- `.github/workflows/e2e.yml`: new `mailgun` job.
