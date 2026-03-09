## ADDED Requirements

### Requirement: Dispatch Mode Field on Agent Definition

The system SHALL add a `dispatch_mode` field to `AgentDefinition` that controls how the runtime schedules execution of that agent.

#### Scenario: Default dispatch mode

- **WHEN** an `AgentDefinition` is created without specifying `dispatch_mode`
- **THEN** the system SHALL default `dispatch_mode` to `sync`
- **AND** the agent SHALL execute synchronously as before this change

#### Scenario: Valid dispatch mode values

- **WHEN** a `dispatch_mode` value is persisted to `kb.agent_definitions`
- **THEN** the system SHALL accept only `sync` or `queued`
- **AND** any other value SHALL be rejected with a validation error

#### Scenario: Dispatch mode stored in DB

- **WHEN** an agent definition is created or updated via the API or auto-provisioner
- **THEN** the `dispatch_mode` value SHALL be persisted in the `kb.agent_definitions.dispatch_mode` column
- **AND** the column SHALL have `DEFAULT 'sync'` at the DB level for backward compatibility

### Requirement: YAML Dispatch Mode Parsing

The system SHALL parse `dispatch_mode` from agent definition YAML files.

#### Scenario: YAML field parsed

- **WHEN** an agent YAML file contains `dispatch_mode: queued`
- **THEN** the auto-provisioner SHALL parse this field
- **AND** store `dispatch_mode: queued` in `kb.agent_definitions`

#### Scenario: Missing YAML field defaults to sync

- **WHEN** an agent YAML file does not contain a `dispatch_mode` field
- **THEN** the system SHALL treat the agent as `dispatch_mode: sync`
- **AND** existing YAML files SHALL continue to work without modification

### Requirement: Dispatch Mode API Visibility

The system SHALL expose `dispatch_mode` on agent definition API responses so callers can inspect how an agent will be dispatched.

#### Scenario: API response includes dispatch mode

- **WHEN** an agent definition is returned via the agents REST API
- **THEN** the JSON response SHALL include `"dispatchMode": "sync"` or `"dispatchMode": "queued"`

#### Scenario: DB migration backward compatibility

- **WHEN** the migration adding `dispatch_mode` is applied to a database with existing agent definitions
- **THEN** all existing rows SHALL receive `dispatch_mode = 'sync'`
- **AND** no existing agent behaviour SHALL change
