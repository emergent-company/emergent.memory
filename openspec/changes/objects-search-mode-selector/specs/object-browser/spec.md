## ADDED Requirements

### Requirement: Select a search mode

The objects page SHALL present search as a split button: a primary action that runs the currently selected search mode, and an adjacent dropdown that lets the user choose the mode. Each dropdown option SHALL show the mode label and a short description of what the mode does. The modes are Unified, Hybrid, and Full-text.

#### Scenario: Default mode is Unified

- **WHEN** the objects page loads with no `mode` selected
- **THEN** the search control shows Unified as the selected mode, and submitting runs the unified search

#### Scenario: Open the mode dropdown

- **WHEN** the user opens the mode dropdown
- **THEN** the control lists Unified, Hybrid, and Full-text, each with a label and a short second-line description

#### Scenario: Choose a different mode

- **WHEN** the user selects Hybrid from the dropdown
- **THEN** the selected mode becomes Hybrid and the search runs with `mode=hybrid`

#### Scenario: Run the selected mode

- **WHEN** the user submits the primary search action
- **THEN** the search runs with the currently selected mode
