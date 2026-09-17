## ADDED Requirements

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
