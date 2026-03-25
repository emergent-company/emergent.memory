## Why

Admins have no way to share MCP access with teammates or external agents without granting full project membership. A lightweight, read-only sharing flow — via a shareable link or email invitation — would let teams onboard AI agents quickly without managing full user accounts or exposing write access.

## What Changes

- Admins can generate a **read-only MCP API token** scoped to a project (using the existing `project_viewer` role and `viewerReadOnlyScopes`)
- A **shareable link** is produced containing the MCP endpoint URL and the token, ready to paste into an agent config
- Admins can optionally **send an email invitation** to one or more addresses; the email includes the MCP endpoint, the token, and a setup guide
- The setup guide explains how to configure popular AI agents (Claude Desktop, Cursor, etc.) to use the MCP endpoint
- Generated tokens are listed and revocable from the existing API token management UI

## Capabilities

### New Capabilities

- `mcp-readonly-token-generation`: Admin UI flow to generate a read-only MCP API token for a project, returning the token value and a pre-formatted MCP endpoint URL
- `mcp-share-link`: Produce a shareable connection string / deep-link that encodes the MCP server URL and API key, ready to copy into an agent config file
- `mcp-email-invite`: Send an email to one or more addresses containing the MCP endpoint, the API key, and a rendered setup guide (Markdown → HTML) for configuring AI agents

### Modified Capabilities

- `project-viewer-role`: No requirement changes — the existing viewer role and `viewerReadOnlyScopes` are reused as-is for the generated tokens

## Impact

- **`apps/server/domain/mcp/`**: New handler endpoints for token generation and email dispatch
- **`apps/server/domain/apitoken/`**: Reuse existing token creation with `project_viewer` scopes; no schema changes expected
- **`apps/server/domain/email/`**: New email template for MCP invite (setup guide content)
- **Admin UI (`/root/emergent.memory.ui`)**: New modal or settings panel under project MCP settings for generating tokens and sending invites
- **No breaking changes** — all new surface area; existing MCP auth flow is unchanged
