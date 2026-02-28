## ADDED Requirements

### Requirement: Traces Project Scoping
The `emergent traces` command SHALL automatically scope its queries to the active project context if one is set (via flag or environment file).

#### Scenario: Active project context scopes traces
- **WHEN** the user executes `emergent traces list` or `emergent traces search`
- **AND** a project context is active (e.g., via `.env.local` or `--project-id`)
- **THEN** the CLI SHALL automatically append a condition (e.g., `.project.id = "<project-id>"`) to the underlying TraceQL query to Tempo
- **AND** it SHALL only return traces associated with that project

#### Scenario: No active project context does not scope traces
- **WHEN** the user executes `emergent traces list` or `emergent traces search`
- **AND** no project context is active
- **THEN** the CLI SHALL execute the query against Tempo without the `.project.id` filter (returning all traces)
