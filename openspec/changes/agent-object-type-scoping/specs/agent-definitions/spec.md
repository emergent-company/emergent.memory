## ADDED Requirements

### Requirement: Per-agent type-scope allowlists

The agent-definition read and write contract SHALL expose two string-array
fields — `objectTypes` and `relationshipTypes` (config keys `objectTypes` /
`relationshipTypes`) — that bound the object types and relationship types an
agent may see. Each field SHALL default to empty when omitted, and an empty
array SHALL mean "all types". The fields SHALL be accepted on create and update
and SHALL be returned on read. Readers SHALL be nil-safe, returning an empty
list when the key is absent. When a list is set to empty, the key SHALL be
removed from the stored config so a default definition carries no redundant
entry. No database migration SHALL be required; the values SHALL live in the
existing `config` JSONB blob.

#### Scenario: Create with allowlists persists them

- **WHEN** a create request sets `objectTypes` to `["Law","Case"]` and `relationshipTypes` to `["cites"]`
- **THEN** the stored definition has `config.objectTypes = ["Law","Case"]` and `config.relationshipTypes = ["cites"]`, and a subsequent read returns both

#### Scenario: Create omitting the fields defaults to all types

- **WHEN** a create request omits `objectTypes` and `relationshipTypes`
- **THEN** the stored definition has no `objectTypes` or `relationshipTypes` key and a subsequent read returns empty lists (all types)

#### Scenario: Update sets the allowlists

- **WHEN** an update request sets `objectTypes` to a non-empty list on a definition that was empty
- **THEN** `config.objectTypes` becomes that list, and a later update setting it to an empty list removes the key again

#### Scenario: Update omitting the fields leaves them unchanged

- **WHEN** an update request omits both `config` and `objectTypes`/`relationshipTypes`
- **THEN** the previously stored allowlists are unchanged

#### Scenario: The typed fields override the raw config keys

- **WHEN** an update request supplies both a `config` object and an explicit `objectTypes`/`relationshipTypes`
- **THEN** the explicit typed values are stored in `config`

#### Scenario: Readers are nil-safe

- **WHEN** a stored definition has no `objectTypes` or `relationshipTypes` key (absent or null)
- **THEN** the readers return an empty list, not an error
