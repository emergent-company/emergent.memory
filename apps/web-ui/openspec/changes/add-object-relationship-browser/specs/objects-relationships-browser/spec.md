## Purpose

Lets users browse a project's memory objects and their relationships through the web app, with search, filtering, list and detail views, and relationship navigation.

## ADDED Requirements

### Requirement: Objects & Relationships menu entry

The web app main menu SHALL include an "Objects & Relationships" entry that navigates to the browser page.

#### Scenario: Menu entry present
- **WHEN** the user opens the web app main menu
- **THEN** an "Objects & Relationships" entry is present
- **AND** selecting it navigates to the objects & relationships browser page

#### Scenario: Spotlight access
- **WHEN** the user opens the ⌘K spotlight palette
- **THEN** the "Objects & Relationships" page is reachable from the palette

### Requirement: Browse project memory objects

The browser SHALL list the project's memory objects and allow searching and filtering them.

#### Scenario: List objects
- **WHEN** the user opens the browser for a project
- **THEN** the project's memory objects are listed

#### Scenario: Search objects
- **WHEN** the user enters a search query
- **THEN** only objects matching the query are shown

#### Scenario: Filter by object type
- **WHEN** the user filters by object type
- **THEN** only objects of that type are shown

### Requirement: View object detail

Selecting an object SHALL display its detail, including its properties and its relationships to other objects.

#### Scenario: Detail on selection
- **WHEN** the user selects an object
- **THEN** a detail view shows the object's properties and its relationships

### Requirement: Relationship navigation

The browser SHALL let the user navigate from an object to its related objects.

#### Scenario: Follow a relationship
- **WHEN** the user follows a relationship from a selected object
- **THEN** the related objects are shown and are navigable

### Requirement: Graph view of relationships

The browser SHALL provide a graph view that renders objects as nodes and relationships as edges.

#### Scenario: Graph rendered
- **WHEN** the user opens the graph view
- **THEN** objects are rendered as nodes and relationships as edges
- **AND** selecting a node opens its object detail

### Requirement: Empty and error states

The browser SHALL present deterministic empty and error states.

#### Scenario: No objects
- **WHEN** the project has no memory objects
- **THEN** an empty-state message is shown

#### Scenario: Backend failure
- **WHEN** the backend query fails
- **THEN** an error message is shown and the browser remains usable
