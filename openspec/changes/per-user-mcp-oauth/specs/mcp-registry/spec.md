## MODIFIED Requirements

### Requirement: Installing an OAuth-protected entry surfaces the authorization step

When a registry entry is installed as a remote MCP server and the server's tool
discovery is rejected with an OAuth challenge, the install result SHALL state
that the server requires authorization and point at the Connect action on the MCP
servers page, so an install that produces no tools is never silent. The install
SHALL still succeed: creating the server row is not reverted because
authorization has not happened yet.

#### Scenario: OAuth-protected entry installed

- **WHEN** a registry entry whose remote requires OAuth is installed
- **THEN** the server is created, the install result reports that authorization is required, and the MCP servers page offers Connect for it

#### Scenario: Ordinary entry installed

- **WHEN** a registry entry whose remote needs no authorization is installed
- **THEN** the install result is the existing success message with its discovered tools

## ADDED Requirements

### Requirement: The server list reports the caller's authorization state

The per-server authorization summary attached to the MCP servers list SHALL
reflect the requesting user's connection state: a personal credential the user
holds, or the project-wide shared credential, resolved the same way the proxy
resolves a bearer. A server on which only another member has a personal
credential SHALL NOT appear connected to the requesting user.

#### Scenario: Caller's personal credential is reported

- **WHEN** the requesting user has a personal OAuth credential for a server
- **THEN** the list summary reports the server connected to that user

#### Scenario: Shared credential is reported

- **WHEN** a server has only the shared credential and no personal credential for the requesting user
- **THEN** the list summary reports the server connected

#### Scenario: Another member's credential is not reported

- **WHEN** only another member has a personal credential for a server
- **THEN** the list summary for the requesting user does not report the server connected
