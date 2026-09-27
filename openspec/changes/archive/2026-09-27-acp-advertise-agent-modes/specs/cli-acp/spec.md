## ADDED Requirements

### Requirement: Agent modes advertised on session/new

The CLI SHALL advertise the project's selectable agents on `session/new`. When it has a non-empty agent list (from the extended AgentCard), the response SHALL carry a `modes` object whose `availableModes` lists one entry per agent (mode `id` = skill slug, `name` = skill name, `description` = skill description), and whose `currentModeId` is the configured default skill. The CLI SHALL also carry a `configOptions` array with a single `select` option (`id` `"agent"`, `category` `"model"`, `type` `"select"`) whose `options` list the same agents and whose `currentValue` is the default skill.

The advertised set SHALL be exactly the external-reachable agents from the AgentCard. The configured default skill SHALL be advertised only when it appears in that list (i.e. is genuinely external-reachable); an `internal`, `project`-visibility, or stale/deleted default SHALL NOT be prepended as a selectable mode.

When no agent list is available (the card could not be fetched or lists no agents), the CLI SHALL degrade to a single mode (the configured default skill) so every session has a valid current mode.

#### Scenario: Modes reflect the project's agents

- **WHEN** the agent is constructed with a list of two skills and the client sends `session/new`
- **THEN** the response carries `modes.currentModeId` equal to the default skill and `modes.availableModes` with the two skills, each carrying its `id`, `name`, and `description`

#### Scenario: A single default mode is always advertised

- **WHEN** the agent is constructed with no agent list and the client sends `session/new`
- **THEN** the response carries a `modes` object with `currentModeId` equal to the default skill and a single `availableModes` entry for that skill

#### Scenario: An unreachable default is not advertised

- **WHEN** the agent is constructed with a non-empty list of skills that does not include the configured default skill
- **THEN** `session/new` advertises only those skills in `availableModes` and does not add the default skill

### Requirement: Mode and config-option selection

The CLI SHALL handle `session/set_mode` and `session/set_config_option` by switching the session's target agent. It SHALL validate the requested value against the advertised agent list and reject unknown values, missing `sessionId`/`modeId`/`configId` with a JSON-RPC error.

For `session/set_mode` the result SHALL be an empty object (per the ACP schema, `SetSessionModeResponse` has no fields) and the new mode SHALL be delivered to the client via a `current_mode_update` session notification. For `session/set_config_option` the result SHALL be the updated `configOptions` state.

#### Scenario: set_mode selects an advertised agent

- **WHEN** the client sends `session/set_mode` with a `modeId` that is one of the advertised modes for an existing session
- **THEN** the CLI records that skill for the session, responds with an empty result object, and emits a `current_mode_update` notification whose `currentModeId` equals the selected mode

#### Scenario: set_mode rejects an unknown mode

- **WHEN** the client sends `session/set_mode` with a `modeId` not in `availableModes`
- **THEN** the CLI returns a JSON-RPC error and leaves the session's skill unchanged

#### Scenario: set_config_option selects an agent

- **WHEN** the client sends `session/set_config_option` with `configId` `"agent"` and a value equal to one of the advertised agents
- **THEN** the CLI records that skill for the session and returns the updated `configOptions` state with `currentValue` equal to the selected value

#### Scenario: prompt routes to the selected agent

- **WHEN** a session's skill has been changed via `session/set_mode` or `session/set_config_option`, and a later `session/prompt` arrives on that session
- **THEN** the A2A message carries `metadata.skillId` equal to the selected skill, not the default skill
