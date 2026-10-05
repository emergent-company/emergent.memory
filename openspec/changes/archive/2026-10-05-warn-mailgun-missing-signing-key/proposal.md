## Why

When outbound email uses the Mailgun transport, `NewSender` selects the Mailgun
sender whenever the domain and API key are set (`cfg.IsConfigured()`), but it
never checks `MailgunSigningKey`. A missing signing key means the public
delivery-status webhook fails closed — every Mailgun event is rejected with
`401` and `kb.email_logs`/`delivery_status` are silently never updated. The
misconfiguration is invisible at startup, so operators only notice after bounce
and complaint tracking has already been lost.

## What Changes

- Log a `WARN` at startup when the Mailgun sender is selected but
  `MAILGUN_SIGNING_KEY` is unset, making the silent fail-closed delivery-tracking
  failure visible.
- Document `MAILGUN_SIGNING_KEY` in the email setup guide and correct the
  "Delivery tracking" section to describe the signed webhook.

## Capabilities

### Modified Capabilities

- `email-delivery-status`: add a startup-visibility requirement — the server SHALL
  log a warning when the Mailgun transport is in use without a signing key.

## Impact

- `apps/server/domain/email/module.go`: warn in `NewSender` when
  `MailgunSigningKey` is empty in the Mailgun branch.
- `apps/server/domain/email/smtp_test.go`: test covering warn/no-warn.
- `docs/site/developer-guide/email-setup.md`: env-var table + delivery-tracking
  rewrite.
