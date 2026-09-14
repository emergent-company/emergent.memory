# object-creation Specification

## Purpose
Enables users to manually create memory objects from the web UI by filling a schema-driven form and persisting the object to the project's knowledge graph.

## Requirements

### Requirement: Create object entry point

The Objects browser SHALL provide a "New object" action that opens a create form for manually adding a memory object.

#### Scenario: Initiate creation
- **WHEN** the user opens the Objects browser
- **THEN** a "New object" action is visible
- **AND** activating it opens the create form

### Requirement: Schema-driven create form

The create form SHALL present a type selector constrained to the project's compiled object types, plus inputs for key, status, labels, and the selected type's schema-defined properties.

#### Scenario: Type selection
- **WHEN** the user selects an object type
- **THEN** the property inputs for that type are shown
- **AND** the available types come from the project's compiled object types

#### Scenario: Required type
- **WHEN** the user submits the form without selecting a type
- **THEN** the submission is rejected with a clear validation message
- **AND** the user's other entered values are preserved

#### Scenario: Internal properties hidden
- **WHEN** an object type defines underscore-prefixed internal properties
- **THEN** those properties are not shown or editable in the create form

### Requirement: Persist and navigate

Submitting a valid create form SHALL create the object in the project's graph and navigate to the new object's detail view.

#### Scenario: Successful creation
- **WHEN** the user submits a valid create form
- **THEN** an object of the chosen type is created with the entered fields
- **AND** the user is navigated to the new object's detail view

#### Scenario: Create failure
- **WHEN** the create request fails (for example, the memory service is unavailable or rejects the object)
- **THEN** the user sees an error message
- **AND** the entered values are preserved for retry

### Requirement: Type-aware property inputs

Each schema-defined property SHALL render an input widget appropriate to its declared type: `date` uses a date picker, `number` a numeric input, `boolean` a toggle switch, and `array`/`object` a multi-value chip input. A property whose type is `string` (or any unrecognised type) uses a plain text input.

#### Scenario: Date property
- **WHEN** a property is declared with type `date`
- **THEN** it renders a date picker input
- **AND** the submitted value is stored as the object's property value

#### Scenario: Number property
- **WHEN** a property is declared with type `number`
- **THEN** it renders a numeric input

#### Scenario: Boolean property
- **WHEN** a property is declared with type `boolean`
- **THEN** it renders a toggle switch

#### Scenario: Array property
- **WHEN** a property is declared with type `array`
- **THEN** it renders a chip input where each value is added and removed individually

#### Scenario: String and unknown types
- **WHEN** a property is declared with type `string` or an unrecognised type
- **THEN** it renders a plain text input

### Requirement: Labels chip input with autocomplete

The labels/tags field SHALL render a chip input that adds a label on Enter/comma and removes it with a per-chip remove action, with a combobox-style autocomplete dropdown drawn from the project's existing labels.

#### Scenario: Add and remove labels
- **WHEN** the user types a label and presses Enter (or the Add action)
- **THEN** the label appears as a removable chip
- **AND** each submitted label is stored on the created object

#### Scenario: Label autocomplete dropdown
- **WHEN** the project already has objects with labels and the user types
- **THEN** a dropdown lists the matching existing labels, excluding already-added ones

#### Scenario: Keyboard selection
- **WHEN** the dropdown is open
- **THEN** ArrowDown/ArrowUp move the highlight among the suggestions
- **AND** Enter adds the highlighted suggestion (or the typed text as a new label when none is highlighted)
- **AND** Escape closes the dropdown

### Requirement: Optional fields

Fields other than type are optional.

#### Scenario: Minimal object
- **WHEN** the user creates an object with only a type and no other fields
- **THEN** the object is created with only the type set
