## ADDED Requirements

### Requirement: Browse and install registry servers from the MCP Servers surface

The gateway SHALL provide a registry page, reachable from the MCP Servers
surface, that searches the official MCP registry through Memory and lists
matching entries with their name, description, and whether they offer a remote
transport, a package, or both. For an entry that offers a remote transport the
page SHALL offer an install action that creates a project MCP server via
Memory's remote-only install and reports the required environment variables.
For a stdio-only entry (no remote transport) the page SHALL NOT offer an install
action; it SHALL instead explain that the server must be installed locally
through the macOS connector. A registry or install failure SHALL surface a clear
error and SHALL NOT leave a partially-created server or a broken page.

#### Scenario: Search the registry

- **WHEN** a user opens the registry page and submits a search
- **THEN** matching entries are listed with their descriptions and transport availability

#### Scenario: Install a remote entry

- **WHEN** a user installs an entry that offers a remote transport
- **THEN** the project MCP server is created, the user is returned to the list with a success message, and the required environment variables are named

#### Scenario: Stdio-only entry shows connector guidance

- **WHEN** a user views an entry that offers only stdio packages
- **THEN** the page explains that the server is installed locally via the macOS connector and offers no server-side install action

#### Scenario: Registry failure

- **WHEN** the registry search or an install request fails
- **THEN** the page shows a clear error message and does not render a broken or partially-installed state
