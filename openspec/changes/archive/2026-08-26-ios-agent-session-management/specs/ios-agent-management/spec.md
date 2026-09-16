## Purpose

Lets the user manage their Alfred agents from the iOS app — listing agents from the control-plane API and adding, removing, enabling, and disabling them — with an empty-agent call-to-action and QR-based backend authentication.

## ADDED Requirements

### Requirement: List agents from the API

The app SHALL fetch and display the list of agents from the control-plane API (`GET /api/agents`) and MUST NOT rely on a hardcoded agent list.

#### Scenario: Agents listed

- **WHEN** the app loads the main screen and the control-plane API is reachable
- **THEN** the app shows the agents returned by the API, including their enabled state

#### Scenario: API unreachable

- **WHEN** the control-plane API is unreachable or returns an error
- **THEN** the app surfaces a clear error and does not show a phantom empty or stale list

### Requirement: Empty-agent call to action

The app SHALL show a call-to-action to add an agent when no agents exist.

#### Scenario: Add first agent

- **WHEN** the agent list is empty
- **THEN** the app presents a prominent call-to-action that opens the add-agent flow

### Requirement: Select an agent

The app SHALL let the user choose an agent from the picker, opening that agent's second level and driving the existing voice connect flow.

#### Scenario: Choose an agent

- **WHEN** the user selects an enabled agent from the picker
- **THEN** the app opens the agent's second level and uses that agent for the next conversation

### Requirement: Add an agent

The app SHALL let the user add a new agent by submitting a name and the required backend configuration to the control-plane API (`POST /api/agents`).

#### Scenario: Agent added

- **WHEN** the user submits a valid new-agent form
- **THEN** the app creates the agent via the API and shows it in the agent picker

#### Scenario: Duplicate name

- **WHEN** the user submits a name that already exists
- **THEN** the app shows the API's conflict error and keeps the form open for correction

### Requirement: Remove an agent

The app SHALL let the user remove an agent via the control-plane API (`DELETE /api/agents/{id}`).

#### Scenario: Agent removed

- **WHEN** the user confirms removal of an agent
- **THEN** the app deletes the agent via the API and removes it from the picker

#### Scenario: Remove failure

- **WHEN** the delete request fails
- **THEN** the app reports the failure and leaves the agent in the picker

### Requirement: Enable and disable agents

The app SHALL let the user enable or disable an agent through the control-plane API (`/activate` and `/deactivate`).

#### Scenario: Disable an agent

- **WHEN** the user disables an enabled agent
- **THEN** the app calls the deactivate endpoint and shows the agent as disabled

#### Scenario: Enable an agent

- **WHEN** the user enables a disabled agent
- **THEN** the app calls the activate endpoint and shows the agent as enabled

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
