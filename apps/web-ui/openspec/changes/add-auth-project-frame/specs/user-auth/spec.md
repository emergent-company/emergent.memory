## Purpose

Lets a user sign in to the web console with their Emergent Memory account and establishes a session that scopes all subsequent Memory access to that user.

## ADDED Requirements

### Requirement: Zitadel sign-in

The gateway SHALL redirect an unauthenticated browser request to the Zitadel authorization endpoint and, on successful authentication, SHALL establish a session for the user.

#### Scenario: Unauthenticated browser visit

- **WHEN** a browser requests a protected page without a session
- **THEN** the gateway redirects to the Zitadel sign-in

#### Scenario: Successful sign-in

- **WHEN** a user completes sign-in and the gateway validates the authorization code
- **THEN** the gateway establishes a session and serves the requested page

#### Scenario: Failed sign-in

- **WHEN** Zitadel returns an error or the gateway cannot validate the authorization code
- **THEN** the gateway does not establish a session and shows an error

### Requirement: Session-scoped Memory access

The gateway SHALL hold the signed-in user's Memory token server-side and MUST NOT expose it to the browser; all Memory calls made on behalf of that session SHALL use the user's token.

#### Scenario: Memory calls use the session token

- **WHEN** a signed-in user performs any action that reads or writes Memory
- **THEN** the gateway calls Memory with the user's bearer token, never a shared server token

#### Scenario: Token never exposed to the browser

- **WHEN** the gateway serves any page or API response to the browser
- **THEN** no Memory token appears in the response, HTML, or client-visible headers

### Requirement: Sign out

The gateway SHALL let a signed-in user end their session.

#### Scenario: User signs out

- **WHEN** a signed-in user signs out
- **THEN** the gateway clears the session and subsequent protected requests redirect to sign-in

### Requirement: API authentication

The `/api` endpoints SHALL accept either a valid session or a valid `X-API-Key`, so the web UI and existing programmatic clients both work.

#### Scenario: API call with session

- **WHEN** a signed-in browser calls an API endpoint
- **THEN** the gateway serves it with the session's token and active project

#### Scenario: API call with API key

- **WHEN** a client calls an API endpoint with a valid `X-API-Key` and no session
- **THEN** the gateway serves it using the server's shared Memory credentials
