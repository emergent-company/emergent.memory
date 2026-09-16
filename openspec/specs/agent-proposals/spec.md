# agent-proposals Specification

## Purpose
Defines the structured proposal payload an agent attaches to its `ask_user` checkpoint, and how the conversation UI renders it as a reviewable proposal card (side-effect summary + object/relationship previews + accept/reject/edit) instead of a raw JSON code fence.

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

The proposal card SHALL surface a concise summary of what applying the proposal would add, change, or remove, computed against the project's current state where that state is available.

#### Scenario: Diff against current types
- **WHEN** a proposal's object/relationship types can be compared against the project's currently compiled types
- **THEN** the card header SHALL indicate the added / changed / removed counts (for example "adds 2 object types, 3 relationship types")

#### Scenario: No prior state
- **WHEN** the project has no current types to diff against
- **THEN** the summary SHALL describe the additions from the proposal body alone

### Requirement: Proposal actions reuse the question respond path

The proposal card SHALL present Accept, Reject, and Edit actions that use the existing question respond/cancel flow (`questionId` + `/respond`); Edit SHALL feed the user's free-text reply back into the run.

#### Scenario: Accept / Reject / Edit
- **WHEN** a user acts on a proposal card
- **THEN** the action SHALL be delivered through the same question respond path as a plain-text question
- **THEN** choosing Edit SHALL allow a free-text response that resumes the run

### Requirement: Backward compatibility

Questions without a `proposal` SHALL be rendered and answered identically to today. The question respond and cancel plumbing SHALL remain unchanged.

#### Scenario: Legacy question unchanged
- **WHEN** a question has no `proposal`
- **THEN** it SHALL render and respond exactly as before this change
