## MODIFIED Requirements

### Requirement: Authenticated access

The API SHALL require authentication — a valid web session OR a valid API key — and MUST NOT expose memories to unauthenticated callers.

#### Scenario: Unauthenticated request

- **WHEN** a client calls the API without a valid session or API key
- **THEN** the API returns an unauthorized response and no memory content

#### Scenario: Session-authenticated request

- **WHEN** a signed-in web client calls the API with a valid session
- **THEN** the API serves the request using the session's Memory token and active project

#### Scenario: API-key-authenticated request

- **WHEN** a programmatic client calls the API with a valid API key
- **THEN** the API serves the request using the server's shared Memory credentials
