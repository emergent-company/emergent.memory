## ADDED Requirements

### Requirement: Select a search mode

The objects page SHALL present search as a split button: a primary action that runs the currently selected search mode, and an adjacent dropdown that lets the user choose the mode. Each dropdown option SHALL show the mode label and a short description of what the mode does. The modes are Unified, Hybrid, and Full-text. The mode SHALL be normalized to one of these three before it is rendered or dispatched, so the label, the submitted value, and the search that runs always agree.

#### Scenario: Default mode is Unified

- **WHEN** the objects page loads with no `mode` selected
- **THEN** the search control shows Unified as the selected mode, and submitting runs the unified search

#### Scenario: Unsupported mode value

- **WHEN** the objects page loads with a `mode` value that is not Unified, Hybrid, or Full-text
- **THEN** the control shows Unified as the selected mode and runs the unified search, rather than a label that disagrees with the search that ran

#### Scenario: Open the mode dropdown

- **WHEN** the user opens the mode dropdown
- **THEN** the control lists Unified, Hybrid, and Full-text, each with a label and a short second-line description

#### Scenario: Choose a different mode

- **WHEN** the user selects Hybrid from the dropdown
- **THEN** the selected mode becomes Hybrid and the search runs with `mode=hybrid`

#### Scenario: Run the selected mode

- **WHEN** the user submits the primary search action
- **THEN** the search runs with the currently selected mode

#### Scenario: Choose a mode without JavaScript

- **WHEN** JavaScript is disabled
- **THEN** the dropdown still opens on focus and choosing a mode (or submitting the primary action) still runs the search, because every mode is a plain form submit button rather than a scripted action
