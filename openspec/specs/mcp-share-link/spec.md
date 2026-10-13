# mcp-share-link Specification

## Purpose
The MCP share response includes ready-to-paste agent config snippets (Claude Desktop, Cursor) and the project-scoped MCP endpoint URL, so a recipient can configure an AI agent client without further steps.

## Requirements

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

The response SHALL include a `mcpUrl` field containing the fully-qualified MCP server URL for the project. This URL MUST be usable directly as the MCP server endpoint in any compliant MCP client. The origin is derived from the request's `Host` and scheme: HTTPS when the request is TLS-terminated or carries an `X-Forwarded-Proto` header, HTTP otherwise. Consistent with the `a2a-discovery` capability, a client-supplied `X-Forwarded-Host` header is deliberately NOT honoured — a spoofed header must not be able to point clients at an attacker-controlled origin. The web-UI gateway, not the server, is responsible for rewriting this URL to the memory server's configured public origin before rendering it to a user.

#### Scenario: mcpUrl is present and well-formed

- **WHEN** a share token is generated
- **THEN** `mcpUrl` in the response is a valid URL pointing to the server's `/api/mcp` endpoint, using the request's scheme (HTTPS when the request is TLS-terminated or carries an `X-Forwarded-Proto` header, HTTP otherwise)

#### Scenario: mcpUrl is derived from the request host and scheme

- **WHEN** the share request arrives at `https://api.example.com`
- **THEN** `mcpUrl` is `https://api.example.com/api/mcp`

### Requirement: Admin UI displays the share snippet in a copyable format

The admin UI SHALL present the generated token and config snippets in a modal or panel with a one-click copy button for each snippet. The raw token value MUST be displayed with a warning that it will not be shown again. The endpoint URL shown and every generated snippet MUST use the memory server's externally-reachable origin (the configured `MEMORY_PUBLIC_URL`, defaulting to `MEMORY_URL`) regardless of the host the backend reports, and the reveal SHALL offer a one-click download of a `.mcpb` Claude Desktop bundle built from the same endpoint and token.

#### Scenario: Token displayed with one-time warning

- **WHEN** the admin generates a share token via the UI
- **THEN** the modal displays the raw token value alongside a message stating it will not be shown again
- **THEN** a "Copy" button is available for the token value

#### Scenario: Config snippet tabs for each supported client

- **WHEN** the share modal is open
- **THEN** the UI shows at least two tabs or sections: one for Claude Desktop and one for Cursor
- **THEN** each section contains the pre-formatted snippet and a "Copy" button

#### Scenario: Snippets and URL use the external origin

- **WHEN** the backend reports an MCP URL on an internal host
- **THEN** the displayed endpoint URL and every snippet use the memory server's external origin instead

#### Scenario: Claude Desktop bundle is downloadable

- **WHEN** the reveal is shown
- **THEN** it offers a download link to the project `.mcpb` bundle bound to the reveal's token
