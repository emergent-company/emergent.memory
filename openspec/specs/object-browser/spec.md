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

The objects page SHALL list the knowledge graph's objects, each showing its name, type, status, and embedding status.

#### Scenario: Objects present

- **WHEN** the objects page loads and objects exist
- **THEN** the objects are listed, each showing name, type, status, and embedding status

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

Selecting an object SHALL show its properties, relationships, and embedding status. Long free-form string properties SHALL be presented as usable multi-line fields, and client-side navigation into the detail view SHALL initialize those fields the same way a direct page load does.

#### Scenario: Open an object

- **WHEN** the user selects an object in the list
- **THEN** the object's properties and embedding status are shown

#### Scenario: Long text is readable

- **WHEN** an object's string property holds long text (for example tens of thousands of characters)
- **THEN** it renders as a multi-line, auto-growing, vertically resizable field sized to a comfortable reading height rather than a single-line box
- **AND** the field shows a live character count

#### Scenario: Client-side navigation initializes long-text fields

- **WHEN** the user navigates to an object's detail view by selecting a link (an htmx-boosted swap of the main content region) rather than loading the URL directly
- **THEN** the long-text fields are grown to their content and their character counts are initialized, exactly as on a direct page load

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
