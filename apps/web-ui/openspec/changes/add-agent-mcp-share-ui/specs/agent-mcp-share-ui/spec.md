## Purpose

Lets a project admin turn a configured agent into a single-tool MCP server from the web app — generate a key, copy the per-agent endpoint and client config, and revoke or rotate access.

## ADDED Requirements

### Requirement: Share an agent as an MCP tool

The agent view SHALL provide a "Share as MCP tool" action. Invoking it SHALL create a per-agent MCP share credential in the backend and open a one-time reveal of the per-agent endpoint URL and raw API key.

#### Scenario: Action creates a share

- **WHEN** an admin triggers "Share as MCP tool" on an agent
- **THEN** a per-agent share is created and the reveal is shown

#### Scenario: Action unavailable without permission

- **WHEN** a user without project admin rights triggers the action
- **THEN** the gateway returns a forbidden response and no share is created

### Requirement: Reveal the endpoint and key once

After creating or rotating a share, the UI SHALL show the per-agent MCP endpoint URL and the raw API key exactly once, with copy controls and ready-to-paste client snippets (Claude Desktop, Claude Code, Cursor, Cloud Code). A warning SHALL state the key will not be shown again. After dismissal, the key MUST NOT be retrievable from the gateway.

#### Scenario: Reveal shows endpoint and key

- **WHEN** a share is created successfully
- **THEN** the reveal shows the endpoint URL, the raw key with a copy button, and a one-time warning

#### Scenario: Snippets reference the endpoint and key

- **WHEN** the reveal is shown
- **THEN** each client snippet contains the per-agent endpoint URL and the new key

#### Scenario: Key not retrievable later

- **WHEN** the admin returns to the agent's shares after dismissing the reveal
- **THEN** the raw key is not displayed and rotation is offered instead

### Requirement: List an agent's MCP shares

The UI SHALL list the active project's shares for the agent showing name, description, status, creation time, and last-used time, most recent first, with an empty state when none exist.

#### Scenario: List renders shares

- **WHEN** an agent has shares
- **THEN** each is listed with its status and timestamps and offers rotate and revoke actions

#### Scenario: Empty state

- **WHEN** the agent has no shares
- **THEN** an empty state with a "create a share" call-to-action is shown

#### Scenario: Load failure

- **WHEN** the backend is unreachable
- **THEN** the UI shows a clear error state and does not render a broken page

### Requirement: Revoke and rotate a share

The UI SHALL let an admin revoke a share after confirmation and rotate a share's key after confirmation, revealing the new key once. A revoked share SHALL show a revoked status and MUST NOT offer further rotation.

#### Scenario: Revoke with confirmation

- **WHEN** the admin confirms revoke
- **THEN** the share is revoked and its key no longer authorizes the per-agent endpoint

#### Scenario: Rotate reveals the new key

- **WHEN** the admin confirms rotation
- **THEN** the new key is revealed once and the previous key no longer authorizes requests

#### Scenario: Revoked share cannot be rotated

- **WHEN** a share is revoked
- **THEN** the UI does not offer rotation for it

### Requirement: Gateway proxy for per-agent shares

The gateway SHALL expose authenticated routes under `/api` that proxy the backend per-agent share endpoints (create, list, revoke, rotate). These routes MUST require a valid session or API key and MUST NOT expose the raw token except in create and rotate responses.

#### Scenario: Authenticated create proxies

- **WHEN** an authenticated client posts a valid create request
- **THEN** the gateway creates the share in the backend and returns the share plus the one-time token

#### Scenario: Unauthenticated request rejected

- **WHEN** an unauthenticated client calls any per-agent share route
- **THEN** the gateway returns unauthorized and no share data

#### Scenario: Backend error surfaced

- **WHEN** the backend rejects a request (for example a duplicate name)
- **THEN** the gateway returns a matching status and message
