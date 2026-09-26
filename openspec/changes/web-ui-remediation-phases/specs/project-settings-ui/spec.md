## MODIFIED Requirements

### Requirement: Edit the project info

The project settings page SHALL allow the user to update the project name, project info, chat prompt template, auto-extract objects, auto-merge extraction branches, and budget.

Success MAY be reported as a page message or a toast. A failure SHALL be reported in the context of the control that caused it, and only at page level when the failure has no control to attach to, so a rejected save is never indistinguishable from an untouched field.

#### Scenario: Save project changes

- **WHEN** the user submits the project info form with valid values
- **THEN** the changes persist and the page shows a success message or toast

#### Scenario: Project save rejected

- **WHEN** the user submits the project info form with an empty project name
- **THEN** the page shows an error in the context of the project-name control (or at page level if the rejection is not attributable to one control) and does not persist the change

## ADDED Requirements

### Requirement: Removing an agent definition override is confirmed

Removing an agent definition override SHALL be confirmed through the shared destructive confirmation dialog
before any request is issued, in line with every other destructive action in the gateway.

#### Scenario: Remove an override asks first

- **WHEN** the user activates the remove action on an agent definition override
- **THEN** the shared confirmation dialog is shown, and the override is deleted only after the user confirms

#### Scenario: Removal is cancelled

- **WHEN** the user dismisses the confirmation dialog without confirming
- **THEN** no removal request is sent and the override remains

### Requirement: Settings autosave shows in-flight and failure feedback

A settings control that saves without a full page submit SHALL show the user that a save is in flight and
SHALL surface a failure. This is additive to the toast feedback the `inline-form-saving` capability
already requires: the in-flight indicator and the failure surface SHALL accompany the existing toast
rather than replace it, and SHALL remain consistent with the `hx-swap="none"` autosave mechanism the e2e
coverage pins.

#### Scenario: In-flight state is visible

- **WHEN** the user edits an autosaving settings control and the request is in flight
- **THEN** the control or its row shows an in-flight indicator until the response arrives

#### Scenario: Failed autosave is surfaced

- **WHEN** an autosave request fails
- **THEN** the failure is shown in the control's own context, in addition to the error toast, and the value is not presented as saved

#### Scenario: Success feedback is unchanged

- **WHEN** an autosave request succeeds
- **THEN** the transient success toast required by `inline-form-saving` is still shown, so the existing contract is not regressed

### Requirement: Form submissions report failure in context

A form submission rejected for a reason that is not a single field's value SHALL present the failure in the
form's own context, and SHALL NOT rely on a global page-level notice alone.

#### Scenario: Non-field rejection is contextual

- **WHEN** a form submission fails for a reason that is not attributable to one field
- **THEN** the failure is shown in the form's context

#### Scenario: Field rejection identifies the field

- **WHEN** a form submission is rejected because a specific field is invalid
- **THEN** that field renders an associated error, so the user does not have to infer which input failed
