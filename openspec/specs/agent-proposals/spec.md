# agent-proposals Specification

## Purpose
Defines the structured proposal payload an agent attaches to its `ask_user` checkpoint, and how the conversation UI renders it as a reviewable proposal card (side-effect summary + object/relationship previews + accept/reject actions) instead of a raw JSON code fence.

## Requirements

### Requirement: ask_user accepts a structured proposal

The `ask_user` tool SHALL accept an optional `proposal` argument carrying a typed payload with a `kind` discriminator, a human-readable `summary`, and a `body` describing the proposed change. The payload SHALL be persisted with the question and surfaced to the conversation UI.

#### Scenario: Blueprint proposal carries manifest types
- **WHEN** an agent calls `ask_user` with a `proposal` whose `kind` is `blueprint` (a schema pack)
- **THEN** the `body` SHALL carry object types and relationship types using the same manifest type definitions the blueprint apply path consumes

#### Scenario: Proposal is persisted with the question
- **WHEN** an agent calls `ask_user` with a valid `proposal`
- **THEN** the proposal SHALL be stored alongside the question and delivered with the question event to the conversation UI

### Requirement: Invalid or absent proposal degrades to plain text

A missing or invalid `proposal` SHALL NOT fail the run. The question SHALL be presented as plain text exactly as it is today.

#### Scenario: Invalid proposal does not error
- **WHEN** an agent supplies a `proposal` that fails to parse or validate against the expected shape
- **THEN** the tool SHALL drop the structured payload and proceed with a plain-text question
- **THEN** the run SHALL NOT return a tool error

#### Scenario: Question without a proposal
- **WHEN** an agent calls `ask_user` without a `proposal`
- **THEN** the question renders as markdown, unchanged from current behaviour

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

The proposal card SHALL surface a concise summary of what applying the proposal adds, derived from the proposal body (for example "Adds 2 object types, 3 relationship types").

#### Scenario: Additions are summarized
- **WHEN** a question carries a `blueprint` proposal with object or relationship types
- **THEN** the card header SHALL indicate the counts of object types and relationship types the proposal adds

### Requirement: Proposal actions reuse the question respond path

The proposal card SHALL present Accept and Reject actions that use the existing question respond/cancel flow (`questionId` + `/respond`). Free-text revision is provided via a question with `interaction_type` `text` rather than a dedicated edit action on a buttons card.

#### Scenario: Accept / Reject
- **WHEN** a user accepts or rejects a proposal card
- **THEN** the action SHALL be delivered through the same question respond path as a plain-text question

#### Scenario: Free-text revision uses a text question
- **WHEN** a proposal requires free-form input from the user
- **THEN** the agent SHALL use a question with `interaction_type` `text` so the reply flows through the same respond path

### Requirement: Backward compatibility

Questions without a `proposal` SHALL be rendered and answered identically to today. The question respond and cancel plumbing SHALL remain unchanged.

#### Scenario: Legacy question unchanged
- **WHEN** a question has no `proposal`
- **THEN** it SHALL render and respond exactly as before this change

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
- `mcp_server` — `{name, type, url, headers, enabled, enabledTools, disabledTools}` (write-path shape; `headers` intentionally omitted from the rendered card)
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

#### Scenario: MCP server proposal masks auth headers
- **WHEN** a question carries a `proposal` whose `kind` is `mcp_server` and whose body includes `headers`
- **THEN** the card SHALL NOT render the `headers` field or any auth credentials it carries

#### Scenario: Provider proposal masks the secret
- **WHEN** a question carries a `proposal` whose `kind` is `provider`
- **THEN** the card SHALL show the provider slug, base URL, and models, and SHALL NOT render the API key

#### Scenario: Object proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `object`
- **THEN** the card SHALL list the proposed entities with their type and properties and the proposed relationships with their source and target

### Requirement: Fenced blueprint manifest renders as a proposal card fallback

When a question has no structured `proposal`, the conversation UI SHALL derive a proposal card from a fenced blueprint manifest embedded in the question text, and SHALL degrade to plain markdown when no manifest is derivable. The structured `proposal` envelope SHALL remain the primary path; this fallback is presentation-only.

#### Scenario: Fenced manifest renders a card
- **WHEN** an agent calls `ask_user` without a `proposal` and the question text embeds a `json`/`yaml`/`yml` fence that decodes to a blueprint manifest (a `packs` array or a bare pack with `objectTypes`/`relationshipTypes`)
- **THEN** the question SHALL render the same proposal card as a structured `blueprint` proposal (object type and relationship type previews)
- **THEN** the manifest SHALL NOT render as an unformatted code fence

#### Scenario: No derivable manifest degrades to markdown
- **WHEN** a question has no `proposal` and no recognizable fenced manifest (no `json`/`yaml`/`yml` fence, a non-manifest fence, malformed content, or a manifest with zero types)
- **THEN** the question SHALL render as markdown, unchanged from current behaviour

#### Scenario: Structured proposal wins over a fence
- **WHEN** a question carries both a structured `proposal` that renders a card and a fenced manifest in its text
- **THEN** the structured `proposal` card SHALL render and the fence SHALL NOT produce a second card

#### Scenario: A present-but-empty proposal still falls back
- **WHEN** a question carries a structured `proposal` that does not render a card (`null`, malformed, or an empty body) and a fenced manifest in its text
- **THEN** the fenced manifest SHALL render the proposal card, identically at the live and history render sites

### Requirement: ask_user advertises the optional proposal argument

The `ask_user` tool SHALL document its optional `proposal` argument in its tool description so agents can discover the structured path and avoid pasting a raw manifest fence into the question text.

#### Scenario: Tool description names the proposal argument
- **WHEN** an agent inspects the `ask_user` tool schema
- **THEN** the description SHALL mention the optional `proposal` argument, its envelope (`kind`, `summary`, `body`), and the `blueprint` body shape (`objectTypes`, `relationshipTypes`)
