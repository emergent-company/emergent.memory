# object-browser Specification

## Purpose
Provides a gateway web UI surface for browsing the knowledge graph's objects (entities) and their relationships.

## Requirements

### Requirement: Navigate to the objects page

The app SHALL provide a navigation entry that opens the objects page.

#### Scenario: Open objects from navigation

- **WHEN** a user selects the objects entry in the app navigation
- **THEN** the objects page opens

### Requirement: List objects

The objects page SHALL list the knowledge graph's objects, each showing its name, type, and status.

#### Scenario: Objects present

- **WHEN** the objects page loads and objects exist
- **THEN** the objects are listed, each showing name, type, and status

#### Scenario: No objects

- **WHEN** the objects page loads and no objects exist
- **THEN** a clear "no objects yet" empty state is shown

### Requirement: Filter objects by type

The objects page SHALL let the user filter the list by object type.

#### Scenario: Filter by type

- **WHEN** the user selects a type filter
- **THEN** only objects of that type are listed

### Requirement: Browse by branch

The objects page SHALL let the user choose which graph branch to browse.

#### Scenario: Choose a branch

- **WHEN** the user selects a branch
- **THEN** the objects on that branch are listed

#### Scenario: Main branch by default

- **WHEN** the objects page loads with no branch selected
- **THEN** the main branch's objects are listed

### Requirement: View an object's details

Selecting an object SHALL show its properties and relationships.

#### Scenario: Open an object

- **WHEN** the user selects an object in the list
- **THEN** the object's properties are shown

#### Scenario: Object has relationships

- **WHEN** the object has relationships
- **THEN** those relationships are listed with their type and source/target names

#### Scenario: Object has no relationships

- **WHEN** the object has no relationships
- **THEN** a clear "no relationships" state is shown

### Requirement: Surface load failures without crashing

The objects page SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable
