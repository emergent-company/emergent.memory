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

## ADDED Requirements

### Requirement: Isolate Memory-managed MCP configuration per config file

The connector SHALL persist the Memory-managed MCP server set (and its
credentials) in a directory derived from the **identity of the config file**, not
from its parent directory alone, so that multiple connector processes sharing a
parent directory but bound to different config files hold **independent** managed
sets. The primary/legacy config file (`memory-connector.yml`) SHALL keep its
existing persistence location for backward compatibility. The account-session
store location, which is derived from the config file's parent directory, SHALL
remain shared across sibling config files so a single account session continues to
serve every connected project.

#### Scenario: Distinct projects hold independent managed sets

- **WHEN** two connector processes run with sibling config files
  (`memory-connector.yml` and `memory-connector-<projectID>.yml`) in the same
  directory, and Memory pushes a managed MCP configuration to one of them
- **THEN** only that config file's managed set is persisted and applied, and the
  other config file's managed set is unchanged

#### Scenario: Primary config keeps its legacy managed location

- **WHEN** a connector runs with the primary/legacy config file
  (`memory-connector.yml`)
- **THEN** its managed set is persisted at the same location as before this
  change, so an existing installation's managed set is preserved

#### Scenario: Account session stays shared

- **WHEN** multiple sibling config files are used for an account's projects
- **THEN** they continue to share one account-session store derived from the
  shared parent directory, and sign-in/`projects use` works for every project
  without re-authentication
