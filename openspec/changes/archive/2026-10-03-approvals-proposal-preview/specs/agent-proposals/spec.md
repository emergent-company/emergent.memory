## ADDED Requirements

### Requirement: Proposal preview on the conversation-less question surface

The conversation-less human-input fallback surface (`/settings/approvals`) SHALL render a pending question's
structured `proposal` as the same sanitized proposal card the conversation UI uses, above the question's
answer controls. A question whose `proposal` is absent or does not render a card SHALL keep its plain-text
rendering. The preview SHALL NOT introduce a second render path — it SHALL reuse the sanitized proposal
renderer so untrusted agent content is escaped and allowlisted identically on both surfaces.

#### Scenario: Pending question shows its proposal preview

- **WHEN** a pending agent question with a structured `proposal` is rendered on `/settings/approvals`
- **THEN** the surface SHALL show the proposal card (kind, side-effect summary, and kind-specific preview) above the answer controls
- **THEN** the question SHALL remain answerable through the existing respond/cancel path

#### Scenario: Question without a renderable proposal is unchanged

- **WHEN** a pending question has no `proposal`, or its `proposal` fails to render a card
- **THEN** the surface SHALL render the question exactly as plain text, unchanged

#### Scenario: Proposal content is escaped

- **WHEN** a proposal carries agent-produced markup in any rendered field
- **THEN** the markup SHALL be escaped by the shared sanitized renderer and SHALL NOT be emitted as live HTML
