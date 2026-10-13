## ADDED Requirements

### Requirement: Configure banner explains each missing key and links to docs

When the edit form is opened for configuration, the banner naming the missing required
keys SHALL also show each key's registry description and SHALL link to the provider's
credential documentation derived from the server's registry repository URL. When the
server has no repository URL, no docs link SHALL render.

#### Scenario: Missing key shows its registry description

- **WHEN** the configure banner renders a missing key whose registry declaration includes a description
- **THEN** the banner shows that description alongside the key name

#### Scenario: Provider docs link is shown for a GitHub install

- **WHEN** the configure banner renders a server whose registry repository is a GitHub repository
- **THEN** it links to the GitHub token settings page

#### Scenario: No provenance yields no link

- **WHEN** the configure banner renders a manually-created server with no registry repository URL
- **THEN** no credential docs link is rendered

### Requirement: Authorization headers are entered as a scheme and token

For a required header named `Authorization`, the create/edit form SHALL render a scheme
picker and a token field instead of a free-form header value. The scheme SHALL default
to `Bearer` (or the scheme named by the registry value template when present) and SHALL
be overridable. On submit the form SHALL assemble the header value as
`"<scheme> <token>"`, SHALL NOT double-prefix a token that already carries the scheme,
and SHALL pass the token through unchanged when the raw scheme is selected. A blank
token SHALL leave the stored secret unchanged on edit.

#### Scenario: Token is assembled with the selected scheme

- **WHEN** a user selects `Bearer` and enters `ghp_x`
- **THEN** the stored `Authorization` header value is `Bearer ghp_x`

#### Scenario: Pasting a full value does not double-prefix

- **WHEN** a user pastes `Bearer ghp_x` into the token field with `Bearer` selected
- **THEN** the stored value is `Bearer ghp_x`, not `Bearer Bearer ghp_x`

#### Scenario: Blank token keeps the stored secret

- **WHEN** an edit is submitted with the token field left blank
- **THEN** the previously stored secret is kept

### Requirement: Authentication rejection is reported as actionable

When a remote MCP server answers a connect, inspect, or discovery request with HTTP
401 or 403, memory SHALL report the failure as a credential rejection naming the status
and the expected form of the credential (for an `Authorization` header, `Bearer
<token>`), rather than a generic connection error.

#### Scenario: Test connection reports a rejected credential

- **WHEN** a save-less connection test reaches a server that answers 401
- **THEN** the reported error attributes the failure to the credential and mentions the `Bearer` form

#### Scenario: Discovery reports a rejected credential

- **WHEN** tool discovery reaches a server that answers 403
- **THEN** the surfaced error attributes the failure to the credential rather than reporting an opaque connection error
