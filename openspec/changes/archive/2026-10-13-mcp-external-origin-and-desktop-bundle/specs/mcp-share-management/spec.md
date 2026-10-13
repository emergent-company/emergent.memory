## MODIFIED Requirements

### Requirement: Reveal the instance API key once

After creating an instance or rotating its key, the gateway SHALL reveal the raw API key exactly once together with copy controls and ready-to-paste configuration snippets for supported clients (Claude Desktop, Claude Code, Cursor, Cloud Code). The endpoint URL shown and every snippet MUST use the memory server's externally-reachable origin (the configured `MEMORY_PUBLIC_URL`, defaulting to `MEMORY_URL`), regardless of the host the backend reports. The reveal SHALL also offer a one-click download of a `.mcpb` Claude Desktop bundle bound to the revealed key. The UI SHALL warn that the key will not be shown again. After the admin dismisses the reveal, the key SHALL NOT be retrievable from the gateway.

#### Scenario: Key revealed after create

- **WHEN** an instance is created successfully
- **THEN** the UI shows the raw key with a one-time warning and copy button

#### Scenario: Snippets use the key and endpoint

- **WHEN** the reveal is shown
- **THEN** each snippet contains the project MCP endpoint URL and the new key

#### Scenario: Snippets and URL use the external origin

- **WHEN** the backend reports an MCP URL on an internal host
- **THEN** the displayed endpoint URL and every snippet use the memory server's external origin instead

#### Scenario: Claude Desktop bundle is downloadable

- **WHEN** the reveal is shown
- **THEN** it offers a download link to the project `.mcpb` bundle bound to the revealed key

#### Scenario: Key not retrievable later

- **WHEN** the admin returns to the instance after dismissing the reveal
- **THEN** the UI does not display the raw key and offers rotation instead
