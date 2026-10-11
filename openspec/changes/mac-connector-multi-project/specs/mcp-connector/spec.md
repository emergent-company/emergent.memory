## MODIFIED Requirements

### Requirement: Connect and register with the project relay hub

The connector SHALL open an outbound WebSocket to the project's MCP relay hub
endpoint and register itself with a stable instance id, version, and its current
tool list, using the project-scoped bearer token from its configuration. A single
machine MAY hold **multiple simultaneous registrations**, one per connected
project, each with its own project-scoped token; the hub keys each session by
`(project, instance)`, so registrations for different projects coexist even when
they share an instance id. Re-registration replacement applies only within the
same project.

#### Scenario: Successful registration

- **WHEN** the connector starts with valid configuration and the hub is reachable
- **THEN** it connects and registers, and the node appears in the Memory project's connected relay sessions with its tool count

#### Scenario: Registration replaces a stale session

- **WHEN** a node with the same instance id reconnects **to the same project**
- **THEN** the new registration replaces the previous session without error, and
  any simultaneous session for a different project is left untouched

#### Scenario: Multiple projects registered simultaneously

- **WHEN** one machine connects to two projects at once
- **THEN** each project's relay session exists independently (keyed by project
  and instance), and both appear in their own project's connected sessions

#### Scenario: Hub unreachable at start

- **WHEN** the hub cannot be reached when the connector starts
- **THEN** the connector retries with backoff and does not exit on its own
