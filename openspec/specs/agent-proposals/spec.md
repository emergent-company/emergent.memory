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

When a question carries a `proposal`, the conversation UI SHALL render it as a proposal card showing the proposed object types and relationship types (name, label, typed properties) rather than a raw JSON/YAML block.

#### Scenario: Object and relationship types are previewed
- **WHEN** a question carries a `blueprint` proposal
- **THEN** the card SHALL list the proposed object types with their properties and the proposed relationship types with their source and target
- **THEN** the card SHALL NOT present the manifest as an unformatted code fence

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
