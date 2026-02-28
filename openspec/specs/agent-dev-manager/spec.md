# Agent Dev Manager

## Requirements

### Requirement: Local Dev Management
The agent SHALL be able to manage the local development environment using standard project commands.

#### Scenario: Starting Services
- **WHEN** the user asks to start the local development environment
- **THEN** the agent runs the appropriate command to spin up services in the background (e.g., `npm run workspace:start` or `task start`)

#### Scenario: Stopping Services
- **WHEN** the user asks to stop the local development environment
- **THEN** the agent runs the appropriate command to halt services (e.g., `npm run workspace:stop` or `task stop`)

#### Scenario: Checking Status
- **WHEN** the user asks if services are running or what ports they are on
- **THEN** the agent uses `task status` to verify and reports back the state and known ports (Frontend: 5176, Backend: 3002)

#### Scenario: Restarting Services
- **WHEN** the user asks to restart a service or the entire environment
- **THEN** the agent first stops the service and then starts it again, unless there's a specific restart command available
