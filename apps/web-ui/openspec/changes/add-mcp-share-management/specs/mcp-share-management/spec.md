## Purpose

Lets a project admin create and manage named MCP share instances from the web app — choosing which memory tools and agents an outside agent may access — and hand out the instance's API key and client config snippets.

## ADDED Requirements

### Requirement: List MCP share instances

The gateway SHALL render an MCP Sharing page listing every share instance for the active project, showing each instance's name, description, tool count, agent count, status, creation time, and last-used time, most recently created first. The page SHALL be reachable from the MCP Servers settings area and SHALL follow the same page-title and test-id conventions as other settings pages. A project with no instances SHALL show an empty state with a call-to-action to create one.

#### Scenario: Page renders instances

- **WHEN** an admin opens the MCP Sharing page for a project with instances
- **THEN** each instance is listed with name, tool count, agent count, status, and timestamps, and offers edit, rotate, and revoke actions

#### Scenario: Empty project

- **WHEN** the project has no share instances
- **THEN** the page shows an empty state with a "create a share" call-to-action

#### Scenario: Load failure

- **WHEN** the backend share API is unreachable
- **THEN** the page shows a clear error state and does not render a broken page

#### Scenario: Legacy instances shown read-only

- **WHEN** the project has an instance created through the backend's legacy share endpoint
- **THEN** it is listed and marked as legacy with no tool/agent allowlist editing

### Requirement: Create an MCP share instance

The gateway SHALL provide a create form with a required unique name, an optional description, a memory-tool picker, and an agent picker. The tool picker SHALL be populated from the backend tool catalog, grouped by category, searchable, and support select-all/clear, and MUST require at least one selected tool. The agent picker SHALL default to no agents (meaning all scope-permitted agents) and let the admin select specific agents. Submitting SHALL validate required fields, reject duplicate names and empty tool selections inline with no partial save, and on success SHALL return the admin to the list.

#### Scenario: Create with selected tools

- **WHEN** an admin names an instance, selects several memory tools, and submits
- **THEN** the instance is created with that tool allowlist and a success confirmation is shown

#### Scenario: Empty tool selection rejected

- **WHEN** an admin submits the form with no tools selected
- **THEN** an inline error is shown and no instance is created

#### Scenario: Duplicate name rejected

- **WHEN** an admin submits a name that matches an existing instance
- **THEN** the form shows an inline error naming the conflict and no instance is created

#### Scenario: Agent preselected from an agent

- **WHEN** an admin starts creation from an agent's "Share via MCP" action
- **THEN** the create form opens with that agent preselected in the agent picker

### Requirement: Reveal the instance API key once

After creating an instance or rotating its key, the gateway SHALL reveal the raw API key exactly once together with copy controls and ready-to-paste configuration snippets for supported clients (Claude Desktop, Claude Code, Cursor, Cloud Code). The UI SHALL warn that the key will not be shown again. After the admin dismisses the reveal, the key SHALL NOT be retrievable from the gateway.

#### Scenario: Key revealed after create

- **WHEN** an instance is created successfully
- **THEN** the UI shows the raw key with a one-time warning and copy button

#### Scenario: Snippets use the key and endpoint

- **WHEN** the reveal is shown
- **THEN** each snippet contains the project MCP endpoint URL and the new key

#### Scenario: Key not retrievable later

- **WHEN** the admin returns to the instance after dismissing the reveal
- **THEN** the UI does not display the raw key and offers rotation instead

### Requirement: Edit an instance

The gateway SHALL provide an edit form pre-filled with the instance's name, description, tool allowlist, and agent allowlist, and SHALL persist changes. Editing the tool allowlist SHALL update the instance's scoping in the backend without changing the instance's existing key. Legacy instances SHALL NOT be editable.

#### Scenario: Change the tool allowlist

- **WHEN** an admin adds or removes tools and saves
- **THEN** the instance's allowlist is updated and the list reflects the new tool count

#### Scenario: Editing keeps the key

- **WHEN** an admin edits an instance's scoping
- **THEN** the instance's existing API key continues to work

### Requirement: Revoke an instance

The gateway SHALL let an admin revoke an instance after confirmation in a dialog. Confirming SHALL revoke the instance in the backend, immediately invalidating its key, and update the list. Revoking SHALL NOT be offered for legacy instances through this UI.

#### Scenario: Revoke with confirmation

- **WHEN** the admin confirms revoke on an instance
- **THEN** the instance's status becomes revoked and its key no longer authenticates

#### Scenario: Cancel revoke

- **WHEN** the admin dismisses the confirmation dialog
- **THEN** the instance remains active

### Requirement: Rotate an instance key

The gateway SHALL let an admin rotate an instance's API key after confirmation and SHALL reveal the new key once using the same reveal flow as creation.

#### Scenario: Rotate reveals a new key

- **WHEN** the admin confirms rotation
- **THEN** the new key is revealed once and the previous key no longer authenticates

### Requirement: Gateway share API

The gateway SHALL expose authenticated routes under `/api` that proxy the backend share-instance and tool-catalog endpoints (list, create, get, update, revoke, rotate, and tool catalog). These routes SHALL require a valid session or API key under the existing auth boundary and MUST NOT expose share data to unauthenticated callers. The raw token value SHALL only ever be returned by create and rotate responses.

#### Scenario: Authenticated create proxies to backend

- **WHEN** an authenticated client posts a valid create request
- **THEN** the gateway creates the instance in the backend and returns the instance plus the one-time token

#### Scenario: Unauthenticated request rejected

- **WHEN** an unauthenticated client calls any share route
- **THEN** the gateway returns an unauthorized response and no share data

#### Scenario: Backend error surfaced

- **WHEN** the backend rejects a request (for example duplicate name)
- **THEN** the gateway returns a matching error status and message to the client
