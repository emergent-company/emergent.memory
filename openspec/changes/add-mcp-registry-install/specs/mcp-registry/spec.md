## Purpose

Lets a user browse the official Model Context Protocol registry through Memory
and install an entry as an MCP server — into a Memory project for remote
servers, or as a locally-hosted server on the user's machine (through the
connector) for stdio servers.

## ADDED Requirements

### Requirement: Browse the official MCP registry through Memory

Memory SHALL expose the official MCP registry through its authenticated,
project-scoped admin API: a search endpoint and a per-server lookup. Each
returned entry SHALL include its registry name, title, description, version,
repository, packages (registry type, name/identifier, version, transport),
remotes (type, URL), and the environment variables and headers it declares
(name, required, secret). Registry failures SHALL surface as clear errors and
SHALL NOT be reported as a successful empty result.

#### Scenario: Search returns matching entries

- **WHEN** a client searches the registry with a query
- **THEN** entries whose names match are returned with their package/remote summary and pagination cursor

#### Scenario: Lookup returns full detail

- **WHEN** a client requests a registry entry by its name (including a name containing a `/`)
- **THEN** the full entry is returned, including packages, remotes, and declared environment variables/headers

#### Scenario: Unknown entry

- **WHEN** a client requests a registry name that does not exist
- **THEN** the API reports a not-found error rather than an empty entry

#### Scenario: Upstream failure is surfaced

- **WHEN** the official registry is unreachable or returns an error
- **THEN** the API reports the failure clearly and does not return a successful empty result

### Requirement: Install a registry entry into a Memory project (remote only)

Memory SHALL install a registry entry into a project only when the entry offers
a remote transport, preferring `streamable-http` over `sse`, creating a project
MCP server with the remote URL and placeholder values for its declared
environment variables and headers. An entry that offers only stdio packages
SHALL be rejected with a clear, actionable error that names the packages and
SHALL NOT create a server. Memory SHALL NOT install stdio packages, because
they would spawn arbitrary processes on the shared server host.

#### Scenario: Install a remote entry

- **WHEN** a user installs a registry entry that offers a remote transport
- **THEN** a project MCP server is created for that URL with the entry's declared environment variables and headers as unset placeholders, and the required-variable names are reported back

#### Scenario: Stdio-only entry is rejected

- **WHEN** a user attempts to install a registry entry that offers only stdio packages
- **THEN** no server is created and the error names the package types and points to local installation instead

### Requirement: The connector proxies registry browse from Memory

The connector SHALL expose registry browse through its loopback management API,
backed by Memory: a search endpoint and a per-server lookup. It SHALL resolve
these using the connector's configured project credentials and SHALL return
Memory's registry data, or a clear error when Memory cannot be reached or no
project connection is configured. The connector SHALL NOT maintain its own
registry index or integrate the official registry directly.

#### Scenario: Proxy a search

- **WHEN** a client searches the connector's registry endpoint
- **THEN** the connector returns Memory's registry search results for that query

#### Scenario: Proxy a lookup for a slash-containing name

- **WHEN** a client requests a registry entry whose name contains a `/`
- **THEN** the connector escapes the name correctly and returns the entry from Memory

#### Scenario: Memory unreachable

- **WHEN** Memory cannot be reached
- **THEN** the connector returns a clear error and does not fabricate results

#### Scenario: No project connection

- **WHEN** the connector has no configured project credentials
- **THEN** the registry endpoints report that the registry is unavailable rather than failing opaquely

### Requirement: The connector suggests a local server config from a registry entry

The connector SHALL derive a suggested local hosted-server configuration from a
registry entry, preferring a locally-runnable package over a remote transport:
an `npm` package becomes command `npx` with `-y <identifier>@<version>`; a
`pypi` package becomes command `uvx` with `<identifier>` (pinned to the version
when present); runtime arguments then package arguments are appended in
registry order; declared environment variables and headers are included as
empty placeholders flagged required/secret. When no supported local package
exists, the connector SHALL fall back to the entry's remote transport
(`http`/`sse`) with its URL and header placeholders. An entry with neither a
supported package nor a remote transport SHALL be reported as unsupported. The
suggestion SHALL include the chosen transport, a derived local name, the
required/secret environment and header keys, and a warning — not a hard failure
— when the runner required by the chosen package (`npx` or `uvx`) is not
available on the connector host.

#### Scenario: npm entry (DevTools MCP)

- **WHEN** the entry offers an npm stdio package
- **THEN** the suggestion is a stdio server running `npx -y <identifier>@<version>` with a derived local name

#### Scenario: pypi entry

- **WHEN** the entry offers a pypi package
- **THEN** the suggestion is a stdio server running `uvx <identifier>`, pinned when the entry carries a version

#### Scenario: Arguments are preserved in order

- **WHEN** the entry declares runtime and/or package arguments
- **THEN** the suggestion appends them in registry order after the runner and package

#### Scenario: Remote fallback

- **WHEN** the entry has no supported local package but offers a remote transport
- **THEN** the suggestion is an `http` or `sse` server with the remote URL and header placeholders

#### Scenario: Unsupported entry

- **WHEN** the entry has neither a supported package nor a remote transport
- **THEN** the connector reports the entry as unsupported and offers no suggested config

#### Scenario: Runner not installed

- **WHEN** the selected package needs a runner that is not available on the connector host
- **THEN** the suggestion is still returned with a warning naming the missing runner

### Requirement: Install from the registry is user-reviewed and stays local

The connector SHALL NOT create a hosted server from a registry entry on its own;
it SHALL return the suggestion for the client to review and edit, and a server
SHALL be created only through the existing create endpoint with the user's final
values. Commands, arguments, environment variables, and headers SHALL remain in
the connector's local configuration and SHALL NOT be sent to Memory.

#### Scenario: Suggestion does not create a server

- **WHEN** a client requests a registry entry's suggestion
- **THEN** no hosted server is created until the client submits a create request

#### Scenario: User-edited values are persisted

- **WHEN** the user edits the suggested command/args/env and confirms
- **THEN** the server is created with those values through the existing create endpoint

#### Scenario: Secrets stay local

- **WHEN** the user enters secret environment variables or headers
- **THEN** they are stored only in the connector's local config and are never sent to Memory
