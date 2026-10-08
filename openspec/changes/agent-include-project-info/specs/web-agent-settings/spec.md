## ADDED Requirements

### Requirement: Project-context opt-in control on the agent General form

The agent **General** settings form SHALL expose a boolean control for the
agent definition's project-context opt-in (`includeProjectInfo`). Checking it
SHALL persist `Config["includeProjectInfo"] = true`; leaving it unchecked SHALL
remove the key. The control SHALL be lossless across partial section saves, like
the form's other General fields.

#### Scenario: Checking the control enables injection

- **WHEN** an operator checks the project-context control and saves the General settings form
- **THEN** the saved agent definition's `config.includeProjectInfo` is `true`

#### Scenario: Unchecking the control disables injection

- **WHEN** an operator unchecks the project-context control and saves the General settings form
- **THEN** the saved agent definition's `config` has no `includeProjectInfo` key

#### Scenario: Auto-save preserves the control

- **WHEN** the General form auto-saves with the control in a given state
- **THEN** the resulting `config.includeProjectInfo` matches that state and no other config key is affected
