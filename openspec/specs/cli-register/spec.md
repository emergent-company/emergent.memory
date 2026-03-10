## ADDED Requirements

### Requirement: User can register an account via CLI

The CLI SHALL provide an `memory register` command that guides a new user through account creation using the OAuth 2.0 Device Authorization Grant flow against Zitadel, saves credentials locally, and confirms server-side account provisioning.

#### Scenario: Successful registration flow

- **WHEN** a user runs `memory register`
- **AND** a server URL is configured
- **AND** the server is not in standalone mode
- **THEN** the CLI SHALL print introductory text explaining that the user will be redirected to create an account
- **AND** the CLI SHALL initiate the OAuth Device Authorization Grant flow
- **AND** the CLI SHALL display a verification URL and user code
- **AND** the CLI SHALL attempt to open the URL in the default browser
- **AND** the CLI SHALL poll for the token until the user completes the Zitadel flow
- **AND** on token receipt the CLI SHALL call `GET /api/auth/me` with the access token
- **AND** the CLI SHALL save credentials to `~/.emergent/credentials.json`
- **AND** the CLI SHALL print a success message including the confirmed user email

#### Scenario: Register distinguishes first-time vs returning user in messaging

- **WHEN** `memory register` completes successfully
- **THEN** the success output SHALL inform the user that if they already had an account they are now logged in
- **AND** the output SHALL include the user's email address as confirmed by the server

#### Scenario: Register fails when no server URL is configured

- **WHEN** a user runs `memory register`
- **AND** no server URL is in the config
- **THEN** the CLI SHALL exit with a non-zero status code
- **AND** the CLI SHALL print an error message directing the user to configure a server URL

#### Scenario: Register fails gracefully in standalone mode

- **WHEN** a user runs `memory register`
- **AND** the server health endpoint indicates `mode: standalone`
- **THEN** the CLI SHALL exit with a non-zero status code
- **AND** the CLI SHALL print a message explaining that registration is not available in standalone mode
- **AND** the CLI SHALL suggest running `memory login` or using the API key method instead

#### Scenario: Register times out waiting for user authorization

- **WHEN** a user runs `memory register`
- **AND** the user does not complete the Zitadel flow within the device code expiry window
- **THEN** the CLI SHALL exit with a non-zero status code
- **AND** the CLI SHALL print a timeout error message

#### Scenario: Account confirmation after device flow

- **WHEN** the device authorization flow completes and a token is obtained
- **AND** `GET /api/auth/me` returns HTTP 200
- **THEN** the CLI SHALL display the user's `user_id` and `email` from the response
- **AND** the CLI SHALL indicate that the account is active on the server

#### Scenario: Account confirmation fails

- **WHEN** the device authorization flow completes and a token is obtained
- **AND** `GET /api/auth/me` returns a non-200 status
- **THEN** the CLI SHALL still save the credentials locally
- **AND** the CLI SHALL print a warning that account confirmation failed
- **AND** the CLI SHALL suggest running `memory status` to check authentication state

### Requirement: CLI E2E test for account confirmation

The test suite SHALL include an E2E test that verifies `GET /api/auth/me` returns a valid user profile when called with a known test token, exercising the account-confirmation step used by `memory register`.

#### Scenario: Auth me returns user info for test token

- **WHEN** `GET /api/auth/me` is called with the `e2e-test-user` static token in the `Authorization: Bearer` header
- **THEN** the response SHALL be HTTP 200
- **AND** the response body SHALL contain a non-empty `user_id`
- **AND** the response body SHALL contain a `type` field

#### Scenario: Auth me returns 401 without a token

- **WHEN** `GET /api/auth/me` is called without an `Authorization` header
- **THEN** the response SHALL be HTTP 401
