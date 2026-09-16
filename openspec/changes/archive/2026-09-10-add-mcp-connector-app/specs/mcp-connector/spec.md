## MODIFIED Requirements

### Requirement: Configure a project from the CLI

The connector SHALL provide an `init` command that captures the server URL, a project-scoped bearer token, and project context, validates connectivity and auth against the hub, and writes a local configuration file with restrictive permissions. The connector SHALL also honor an optional `disabled_tools` list in its configuration: such tools are NOT registered with the hub, are NOT reported by `status`, and are rejected when called.

#### Scenario: init succeeds

- **WHEN** a user runs init with a reachable server and valid token
- **THEN** the configuration is written and the user is told the connector is ready to relay

#### Scenario: init rejects invalid token

- **WHEN** a user runs init with a server that rejects the token
- **THEN** init reports the auth failure and writes no configuration

#### Scenario: Disabled tools are not registered

- **WHEN** the configuration lists a tool under `disabled_tools`
- **THEN** that tool is absent from the registered tool list and from `status`, and calls to it return an error naming the tool as disabled
