## ADDED Requirements

### Requirement: API response includes pre-formatted agent config snippets
The `POST /api/projects/{projectId}/mcp/share` response SHALL include a `snippets` object containing ready-to-use configuration blocks for supported AI agent clients. Each snippet MUST be a valid, complete configuration string that the user can paste directly into the agent's config file without modification.

#### Scenario: Response contains Claude Desktop snippet
- **WHEN** a share token is successfully generated
- **THEN** the response includes `snippets.claudeDesktop` as a JSON string representing a valid Claude Desktop `mcpServers` entry with the correct `url` and `X-API-Key` header

#### Scenario: Response contains Cursor snippet
- **WHEN** a share token is successfully generated
- **THEN** the response includes `snippets.cursor` as a JSON string representing a valid Cursor MCP server config entry

#### Scenario: Snippets use the generated token and project-scoped MCP URL
- **WHEN** a share token is generated for project `abc-123`
- **THEN** all snippets reference the MCP endpoint URL for that project and embed the newly generated API key

### Requirement: Share response includes the MCP endpoint URL
The response SHALL include a `mcpUrl` field containing the fully-qualified MCP server URL for the project. This URL MUST be usable directly as the MCP server endpoint in any compliant MCP client.

#### Scenario: mcpUrl is present and well-formed
- **WHEN** a share token is generated
- **THEN** `mcpUrl` in the response is a valid HTTPS URL pointing to the server's `/api/mcp` endpoint

#### Scenario: mcpUrl is consistent with the server's configured base URL
- **WHEN** the server is deployed at `https://api.example.com`
- **THEN** `mcpUrl` is `https://api.example.com/api/mcp`

### Requirement: Admin UI displays the share snippet in a copyable format
The admin UI SHALL present the generated token and config snippets in a modal or panel with a one-click copy button for each snippet. The raw token value MUST be displayed with a warning that it will not be shown again.

#### Scenario: Token displayed with one-time warning
- **WHEN** the admin generates a share token via the UI
- **THEN** the modal displays the raw token value alongside a message stating it will not be shown again
- **THEN** a "Copy" button is available for the token value

#### Scenario: Config snippet tabs for each supported client
- **WHEN** the share modal is open
- **THEN** the UI shows at least two tabs or sections: one for Claude Desktop and one for Cursor
- **THEN** each section contains the pre-formatted snippet and a "Copy" button
