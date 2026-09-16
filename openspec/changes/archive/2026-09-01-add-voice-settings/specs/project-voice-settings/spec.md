## Purpose

A Voice settings section on the Project Settings page that lets a user configure text-to-speech and speech-to-text providers plus other voice options, toggle voice on or off for the project, and have the chat UI hide its voice controls whenever voice is disabled.

## ADDED Requirements

### Requirement: Show the Voice settings section

The Project Settings page SHALL include a Voice section that displays the project's voice configuration and the master enable toggle.

#### Scenario: Voice section present

- **WHEN** the Project Settings page loads
- **THEN** a Voice section is shown with the master enable/disable toggle and the editable voice fields

### Requirement: Enable or disable voice

The Voice section SHALL include a master toggle that enables or disables voice for the project.

#### Scenario: Disable voice

- **WHEN** the user turns the master toggle off and saves
- **THEN** the disabled state persists and voice is turned off for the project

#### Scenario: Enable voice

- **WHEN** the user turns the master toggle on and saves
- **THEN** the enabled state persists and voice is turned on for the project

### Requirement: Configure the text-to-speech provider

The Voice section SHALL allow the user to select the text-to-speech provider and configure its model and voice.

#### Scenario: Save text-to-speech settings

- **WHEN** the user selects a text-to-speech provider and edits its model or voice and saves
- **THEN** the values persist and the page shows a success message

### Requirement: Configure the speech-to-text provider

The Voice section SHALL allow the user to select the speech-to-text provider and configure its model and language.

#### Scenario: Save speech-to-text settings

- **WHEN** the user selects a speech-to-text provider and edits its model or language and saves
- **THEN** the values persist and the page shows a success message

### Requirement: Show provider secrets as set or not set

The Voice section SHALL display provider secrets (text-to-speech API key, speech-to-text API key, and LiveKit API secret) only as a set/not-set state, never as their plaintext values, and SHALL NOT let the user edit them in the UI.

#### Scenario: Secrets displayed as set or not set

- **WHEN** the Voice section loads
- **THEN** each provider secret is shown as either set or not set, with no plaintext value and no edit field

### Requirement: Configure the remaining voice options

The Voice section SHALL allow the user to configure the LiveKit endpoint, exit keywords, goodbye text, away timeout, endpoint delays, and the interruption and preemptive-generation toggles.

#### Scenario: Save voice options

- **WHEN** the user edits any of these voice options and saves
- **THEN** the values persist and the page shows a success message

### Requirement: Show unset voice fields as not set

The Voice section SHALL show a clear "not set" state for any optional voice field that has no stored or default value.

#### Scenario: Optional voice field absent

- **WHEN** an optional voice field is not set
- **THEN** that field is shown as a clear "not set" state

### Requirement: Hide voice controls when voice is disabled

The chat page SHALL omit its voice controls when voice is disabled for the project.

#### Scenario: Voice disabled

- **WHEN** the chat page loads and voice is disabled
- **THEN** the call button, mute button, mic level meter, voice status readout, audio element, and voice client script are not rendered, and the UI presents a text-only chat

#### Scenario: Voice enabled

- **WHEN** the chat page loads and voice is enabled
- **THEN** the voice controls are rendered and the voice client script is included

### Requirement: Handle invalid voice values

The Voice section SHALL reject invalid values and persist nothing when validation fails.

#### Scenario: Invalid away timeout or endpoint delay

- **WHEN** the user submits a non-numeric away timeout or endpoint delay
- **THEN** the page shows an error and does not persist the change

### Requirement: Handle an unreachable memory service

The Voice section SHALL show an error state when the Memory service cannot be reached, without crashing the rest of the Project Settings page.

#### Scenario: Memory unreachable

- **WHEN** the Voice section loads and the Memory service is unreachable
- **THEN** an error message is shown and the section does not present stale values as authoritative
