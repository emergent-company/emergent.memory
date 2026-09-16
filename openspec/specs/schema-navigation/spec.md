# schema-navigation Specification

## Purpose
A two-item secondary navigation rail that organizes the Schema area (the compiled schema and the project's schema packs) so users can move between the compiled view and per-pack detail without scrolling one long page.

## Requirements

### Requirement: Show the schema sub-navigation

Every page in the Schema area SHALL show a secondary navigation rail with exactly two items: Compiled schema (`/schema`) and Schema packs (`/schema/packs`).

#### Scenario: Rail present on the compiled schema page

- **WHEN** the compiled schema page loads
- **THEN** the rail shows the Compiled schema and Schema packs links

#### Scenario: Rail present on a schema packs page

- **WHEN** the Schema packs list, the Add schema page, or a pack detail page loads
- **THEN** the rail shows the Compiled schema and Schema packs links

### Requirement: Highlight the active schema section

The rail SHALL highlight the item for the current Schema section, marking it as the active page.

#### Scenario: Compiled section active

- **WHEN** the compiled schema page, an object-type detail page, or the object-type editor loads
- **THEN** the Compiled schema link is highlighted as the active page

#### Scenario: Packs section active

- **WHEN** the Schema packs list, the Add schema page, or a pack detail page loads
- **THEN** the Schema packs link is highlighted as the active page

### Requirement: List the project's active schema packs

`GET /schema/packs` SHALL list the project's active schema packs. Each row SHALL show the pack's name, version, owner (Global or Project), type counts, and description, and SHALL link to the pack detail.

#### Scenario: Active packs listed

- **WHEN** the project has one or more active schema packs
- **THEN** the list shows each pack with its name, version, owner badge, type counts, and description

#### Scenario: Global pack without a blueprint

- **WHEN** a global pack contributes compiled types but has no owning blueprint
- **THEN** the pack is listed and labelled Global

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

### Requirement: Read-only schema pack detail

`GET /schema/packs/{id}` SHALL render a read-only detail for one active pack: a metadata header (name, version, description, owner, installed/active state, and a link to the owning blueprint when one applies) and the object-type and relationship-type tables contributed by the pack. Object types SHALL link to `/schema/object-types/{name}`. The detail SHALL NOT offer any mutation.

#### Scenario: Pack detail shows contributing types

- **WHEN** an active pack's detail page loads
- **THEN** it shows the pack metadata and the object and relationship types the pack contributes, with object types linking to their object-type page

#### Scenario: Owning blueprint link

- **WHEN** an applied blueprint owns the pack
- **THEN** the detail shows a link to that blueprint's page

#### Scenario: Unknown pack id

- **WHEN** the requested pack id is not an active pack
- **THEN** the detail renders a not-found state without panicking

### Requirement: Add schema entry point

The Schema area SHALL provide an Add schema entry point that opens the existing install flow at `/schema/add`. Add schema SHALL NOT be a rail item; installing a pack via `/schema/add` SHALL continue to work.

#### Scenario: Add schema from the packs list

- **WHEN** the user selects Add schema on the Schema packs list
- **THEN** the existing Add schema install page opens

#### Scenario: Install still works

- **WHEN** the user installs an available pack from the Add schema page
- **THEN** the pack is installed and the project returns to the compiled schema, as before
