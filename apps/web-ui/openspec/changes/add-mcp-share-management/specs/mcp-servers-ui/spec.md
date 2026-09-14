## ADDED Requirements

### Requirement: Reach MCP Sharing from the MCP Servers page

The MCP Servers page SHALL provide a navigation entry point to the MCP Sharing area so admins can move between managing servers the project consumes and shares the project exposes.

#### Scenario: Entry point present

- **WHEN** a user opens the MCP Servers page
- **THEN** a visible link to the MCP Sharing page is shown

#### Scenario: Navigate to sharing

- **WHEN** the user selects the MCP Sharing entry point
- **THEN** the MCP Sharing page opens in the same project context
