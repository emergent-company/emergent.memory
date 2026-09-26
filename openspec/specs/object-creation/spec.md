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

Each schema-defined property SHALL render an input widget appropriate to its declared type: `date` uses a date picker, `number` and `integer` a numeric input, `boolean` a toggle switch, and `array`/`object` a multi-value chip input. A `string` property SHALL render a multi-line plain-text field by default; it SHALL render a single-line text input when it declares `widget: "input"`, and a select of the allowed values when it declares an `enum`. The multi-line field SHALL be auto-growing and vertically resizable, and SHALL carry a live character count; no other widget type SHALL show a character count.

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
- **WHEN** a property is declared with type `string` or an unrecognised type and does not declare `widget: "input"` or an `enum`
- **THEN** it renders an auto-growing, vertically resizable multi-line text field sized to present long text at a comfortable height
- **AND** the field shows a live, digit-grouped character count (for example "41,594 characters")

#### Scenario: Single-line override
- **WHEN** a `string` property declares `widget: "input"`
- **THEN** it renders a single-line text input
- **AND** no character count is shown

#### Scenario: Enum property
- **WHEN** a `string` property declares an `enum`
- **THEN** it renders a select of the allowed values
- **AND** no character count is shown

#### Scenario: Character count excludes non-text widgets
- **WHEN** a property renders as a date picker, numeric input, toggle switch, select, or chip input
- **THEN** no character count is shown

#### Scenario: Count updates live
- **WHEN** the user edits a multi-line text field
- **THEN** its character count updates to reflect the current value

#### Scenario: Count matches the browser for normalized line endings
- **WHEN** a multi-line value is stored with CRLF or lone CR line endings
- **THEN** the server-rendered initial count treats each of them as a single LF, so it matches the count the browser computes for the same field

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

The create form SHALL require only the type field; key, status, and labels are optional, and schema-defined properties are optional unless the schema declares them required.

#### Scenario: Minimal object
- **WHEN** the user creates an object with only a type and no other fields
- **THEN** the object is created with only the type set
