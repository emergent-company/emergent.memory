## MODIFIED Requirements

### Requirement: List objects

The objects page SHALL list the knowledge graph's objects in browse mode as the 25 most-recent per page (newest first), each showing its name, type, status, and embedding status, with a "Load more" control when a further page exists. When an agent-provenance filter is active, the list SHALL show only objects matching that filter.

#### Scenario: Objects present

- **WHEN** the objects page loads in browse mode and objects exist
- **THEN** the 25 most-recent objects are listed, each showing name, type, status, and embedding status

#### Scenario: More pages exist

- **WHEN** more than one page of objects exists
- **THEN** a "Load more" control is shown and activating it appends the next page of objects

#### Scenario: No objects

- **WHEN** the objects page loads in browse mode and no objects exist
- **THEN** a clear "no objects yet" empty state is shown

## ADDED Requirements

### Requirement: Filter objects by agent provenance

The objects page SHALL let the user filter the list by agent provenance, in Created-by or Updated-by modes, and SHALL reuse the same browse/detail components for an agent-scoped view.

#### Scenario: Filter by creator

- **WHEN** the user selects an agent-provenance filter in "Created by" mode
- **THEN** only objects whose earliest surviving version was authored by that agent are listed

#### Scenario: Filter by updater

- **WHEN** the user selects an agent-provenance filter in "Updated by" mode
- **THEN** only objects whose HEAD version was authored by that agent are listed

#### Scenario: Agent-scoped view reuses browse components

- **WHEN** the user opens an agent-scoped object view from the agent dashboard
- **THEN** the existing object browse and detail components render, scoped to that agent's provenance, with no memory-specific naming
