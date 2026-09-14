## ADDED Requirements

### Requirement: Save coupled voice fields as a group

The Voice section SHALL save voice fields that depend on one another together as a single unit. The endpoint minimum and maximum delays SHALL be validated together so the minimum is never greater than the maximum, and the text-to-speech provider, model, and voice SHALL be saved together, with the model and voice cleared when the provider no longer uses them.

#### Scenario: Endpoint minimum exceeds maximum

- **WHEN** the user enters an endpoint minimum delay greater than the endpoint maximum delay
- **THEN** neither value is persisted and an error toast is shown

#### Scenario: Provider change clears unused model and voice

- **WHEN** the user selects a text-to-speech provider that does not use a model or voice
- **THEN** the model and voice are cleared and saved together with the provider

## MODIFIED Requirements

### Requirement: Enable or disable voice

The Voice section SHALL include a master toggle that enables or disables voice for the project, persisting the change immediately.

#### Scenario: Disable voice

- **WHEN** the user turns the master toggle off
- **THEN** the disabled state persists immediately and voice is turned off for the project

#### Scenario: Enable voice

- **WHEN** the user turns the master toggle on
- **THEN** the enabled state persists immediately and voice is turned on for the project

### Requirement: Configure the text-to-speech provider

The Voice section SHALL allow the user to select the text-to-speech provider and configure its model and voice, persisting each change immediately.

#### Scenario: Save text-to-speech settings

- **WHEN** the user selects a text-to-speech provider or edits its model or voice
- **THEN** the values persist immediately and a success toast is shown

### Requirement: Configure the speech-to-text provider

The Voice section SHALL allow the user to select the speech-to-text provider and configure its model and language, persisting each change immediately.

#### Scenario: Save speech-to-text settings

- **WHEN** the user selects a speech-to-text provider or edits its model or language
- **THEN** the values persist immediately and a success toast is shown

### Requirement: Configure the remaining voice options

The Voice section SHALL allow the user to configure the LiveKit endpoint, exit keywords, goodbye text, away timeout, endpoint delays, and the interruption and preemptive-generation toggles, persisting each change immediately.

#### Scenario: Save voice options

- **WHEN** the user edits any of these voice options
- **THEN** the values persist immediately and a success toast is shown

### Requirement: Handle invalid voice values

The Voice section SHALL reject invalid values and persist nothing when validation fails.

#### Scenario: Invalid away timeout or endpoint delay

- **WHEN** the user enters a non-numeric away timeout or endpoint delay
- **THEN** an error toast is shown and the change is not persisted
