## ADDED Requirements

### Requirement: Proposal card is A2UI-representable

A `proposal` (from `ask_user`) SHALL be representable as an A2UI `proposal` component in the catalog, so that an A2UI-capable client (web, iOS, or an external A2A client) can render the proposal natively. The A2UI `proposal` component SHALL carry the same `kind`, `summary`, and `body` as the existing proposal envelope, and SHALL NOT echo secret fields (API keys, auth headers).

#### Scenario: Proposal renders through A2UI
- **WHEN** a question carries a `proposal` and the client supports A2UI
- **THEN** the client SHALL render the proposal as an A2UI `proposal` card from the catalog

#### Scenario: Proposal body is preserved
- **WHEN** a `proposal` is emitted as an A2UI component
- **THEN** its `kind`, `summary`, and `body` match the proposal envelope, and secret fields are not rendered
