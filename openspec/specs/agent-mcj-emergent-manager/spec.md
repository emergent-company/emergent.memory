# Agent Mcj Emergent Manager

## Requirements

### Requirement: Remote Server Management via SSH
The agent SHALL be able to manage the `mcj-emergent` server by executing commands over SSH.

#### Scenario: Connecting to the Server
- **WHEN** a management task for `mcj-emergent` is requested
- **THEN** the agent SHALL execute the command using `ssh mcj-emergent '<remote-command>'`

### Requirement: Application Upgrades
The agent SHALL use the `emergent` CLI to perform application upgrades on the remote server.

#### Scenario: Upgrading the Application
- **WHEN** the user asks to upgrade the `mcj-emergent` server
- **THEN** the agent SHALL run `ssh mcj-emergent 'emergent upgrade'` and report the outcome.

### Requirement: Service Management
The agent SHALL use the `emergent` CLI for all service management tasks on the remote server.

#### Scenario: Checking Service Status
- **WHEN** the user asks for the status of the `mcj-emergent` services
- **THEN** the agent SHALL run `ssh mcj-emergent 'emergent status'` and return the output.

#### Scenario: Restarting Services
- **WHEN** the user asks to restart the services on `mcj-emergent`
- **THEN** the agent SHALL run `ssh mcj-emergent 'emergent restart'` and confirm the operation's success.
