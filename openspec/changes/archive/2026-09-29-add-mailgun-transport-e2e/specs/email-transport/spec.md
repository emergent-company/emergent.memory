## ADDED Requirements

### Requirement: Mailgun API base override

The system SHALL allow overriding the Mailgun API base URL via the
`MAILGUN_API_BASE` environment variable. When set, the sender SHALL call the
Mailgun SDK's `SetAPIBase` with the configured value, taking precedence over the
EU region default. When `MAILGUN_API_BASE` is unset, the sender SHALL preserve
the existing behaviour: the EU base when `MAILGUN_REGION=eu`, otherwise the SDK
default (US). The value SHALL be treated as the complete base URL, including any
`/v3` path segment, matching the Mailgun SDK's `SetAPIBase` contract.

#### Scenario: Override applied

- **WHEN** `MAILGUN_API_BASE` is set and Mailgun is otherwise configured
- **THEN** the Mailgun sender sends to `POST {MAILGUN_API_BASE}/{domain}/messages`

#### Scenario: Override beats EU region default

- **WHEN** `MAILGUN_API_BASE` is set and `MAILGUN_REGION=eu`
- **THEN** the sender uses `MAILGUN_API_BASE`, not the EU base

#### Scenario: Unset preserves region behaviour

- **WHEN** `MAILGUN_API_BASE` is unset and `MAILGUN_REGION=eu`
- **THEN** the sender uses the EU base; the US/SDK default is used otherwise
