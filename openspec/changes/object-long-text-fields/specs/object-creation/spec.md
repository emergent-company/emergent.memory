## Purpose

Correct the schema-driven create form's property inputs so free-form string
properties present long text in a usable multi-line field: an auto-growing,
vertically resizable textarea with a live character count, distinct from the
single-line override and the other type-specific widgets.

## MODIFIED Requirements

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
