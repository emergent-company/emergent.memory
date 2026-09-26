# connector-core Specification

## Purpose
Defines cross-connector session lifecycle guarantees: the stored OIDC refresh token is preserved across session import, and the session is retained on transient refresh failures.

## Requirements

### Requirement: Preserve the stored refresh token across session import

`auth import` SHALL preserve an existing stored refresh token when the payload omits
`refresh_token`, instead of overwriting the session without it. A non-empty payload
`refresh_token` SHALL replace the stored one. The CLI SHALL NOT print refresh tokens.

#### Scenario: Import without a refresh token keeps the stored one

- **WHEN** a refreshable session is stored for the server and `auth import` receives a payload without `refresh_token`
- **THEN** the stored refresh token is retained and the session remains refreshable

#### Scenario: Import with a refresh token replaces it

- **WHEN** the payload carries a non-empty `refresh_token`
- **THEN** the stored session is updated with the new value

#### Scenario: Import with no stored session

- **WHEN** no session is stored and the payload carries only an access token
- **THEN** the import succeeds and stores a session with no refresh token

### Requirement: Retain the session on transient refresh failures

The connector SHALL clear the stored session only when the token endpoint rejects the
refresh token as an authentication error (an OAuth error such as `invalid_grant` /
`invalid_token`, or HTTP 400/401). Network, timeout, response-parsing, and 5xx failures
SHALL leave the stored session and refresh token intact and SHALL surface the failure to the
caller.

#### Scenario: Transient failure keeps the session

- **WHEN** a refresh fails because the token endpoint is unreachable, times out, or returns a 5xx
- **THEN** the stored session and refresh token are retained and the command reports the failure

#### Scenario: Rejected refresh clears the session

- **WHEN** the token endpoint rejects the refresh token with `invalid_grant`
- **THEN** the stored session is cleared so the user is prompted to sign in again
