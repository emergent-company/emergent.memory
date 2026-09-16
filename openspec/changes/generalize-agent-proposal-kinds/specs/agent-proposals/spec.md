## MODIFIED Requirements

### Requirement: Proposal renders as a structured card

When a question carries a `proposal`, the conversation UI SHALL render it as a proposal card selected by the proposal's `kind`, rather than a raw JSON/YAML block. A `kind` without a dedicated renderer SHALL render a summary-only card (kind badge + summary text) and SHALL NOT error.

#### Scenario: Object and relationship types are previewed
- **WHEN** a question carries a `blueprint` proposal
- **THEN** the card SHALL list the proposed object types with their properties and the proposed relationship types with their source and target
- **THEN** the card SHALL NOT present the manifest as an unformatted code fence

#### Scenario: Unknown kind degrades to a summary card
- **WHEN** a question carries a `proposal` whose `kind` has no dedicated renderer
- **THEN** the card SHALL show the kind and the proposal `summary`
- **THEN** the question SHALL still be answerable and SHALL NOT error

### Requirement: Proposal card shows a side-effect summary

The proposal card SHALL surface a concise summary of what applying the proposal adds, changes, or removes, computed against the project's current state for the kinds that carry a current state (schema types, agent definitions, skills, MCP servers).

#### Scenario: Additions are summarized
- **WHEN** a question carries a `blueprint` proposal with object or relationship types
- **THEN** the card header SHALL indicate the counts of object types and relationship types the proposal adds

#### Scenario: Diff against current state
- **WHEN** a proposal's contents can be compared against the project's current state
- **THEN** the card header SHALL indicate the added / changed / removed counts (for example "adds 2 object types, changes 1 agent")

#### Scenario: No prior state
- **WHEN** the project has no current state to diff against
- **THEN** the summary SHALL describe the additions from the proposal body alone

## ADDED Requirements

### Requirement: Proposal kinds are extensible via a kind registry

The gateway SHALL resolve a proposal's `kind` through an extensible registry of kind renderers (body parser + card renderer). Adding a kind SHALL be an additive, isolated change that does not alter the envelope, the `ask_user` tool contract, or the behavior of existing kinds.

#### Scenario: New kind is additive
- **WHEN** a new `kind` renderer is registered
- **THEN** existing kinds and the summary-only fallback SHALL behave unchanged
- **THEN** the `ask_user` envelope (`kind`, `summary`, `body`) SHALL remain unchanged

### Requirement: First-class proposal kinds for writable resources

The proposal card SHALL provide first-class renderers for the operator's writable resources, each using the same body shape the corresponding write tool consumes:

- `skill` — `{name, description, prompt, tools, bannedTools}`
- `agent` — `{name, description, model, systemPrompt, tools, skills, bannedTools, flowType, visibility}`
- `mcp_server` — `{name, type, url, headers, enabled, enabledTools, disabledTools}`
- `provider` — `{provider, baseUrl, models}` (secret masked, never echoed)
- `object` — `{entities, relationships}`

#### Scenario: Skill proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `skill`
- **THEN** the card SHALL show the skill's name, description, and prompt, and its tool/banned-tool lists

#### Scenario: Agent proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `agent`
- **THEN** the card SHALL show the agent's name, model, and system prompt, and its tool/skill/banned-tool lists

#### Scenario: MCP server proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `mcp_server`
- **THEN** the card SHALL show the server's name, type, URL, and the tools it enables/disables

#### Scenario: Provider proposal masks the secret
- **WHEN** a question carries a `proposal` whose `kind` is `provider`
- **THEN** the card SHALL show the provider slug, base URL, and models, and SHALL NOT render the API key

#### Scenario: Object proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `object`
- **THEN** the card SHALL list the proposed entities with their type and properties and the proposed relationships with their source and target
