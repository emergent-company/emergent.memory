## ADDED Requirements

### Requirement: An installed server that requires authorization says so

When an installed or registered remote MCP server requires OAuth authorization,
the MCP servers page SHALL explain that authorization is required and offer a
Connect action, instead of presenting the server as merely unreachable. The
authorization state SHALL be discoverable from the server row — through the
inspect result and through the server's stored authorization status — and a row
with a stored credential SHALL show that it is connected together with a
Disconnect action.

#### Scenario: Inspect reports an authorization requirement

- **WHEN** the user inspects a server whose connection is rejected with an OAuth challenge
- **THEN** the inspect panel states that authorization is required and shows a Connect action

#### Scenario: Connect starts the browser flow

- **WHEN** the user activates Connect for a server that requires authorization
- **THEN** the browser is sent to the authorization server and returns to the MCP servers page afterwards

#### Scenario: Connected state is visible

- **WHEN** a server has a stored OAuth credential
- **THEN** the row shows a connected state with a Disconnect action

#### Scenario: Callback outcome is reported

- **WHEN** the browser returns from the authorization server
- **THEN** the page reports whether the connection succeeded, with an error indication otherwise

#### Scenario: Disconnect

- **WHEN** the user disconnects a connected server
- **THEN** the stored credential is removed and the row returns to its unconnected state

### Requirement: Static credential configuration stays available

A server that accepts a static API token header SHALL remain configurable through
the existing secret header form; the authorization flow SHALL be an additional
path, not a replacement, so a user can choose either means of authentication.

#### Scenario: Token header still configurable

- **WHEN** the user configures a secret header value for a server that also supports OAuth
- **THEN** the header value is stored and used exactly as before, without starting an authorization flow
