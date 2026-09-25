## Purpose

Defines how the server selects and uses an outbound email transport, including an SMTP transport used to deliver transactional email (such as project invitations) to a capture service in test environments.

## ADDED Requirements

### Requirement: Transport selection
The system SHALL select the outbound email transport from the `EMAIL_TRANSPORT` environment variable, supporting `mailgun` (default) and `smtp`. When email sending is disabled (`EMAIL_ENABLED=false`), the system SHALL use a no-op sender regardless of transport.

#### Scenario: SMTP transport selected
- **WHEN** `EMAIL_ENABLED=true` and `EMAIL_TRANSPORT=smtp`
- **THEN** the server uses the SMTP sender

#### Scenario: Mailgun transport default
- **WHEN** `EMAIL_ENABLED=true`, `EMAIL_TRANSPORT` is unset or `mailgun`, and Mailgun domain and API key are configured
- **THEN** the server uses the Mailgun sender

#### Scenario: Email disabled
- **WHEN** `EMAIL_ENABLED=false`
- **THEN** the server uses the no-op sender regardless of transport

### Requirement: SMTP delivery
The system SHALL deliver email over SMTP using `SMTP_HOST`, `SMTP_PORT`, optional `SMTP_USERNAME`/`SMTP_PASSWORD` authentication, and `SMTP_TLS` (one of `none`, `starttls`, `tls`). The message SHALL be sent as MIME `multipart/alternative` containing `text/plain` and `text/html` parts, and SHALL include a generated `Message-ID` header.

#### Scenario: Send over plain SMTP
- **WHEN** an email job is sent via the SMTP sender with `SMTP_TLS=none` and no credentials
- **THEN** the SMTP server receives a `multipart/alternative` message containing the text and HTML bodies

#### Scenario: Authenticated SMTP
- **WHEN** `SMTP_USERNAME` and `SMTP_PASSWORD` are set
- **THEN** the sender authenticates before delivering the message

#### Scenario: Plain-text part carries actionable content
- **WHEN** an email is delivered over SMTP
- **THEN** the `text/plain` part SHALL carry the same actionable content as the HTML part, including any accept link and CLI install instructions, so plain-text clients and test captures receive the full message

### Requirement: SMTP hardening
The system SHALL reject an unrecognised `SMTP_TLS` value (fail closed) rather than silently treating it as plaintext, and SHALL bound the entire SMTP exchange by a deadline so a host that accepts the connection and then goes silent cannot stall a worker goroutine indefinitely.

#### Scenario: Unknown TLS mode rejected
- **WHEN** `SMTP_TLS` is set to a value other than `none`, `starttls`, or `tls`
- **THEN** the sender reports a configuration error and does not send over plaintext

#### Scenario: Silent server bounded by deadline
- **WHEN** the SMTP server accepts the connection but stops responding
- **THEN** the sender aborts once the send deadline is reached instead of blocking a worker goroutine indefinitely
