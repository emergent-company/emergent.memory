## ADDED Requirements

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
