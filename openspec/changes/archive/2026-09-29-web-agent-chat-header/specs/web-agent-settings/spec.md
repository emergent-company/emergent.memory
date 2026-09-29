## ADDED Requirements

### Requirement: Short description field on the agent General settings form

The agent General settings form SHALL expose an editable **Short description**
field bound to the definition's existing `description`, saved through the
general-settings path. The value SHALL be trimmed, and an empty value SHALL
clear the stored description. The field SHALL carry a hint that it is shown
under the agent's name in chat.

#### Scenario: Save a short description

- **WHEN** the owner enters a description on the General settings form and saves
- **THEN** the agent definition's `description` is persisted and shown under the agent's name in chat

#### Scenario: Clear the description

- **WHEN** the owner clears the field and saves
- **THEN** the stored `description` becomes empty and chat shows no description line
