# ios-agent-management Specification

## Purpose
Lets the user manage their Memory agents from the iOS app — listing agents from the control-plane API and adding, removing, enabling, and disabling them — with an empty-agent call-to-action and QR-based backend authentication.

## Requirements

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

### Requirement: Select an agent

The app SHALL let the user choose an agent from the picker, opening that agent's second level and driving the existing voice connect flow.

#### Scenario: Choose an agent

- **WHEN** the user selects an enabled agent from the picker
- **THEN** the app opens the agent's second level and uses that agent for the next conversation

### Requirement: Authenticated access

The app SHALL send the configured API key as the `X-API-Key` header on every control-plane request.

#### Scenario: Missing or invalid key

- **WHEN** the control-plane API rejects the API key as unauthorized
- **THEN** the app surfaces an authorization error and exposes the path to re-authenticate

### Requirement: Authenticate via QR code

The app SHALL let the user authenticate to the backend by scanning a QR code that carries the server URL and API key; the QR flow MUST NOT configure an agent.

#### Scenario: QR authenticates the app

- **WHEN** the user scans a valid setup QR code
- **THEN** the app stores the server URL and API key and can reach the backend, without creating or changing an agent

#### Scenario: Invalid QR

- **WHEN** the scanned QR is malformed or lacks the required fields
- **THEN** the app reports the failure and keeps the previous configuration
