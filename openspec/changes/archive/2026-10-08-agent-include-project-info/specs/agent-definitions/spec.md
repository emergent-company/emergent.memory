## ADDED Requirements

### Requirement: Per-agent project-context opt-in flag

The agent-definition read and write contract SHALL expose a boolean
`includeProjectInfo` field (config key `includeProjectInfo`) that controls
whether project-level context is injected at run time. The field SHALL default
to `false` when omitted. It SHALL be accepted on create and update and SHALL be
returned on read. When set to `false`, the key SHALL be removed from the stored
config so a default definition carries no redundant entry.

#### Scenario: Create with the flag enabled

- **WHEN** a create request sets `includeProjectInfo` to `true`
- **THEN** the stored definition has `config.includeProjectInfo = true` and a subsequent read returns `includeProjectInfo: true`

#### Scenario: Create omitting the flag defaults to off

- **WHEN** a create request omits `includeProjectInfo`
- **THEN** the stored definition has no `config.includeProjectInfo` key and a subsequent read returns `includeProjectInfo: false`

#### Scenario: Update toggles the flag

- **WHEN** an update request sets `includeProjectInfo` to `true` on a definition that was off
- **THEN** `config.includeProjectInfo` becomes `true`, and a later update setting it to `false` removes the key again

#### Scenario: Update omitting the flag leaves it unchanged

- **WHEN** an update request omits both `config` and `includeProjectInfo`
- **THEN** the previously stored `includeProjectInfo` value is unchanged

#### Scenario: The typed field overrides the raw config key

- **WHEN** an update request supplies both a `config` object and an explicit `includeProjectInfo`
- **THEN** the explicit `includeProjectInfo` value is stored in `config`
