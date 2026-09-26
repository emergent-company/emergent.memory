## Purpose

Ensure the object detail view presents long free-form string properties as
usable multi-line fields, and that those fields are initialized identically
whether the detail view is loaded directly or reached by boosted client-side
navigation.

## MODIFIED Requirements

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
