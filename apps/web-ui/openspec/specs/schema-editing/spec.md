# schema-editing Specification

## Purpose

Lets users add schemas and create or edit object types directly in the Schema area of the web UI, showing which types came from a blueprint and warning before edits diverge from it, while surfacing the schema's version history.

## Requirements

### Requirement: Add a schema from the Schema area

The Schema page SHALL expose an "Add schema" action that lists installable schemas (bundled or registry) and installs the selected one in place, without leaving the Schema area.

#### Scenario: Install an available schema

- **WHEN** a user selects an available schema from the "Add schema" action and confirms
- **THEN** the schema is installed and its object and relationship types appear in the Schema area

#### Scenario: Install reports conflicts

- **WHEN** installing the selected schema would conflict with an existing type name
- **THEN** the UI reports the conflicts and does not silently overwrite the existing type

#### Scenario: No installable schemas

- **WHEN** the user opens "Add schema" and no uninstalled schemas exist
- **THEN** the UI shows an empty state instead of an empty selector

#### Scenario: Project-local override pack is not installable

- **WHEN** the project owns the `project-schema-overrides` pack created by editing a blueprint-derived type
- **THEN** "Add schema" SHALL NOT list that pack as an installable schema, because it is an internal copy-on-write artifact of the project's own schema, not a user-installable pack

#### Scenario: Schema service unavailable

- **WHEN** the install request fails because the memory service is unreachable
- **THEN** the UI shows an error and leaves the schema uninstalled

### Requirement: Create an object type in the Schema area

The Schema area SHALL let a user create a project object type with a name and typed properties, which becomes selectable in the object-create form.

#### Scenario: Create an object type

- **WHEN** a user creates an object type with a name and at least one property
- **THEN** the type appears in the Schema area and in the object-create type selector

#### Scenario: Invalid type definition

- **WHEN** a user submits an object type without a name or with a malformed property definition
- **THEN** the submission is rejected with a clear validation message and the entered values are preserved

### Requirement: Edit an object type in the Schema area

The Schema area SHALL let a user edit an object type's description, its type-level ui accent (icon and color), and its typed properties — adding, removing, and changing properties and each property's widget hint — and persist the change; the type name SHALL be immutable on edit.

#### Scenario: Edit a property

- **WHEN** a user edits a property of an existing object type and saves
- **THEN** the change is persisted and the compiled type subsequently reports the updated property

#### Scenario: Edit the type ui accent

- **WHEN** a user sets an icon and/or color on an object type and saves
- **THEN** the effective type's ui block carries the icon and color, and clearing a value removes it from the block

#### Scenario: Set a property widget hint

- **WHEN** a user chooses a widget hint (auto, single line, or text area) for a property and saves
- **THEN** the persisted property carries that widget, and "auto" persists no widget hint

#### Scenario: Edit resolves the effective (non-shadowed) type

- **WHEN** a project-owned override pack shadows a blueprint pack defining a type of the same name
- **THEN** the object-type detail and editor resolve the effective (non-shadowed) type, so a saved edit is shown rather than the stale shadowed blueprint definition

#### Scenario: Add and remove properties

- **WHEN** a user adds a new property and removes an existing one on an object type and saves
- **THEN** the persisted object type contains exactly the properties shown in the editor

#### Scenario: Type name is immutable

- **WHEN** a user views the object-type editor for an existing type
- **THEN** the name field is not editable and only description and properties can change

#### Scenario: Edit of an unknown type

- **WHEN** a user opens the editor for a type that does not exist
- **THEN** the UI reports that the type is unknown and offers a way back to the Schema list

#### Scenario: Edit rejected by the service

- **WHEN** the save request fails validation or the memory service rejects it
- **THEN** the UI shows an error and preserves the edited values for retry

### Requirement: Show blueprint provenance for object types

Each object type SHALL display the schema it originates from, and a type whose origin is an installed blueprint SHALL be marked as blueprint-derived and identify the owning blueprint by name and version.

#### Scenario: Blueprint-derived type

- **WHEN** an object type originates from an installed blueprint
- **THEN** the type is shown with a blueprint-derived marker and the owning blueprint's name and version

#### Scenario: Project-authored type

- **WHEN** an object type was authored directly in the project and does not originate from a blueprint
- **THEN** the type is shown without a blueprint-derived marker

#### Scenario: Shadowed duplicate is not listed

- **WHEN** two active schema packs declare a type of the same name and the earlier one is shadowed
- **THEN** the Schema area lists only the effective (non-shadowed) type once, and SHALL NOT render a duplicate row for the shadowed definition

#### Scenario: Navigate to owning blueprint

- **WHEN** a user follows the owning blueprint reference on a blueprint-derived type
- **THEN** the browser navigates to that blueprint's detail view

### Requirement: Warn before editing a blueprint-derived type

The UI SHALL warn the user before saving an edit to a blueprint-derived object type, stating that the edit diverges from the owning blueprint and may be overwritten when that blueprint is updated or reinstalled, and SHALL require explicit confirmation.

#### Scenario: Warning shown

- **WHEN** a user attempts to save an edit to a blueprint-derived object type
- **THEN** the UI names the owning blueprint and warns that the edit diverges from it and may be overwritten

#### Scenario: Confirmed edit proceeds

- **WHEN** the user confirms the warning
- **THEN** the edit is saved

#### Scenario: Cancelled edit aborts

- **WHEN** the user dismisses the warning
- **THEN** no change is persisted and the editor keeps the edited values

### Requirement: Show object-type schema history

The object-type detail view SHALL show the schema version history the backend exposes for the type's schema, marking the active version and showing version labels with timestamps when available.

#### Scenario: History available

- **WHEN** the backend exposes schema history for the type's schema
- **THEN** the detail view lists the versions with the active version marked and a timestamp where available

#### Scenario: History unavailable

- **WHEN** the schema history cannot be retrieved
- **THEN** the detail view still renders the type and shows a non-blocking notice that history is unavailable

#### Scenario: Single version

- **WHEN** the type's schema has only one version
- **THEN** the detail view shows the single active version without implying prior history exists

### Requirement: Authorize schema mutations

Schema-area create, edit, and install actions SHALL require the schema write capability and SHALL be rejected when the caller lacks it.

#### Scenario: Mutation without write capability

- **WHEN** a caller without the schema write capability attempts to create or edit an object type
- **THEN** the request is rejected with an authorization error and no change is persisted
