## MODIFIED Requirements

### Requirement: List the project's active schema packs

`GET /schema/packs` SHALL list the project's active schema packs. Each row SHALL show the pack's
name, version, owner (Global or Project), type counts, and description, and SHALL link to the pack
detail. A project's packs come only from applied blueprints, a manual Add-schema install, or a
project-authored pack; there is no read-time system pack.

#### Scenario: Active packs listed

- **WHEN** the project has one or more active schema packs
- **THEN** the list shows each pack with its name, version, owner badge, type counts, and description

#### Scenario: Global pack from an applied blueprint

- **WHEN** a built-in blueprint pack contributes compiled types through an applied blueprint
- **THEN** the pack is listed and labelled Global, with a link to the owning blueprint

#### Scenario: Global pack without a blueprint

- **WHEN** compiled types are loaded
- **THEN** no global pack exists without an owning blueprint — the read-time system pack
  (`is_system=true`) is removed, so every global pack is linked to its owning blueprint

#### Scenario: Project override pack

- **WHEN** a project-owned override pack contributes compiled types
- **THEN** the pack is listed as Project-owned with the types it contributes

#### Scenario: Superseded types are not listed

- **WHEN** a later pack supersedes a type declared by an earlier pack
- **THEN** the superseded type is not listed under the earlier pack and is not counted toward its type totals

#### Scenario: No active packs

- **WHEN** the project has no active schema packs
- **THEN** the list shows an empty state that points at the Add schema action

#### Scenario: Packs list load failure

- **WHEN** the compiled types cannot be loaded
- **THEN** the page shows a clear error state and the rail remains available
