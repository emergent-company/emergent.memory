## ADDED Requirements

### Requirement: Type-scope control on the agent General form

The agent **General** settings form SHALL expose a type-scope control that lets
an operator set the agent definition's object-type and relationship-type
allowlists. Saving a non-empty selection SHALL persist
`Config["objectTypes"]` / `Config["relationshipTypes"]` to the corresponding
lists; leaving the selection empty SHALL remove the keys. The control SHALL be
lossless across partial section saves, like the form's other General fields.

#### Scenario: Selecting types persists the allowlists

- **WHEN** an operator selects object types and relationship types and saves the General settings form
- **THEN** the saved agent definition's `config.objectTypes` and `config.relationshipTypes` hold the selected lists

#### Scenario: Clearing the selection removes the keys

- **WHEN** an operator clears the type-scope control and saves the General settings form
- **THEN** the saved agent definition's `config` has no `objectTypes` or `relationshipTypes` key, meaning "all types"

#### Scenario: Auto-save preserves the control

- **WHEN** the General form auto-saves with the type-scope control in a given state
- **THEN** the resulting `config.objectTypes` / `config.relationshipTypes` match that state and no other config key is affected
