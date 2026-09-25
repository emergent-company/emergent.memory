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
