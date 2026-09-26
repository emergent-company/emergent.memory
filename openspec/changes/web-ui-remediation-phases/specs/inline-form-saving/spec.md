## Purpose

Records that the settings autosave feedback introduced by this change is additive to the toast feedback
this capability already owns, so the two are not read as competing contracts.

## MODIFIED Requirements

### Requirement: Show transient success feedback

The form SHALL show a transient toast notification confirming a successful inline save, and the toast SHALL dismiss on its own.

A field-level success or in-flight indicator MAY accompany the toast for a control that saves without a page submit, and SHALL NOT replace it.

#### Scenario: Save succeeds

- **WHEN** an inline save succeeds
- **THEN** a success toast is shown and dismisses automatically without further user action

#### Scenario: An accompanying field indicator does not replace the toast

- **WHEN** an inline save for a settings control succeeds and that control also renders an in-flight or success indicator
- **THEN** the success toast is still shown

### Requirement: Show error feedback on failure

The form SHALL show a toast notification with the error when an inline save fails and SHALL NOT persist the change.

An additional in-context error surface MAY accompany the toast for a control that saves without a page submit, and SHALL NOT replace it.

#### Scenario: Save fails

- **WHEN** an inline save fails
- **THEN** an error toast is shown and the change is not persisted

#### Scenario: An accompanying in-context error does not replace the toast

- **WHEN** an inline save for a settings control fails and that control also renders an in-context error
- **THEN** the error toast is still shown
