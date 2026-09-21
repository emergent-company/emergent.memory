## MODIFIED Requirements

### Requirement: List objects

The objects page SHALL list the knowledge graph's objects, each showing its name, type, status, and embedding status.

#### Scenario: Objects present

- **WHEN** the objects page loads and objects exist
- **THEN** the objects are listed, each showing name, type, status, and embedding status

#### Scenario: No objects

- **WHEN** the objects page loads and no objects exist
- **THEN** a clear "no objects yet" empty state is shown

### Requirement: View an object's details

Selecting an object SHALL show its properties, relationships, and embedding status.

#### Scenario: Open an object

- **WHEN** the user selects an object in the list
- **THEN** the object's properties and embedding status are shown

#### Scenario: Object has relationships

- **WHEN** the object has relationships
- **THEN** those relationships are listed with their type and source/target names

#### Scenario: Object has no relationships

- **WHEN** the object has no relationships
- **THEN** a clear "no relationships" state is shown
