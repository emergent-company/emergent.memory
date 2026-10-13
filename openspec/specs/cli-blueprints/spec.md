# cli-blueprints Specification

## Purpose
The `memory blueprints` CLI: exporting and inspecting blueprint sources, and
installing a blueprint into a project. Installation resolves each agent's
blueprint-pinned model against the project's configured providers, so an
installed agent is served by a model the project can actually reach.

## Requirements

### Requirement: Install-time model mapping for pinned agents

`memory blueprints install` SHALL map each agent's blueprint-pinned model to a
project-servable model at install time. After Preflight passes (and before the
applier runs), the CLI SHALL compare each agent's normalized `provider/model`
reference against the project's configured providers and resolve an effective
model choice for it, rewriting the agent's model name in place so the applier
sends the resolved reference. A blueprint with no pinned models (or `--no-map`)
SHALL skip the mapping step entirely.

#### Scenario: Pinned model is servable

- **WHEN** an agent pins a `provider/model` whose provider the project has configured
- **THEN** the pinned reference is kept as-is and the applier sends it unchanged

#### Scenario: Pinned model is not servable

- **WHEN** an agent pins a `provider/model` whose provider the project has not configured
- **THEN** the CLI emits a warning naming the agent, its pin, and the project's configured models, and suggests `--map-model <agent>=<provider/model>` or `memory provider configure-project <provider>`

#### Scenario: No pinned models

- **WHEN** the blueprint's agents define no model block
- **THEN** no mapping step runs and no model-overrides are fetched or persisted

### Requirement: Resolution precedence

The CLI SHALL resolve each agent's effective model choice with the precedence
**stored override > pinned model (if servable) > project default (empty)**. A
stored override that is PRESENT wins even when its value is empty: the server
persists an empty value as an explicit "project default" choice, so a stored
empty value SHALL resolve to the project default rather than falling through to
the pinned model. Only an absent key falls through. When the project's provider
list cannot be fetched, the CLI SHALL keep a pinned model unchanged rather than
downgrading it to the project default.

#### Scenario: Stored override wins

- **WHEN** a stored per-project override exists for the agent
- **THEN** the stored value is used even over a servable pinned model

#### Scenario: Stored empty override is an explicit project default

- **WHEN** a stored override for the agent is present with an empty value
- **THEN** the project default (empty model) is used, even when the agent's pin is servable

#### Scenario: Servable pin with no override

- **WHEN** the agent has no stored override and its pin is servable
- **THEN** the pinned model is used

#### Scenario: Non-servable pin with no override

- **WHEN** the agent has no stored override and its pin is not servable
- **THEN** the project default (empty model) is used

#### Scenario: Provider list unavailable

- **WHEN** the project's provider list cannot be fetched
- **THEN** a pinned model is kept unchanged (availability unknown) rather than downgraded

### Requirement: `--map-model` and `--no-map` flags

The install command SHALL accept a repeatable `--map-model <agent>=<provider/model>`
flag and a `--no-map` flag. A `--map-model` value with an empty model SHALL select
the project default for that agent; a non-empty model SHALL be validated as a
`provider/model` reference. Any `--map-model` flag SHALL make the mapping step
non-interactive, resolving the unmapped agents by stored override > pinned >
project default. `--no-map` SHALL skip mapping and apply the blueprint's pinned
models verbatim.

#### Scenario: Explicit mapping

- **WHEN** `--map-model operator=deepseek/deepseek-v4-flash` is supplied
- **THEN** the `operator` agent is mapped to `deepseek/deepseek-v4-flash` regardless of its pin

#### Scenario: Explicit default

- **WHEN** `--map-model operator=` is supplied
- **THEN** the `operator` agent is mapped to the project default (empty model)

#### Scenario: Malformed mapping aborts

- **WHEN** `--map-model` supplies a model that is not a valid `provider/model` reference, or omits the `=` separator or agent name
- **THEN** the install fails with a clear error before the applier runs (no agent is applied, nothing is persisted)

#### Scenario: No-map

- **WHEN** `--no-map` is supplied
- **THEN** the blueprint's pinned models are applied as-is and no model-overrides are fetched or persisted

### Requirement: Interactive prompt

When no `--map-model` flag is supplied and the CLI is attached to an interactive
terminal (stdin is a TTY and not in CI), the CLI SHALL prompt for each pinned
agent, presenting a numbered selection whose first option is the project default
and whose remaining options are the project's configured generative models
(sorted and deduplicated). The prompt SHALL default to the resolved choice and
fall back to it on empty, out-of-range, or unparseable input.

#### Scenario: Prompt default

- **WHEN** the user presses enter at the prompt without entering a number
- **THEN** the resolved choice (stored override > servable pin > project default) is kept

#### Scenario: Prompt selection

- **WHEN** the user enters a valid option number
- **THEN** that model (or the project default for option 1) is applied to the agent

#### Scenario: Non-interactive context skips the prompt

- **WHEN** stdin is not a terminal or CI is set
- **THEN** no prompt is shown and the resolved choice is applied

### Requirement: Per-project persistence

After a successful, non-dry-run install in which the user made a mapping decision
(explicit `--map-model` flags or an interactive prompt), the CLI SHALL persist the
resolved agent→model choices via `PUT /api/blueprints/model-overrides` keyed by the
blueprint's project name (the manifest `name`, falling back to the source
directory base name). A purely auto-resolved mapping (non-interactive with no
flags) SHALL NOT be persisted, so a blueprint's pins are not frozen against future
upstream changes. A persistence failure SHALL be a warning, not an install failure.

#### Scenario: Explicit mapping persisted

- **WHEN** a non-dry-run install completes with an explicit `--map-model`
- **THEN** the resolved choices are written to the model-overrides store under the blueprint name

#### Scenario: Auto-resolved mapping not persisted

- **WHEN** a non-interactive install with no `--map-model` flags resolves only from the blueprint's own pins
- **THEN** no model-overrides PUT is issued

#### Scenario: Dry-run does not persist

- **WHEN** the install runs with `--dry-run`
- **THEN** no model-overrides PUT is issued

#### Scenario: Persistence failure is advisory

- **WHEN** the model-overrides PUT fails after a successful apply
- **THEN** the install still succeeds with a warning

### Requirement: Preflight remains the only abort

Model-mapping availability SHALL be advisory: a failure to list providers or
fetch stored overrides SHALL be a warning, never an abort. The offline Preflight
gate SHALL remain the only hard failure — a malformed agent model reference SHALL
abort the install before any API call or mutation, regardless of the mapping step.

#### Scenario: Malformed model still aborts

- **WHEN** an agent's pinned model is not a valid `provider/model` reference
- **THEN** the install aborts before any API request is issued, even though mapping is advisory
