## ADDED Requirements

### Requirement: Missing signing key is visible at startup

The server SHALL log a startup `WARN` when outbound email uses the Mailgun
transport but `MAILGUN_SIGNING_KEY` is unset, so that the fail-closed
delivery-tracking failure is visible rather than silent.

#### Scenario: Mailgun configured without a signing key

- **WHEN** the server starts with `EMAIL_ENABLED=true`, `EMAIL_TRANSPORT=mailgun`,
  `MAILGUN_DOMAIN` and `MAILGUN_API_KEY` set, but `MAILGUN_SIGNING_KEY` empty
- **THEN** the server selects the Mailgun sender and logs a `WARN` stating the
  signing key is unset and that delivery status will not update

#### Scenario: Mailgun configured with a signing key

- **WHEN** the server starts with the Mailgun transport and a non-empty
  `MAILGUN_SIGNING_KEY`
- **THEN** the server does not log the missing-signing-key warning
