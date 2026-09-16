## Purpose

A reusable pattern for gateway forms that persists individual field values automatically as the user changes them, surfaces success and error feedback as toast notifications, and retains an explicit Save button only where auto-save would be unsafe or inappropriate.

## ADDED Requirements

### Requirement: Auto-save an independent field on change

The form SHALL persist a field's value automatically when the user changes the field and the value is complete and valid, without requiring a separate Save action.

#### Scenario: Field changed

- **WHEN** the user changes an independent field to a complete, valid value
- **THEN** the new value is persisted without the user pressing a Save button

### Requirement: Debounce continuous text input

The form SHALL debounce saves for continuous text input so a save is not issued on every keystroke.

#### Scenario: Typing a text value

- **WHEN** the user types several characters in a row in a text field
- **THEN** the value is saved only after the user pauses, not after each keystroke

### Requirement: Show transient success feedback

The form SHALL show a transient toast notification confirming a successful inline save, and the toast SHALL dismiss on its own.

#### Scenario: Save succeeds

- **WHEN** an inline save succeeds
- **THEN** a success toast is shown and dismisses automatically without further user action

### Requirement: Show error feedback on failure

The form SHALL show a toast notification with the error when an inline save fails and SHALL NOT persist the change.

#### Scenario: Save fails

- **WHEN** an inline save fails
- **THEN** an error toast is shown and the change is not persisted

### Requirement: Save bound fields as an atomic unit

The form SHALL save fields that depend on one another together as a single unit and SHALL NOT save them individually.

#### Scenario: A member of a bound set is edited

- **WHEN** the user edits one member of a set of bound fields
- **THEN** the whole set is saved together atomically as one change

### Requirement: Validate bound fields before saving

The form SHALL validate the combined values of bound fields locally before saving and SHALL NOT send a save request while the combination is invalid.

#### Scenario: Invalid combination

- **WHEN** the combined values of bound fields are invalid, for example a minimum value greater than its maximum
- **THEN** no save request is sent and an error toast is shown

### Requirement: Retain a Save button only where auto-save is unsafe

The form SHALL keep a dedicated Save button only for actions that are unsafe or inappropriate to auto-save, such as destructive or irreversible actions. Bound fields that must be committed together are saved as an atomic group rather than gated behind a Save button.

#### Scenario: Destructive action

- **WHEN** an action is destructive or irreversible
- **THEN** that action retains a dedicated Save button and is not triggered by auto-save

### Requirement: Do not auto-save invalid or incomplete values

The form SHALL NOT auto-save a value that is invalid or incomplete, such as a non-numeric value in a numeric field, and SHALL surface an error instead.

#### Scenario: Invalid value entered

- **WHEN** the user enters an invalid value in a field
- **THEN** the value is not persisted and an error toast is shown
