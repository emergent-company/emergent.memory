## MODIFIED Requirements

### Requirement: List agents from the API

The app SHALL fetch and display the list of agents from the control-plane API (`GET /api/agents`) and MUST NOT rely on a hardcoded agent list. The list SHALL render each agent as a standard native iOS list row using system fonts, default insets, and default row height, without custom card backgrounds, oversized padding, or decorative entrance animations.

#### Scenario: Agents listed

- **WHEN** the app loads the main screen and the control-plane API is reachable
- **THEN** the app shows the agents returned by the API as native list rows

#### Scenario: API unreachable

- **WHEN** the control-plane API is unreachable or returns an error
- **THEN** the app surfaces a clear error and does not show a phantom empty or stale list

### Requirement: Empty-agent call to action

The app SHALL show a read-only empty state when no agents exist, directing the user to configure agents from web/desktop instead of opening an add-agent flow.

#### Scenario: Add first agent

- **WHEN** the agent list is empty
- **THEN** the app shows a message directing the user to add agents from web/desktop and does not present any add-agent affordance

## REMOVED Requirements

### Requirement: Add an agent

**Reason**: Agent configuration is no longer supported from the phone; agents are managed from web/desktop.

**Migration**: No migration. The add-agent UI is removed and users add agents from web/desktop.

### Requirement: Remove an agent

**Reason**: Agent configuration is no longer supported from the phone; agents are managed from web/desktop.

**Migration**: No migration. The remove-agent UI is removed and users remove agents from web/desktop.

### Requirement: Enable and disable agents

**Reason**: Agent configuration is no longer supported from the phone; agents are managed from web/desktop.

**Migration**: No migration. The enable/disable UI is removed and users toggle agents from web/desktop.
