## MODIFIED Requirements

### Requirement: Agent model references are validated as structured references

Every agent-definition write path — REST create/update, the MCP
`create_agent_definition` / `update_agent_definition` tools, project agent
overrides, and blueprint apply — SHALL validate `model.name` through one shared
funnel (`agents.NormalizeAgentModelConfig`, built on `modelref.ParseWithProvider`
and `modelref.Parse`), so all paths accept and reject the same references.

A reference SHALL be accepted in either of two forms:

- the canonical prefixed form `provider/model`, splitting on the first `/` and
  preserving any slashes in the model segment; or
- a structured bare `model.name` together with a separate `model.provider` slug.

The provider segment and the model segment MUST both be non-empty. The model
segment MUST NOT begin with `/`, so a doubled separator such as
`openai//gpt-4o` is rejected. When both a prefix and a structured provider are
present they MUST agree; a structured provider without a model name is rejected;
a provider slug MUST NOT itself contain `/`. A stored value SHALL be normalized
to the canonical `provider/model` form with `model.provider` cleared, so exactly
one representation is persisted. An empty `model` (no override) SHALL remain
valid and means "use the project default". A malformed value already stored in a
legacy project override MUST NOT be applied to a definition at run time.

#### Scenario: Malformed empty-provider reference is rejected

- **WHEN** a create or update request sets `model.name` to `/deepseek-v4-flash`
- **THEN** the request is rejected with a 400 naming the expected `provider/model` form
- **AND** no definition is written

#### Scenario: Reference with no provider segment is rejected

- **WHEN** a create or update request sets a bare `model.name` of `deepseek-v4-flash` with no `model.provider`
- **THEN** the request is rejected with a 400

#### Scenario: Reference with an empty model segment is rejected

- **WHEN** a create or update request sets `model.name` to `openai/`
- **THEN** the request is rejected with a 400

#### Scenario: Doubled separator is rejected

- **WHEN** a create or update request sets `model.name` to `openai//gpt-4o`
- **THEN** the request is rejected with a 400
- **AND** the malformed reference is not persisted

#### Scenario: Structured provider and bare name are accepted

- **WHEN** a request sets `model.name` to `gpt-4o` and `model.provider` to `openai`
- **THEN** the definition is written with the canonical `openai/gpt-4o` reference
- **AND** `model.provider` is cleared

#### Scenario: Conflicting providers are rejected

- **WHEN** a request sets `model.name` to `openai/gpt-4o` and `model.provider` to `google`
- **THEN** the request is rejected with a 400

#### Scenario: MCP tools validate like REST

- **WHEN** the MCP `create_agent_definition` or `update_agent_definition` tool is called with a malformed model reference
- **THEN** it returns the same `provider/model` validation error instead of writing the value

#### Scenario: Project override validates like REST

- **WHEN** `PUT .../agent-definitions/overrides/:agentName` sets a malformed `model.name`
- **THEN** the request is rejected with a 400 and no override is persisted

#### Scenario: Blueprint apply validates like REST

- **WHEN** a blueprint manifest sets `agents[].model.name` to a malformed reference
- **THEN** the apply is rejected with a 400
- **AND** no definition is written with the malformed value

#### Scenario: Blueprint apply canonicalizes a valid reference

- **WHEN** a blueprint manifest sets `agents[].model.name` to ` openai / gpt-4o `
- **THEN** the definition is written with the canonical `openai/gpt-4o` reference

#### Scenario: Malformed stored override is not applied

- **WHEN** a legacy project override persisted before validation holds a malformed `model.name`
- **THEN** applying it to a definition leaves the definition's valid model unchanged

#### Scenario: Well-formed reference is accepted

- **WHEN** a create or update request sets `model.name` to `openai/deepseek-v4-flash`
- **THEN** the definition is written with that canonical reference

#### Scenario: No override is allowed

- **WHEN** a request omits `model` or sends an empty `model.name`
- **THEN** the definition is written with no model override and inherits the project default
