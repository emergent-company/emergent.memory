## REMOVED Requirements

### Requirement: Type-scope control on the agent General form

## ADDED Requirements

### Requirement: Type-scope control on the agent Graph subpage

The agent **Graph** settings subpage (`/agents/:id/settings/graph`) SHALL expose
a type-scope control that lets an operator set the agent definition's
object-type and relationship-type allowlists. Saving a non-empty selection SHALL
persist `Config["objectTypes"]` / `Config["relationshipTypes"]` to the
corresponding lists; leaving the selection empty (present but blank) SHALL remove
the keys, meaning "all types". The control SHALL be lossless across partial
section saves: the rendered form SHALL submit each allowlist field via a hidden
companion input so that clearing every box ("allow all types") is
distinguishable from a partial body that omits the field ("leave the stored
scope unchanged"). The General form SHALL NOT own these fields.

#### Scenario: Selecting types persists the allowlists

- **WHEN** an operator selects object types and relationship types on the Graph subpage and saves
- **THEN** the saved agent definition's `config.objectTypes` and `config.relationshipTypes` hold the selected lists

#### Scenario: Clearing the selection removes the keys

- **WHEN** an operator unchecks every box in the type-scope control and saves the Graph subpage
- **THEN** the submitted body carries each field present-but-blank, and the saved agent definition's `config` has no `objectTypes` or `relationshipTypes` key, meaning "all types"

#### Scenario: Omitting the field preserves the stored scope

- **WHEN** a save reaches the Graph applier without carrying the `objectType` / `relationshipType` field
- **THEN** the stored allowlists are left unchanged (and every unrelated config key is untouched)

#### Scenario: General save leaves the graph scope intact

- **WHEN** an operator saves the General settings form
- **THEN** the agent's `config.objectTypes` and `config.relationshipTypes` are unaffected
