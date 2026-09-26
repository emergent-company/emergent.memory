## Purpose

Defines the declarative agent-to-UI capability: an A2UI v0.9.1 catalog of structured components (cards) that an agent emits as JSON, the server transports over chat SSE and A2A, and native clients (web and iOS) render. It covers catalog ownership, executor emission, validation, rendering, the action round-trip, and the security model.

## ADDED Requirements

### Requirement: Catalog is the single source of truth

The system SHALL define the set of agent-renderable components in a single A2UI v0.9.1 JSON Schema catalog. The server SHALL validate every emitted surface message against that catalog before transport, and every renderer SHALL resolve components by catalog id. The catalog SHALL be additive: adding a component SHALL NOT change the envelope or existing components.

#### Scenario: Emitted messages validate against the catalog
- **WHEN** an agent emits an A2UI surface
- **THEN** each `createSurface`/`updateComponents`/`updateDataModel` message SHALL validate against the catalog before the server transports it

#### Scenario: Catalog is additive
- **WHEN** a new component is added to the catalog
- **THEN** existing components, the envelope, and unmodified renderers SHALL behave unchanged

### Requirement: First-class card components

The catalog SHALL provide first-class components for the structured output agents already produce: `proposal`, `approval`, `question`, `code`, `entity`, `object-form`, `todo`, and `result`. Each SHALL carry the same body shape its write-path counterpart consumes.

#### Scenario: Proposal component
- **WHEN** an agent emits a `proposal` component
- **THEN** the client SHALL render a reviewable card with the proposal `kind`, `summary`, and `body` and accept/reject actions

#### Scenario: Approval and proposal components mask credentials
- **WHEN** an agent emits an `approval` or `proposal` component whose body contains auth headers or an API key
- **THEN** the client SHALL NOT render those secret fields

### Requirement: Agent emits A2UI as fenced or structured output

The executor SHALL recognize A2UI emitted as a fenced ```a2ui block and/or a structured tool output, extract the messages, and validate them against the catalog. Invalid or absent A2UI SHALL NOT fail the run; the turn SHALL proceed as markdown text.

#### Scenario: Valid A2UI is extracted
- **WHEN** the model output contains a well-formed ```a2ui fenced block matching the catalog
- **THEN** the executor SHALL emit a structured-UI event carrying the surface

#### Scenario: Invalid A2UI degrades to text
- **WHEN** the model output contains an ```a2ui block that fails catalog validation
- **THEN** the run SHALL proceed as markdown text and SHALL NOT return an error

### Requirement: Web renders surfaces through a component registry

The web gateway SHALL render A2UI surfaces through a component registry keyed by catalog id. A component without a registered renderer SHALL fall back to a summary/text render and SHALL NOT error. Rendering SHALL NOT interpret emitted content as executable markup.

#### Scenario: Unknown component falls back
- **WHEN** a surface references a catalog id with no registered renderer
- **THEN** the client SHALL render a summary/text fallback and SHALL NOT error

#### Scenario: No markup injection
- **WHEN** a surface component carries text containing markup such as `<img src=x onerror=alert(1)>`
- **THEN** the client SHALL render it as literal text and SHALL NOT create an element from it

### Requirement: iOS renders the same surfaces natively

The iOS client SHALL render A2UI surfaces with native SwiftUI views from the same catalog, consuming the same JSON payload as the web client.

#### Scenario: Same payload, native view
- **WHEN** the server emits a surface for a `proposal` or `question` component
- **THEN** the iOS client SHALL render a native SwiftUI card for it, not a web view or raw JSON

### Requirement: Action round-trip returns to the run

A user action on a surface (button `event` or input `functionCall`) SHALL return to the server carrying the `surfaceId` and action payload, and the server SHALL resume the run and emit updated `updateComponents`/`updateDataModel` for that surface. Surface actions SHALL be distinct from `ask_user` question answers.

#### Scenario: Action resumes and updates the surface
- **WHEN** a user submits an action on a surface
- **THEN** the server SHALL resume the run and emit updated messages for the same `surfaceId`

#### Scenario: Surface action is not a question answer
- **WHEN** a surface action arrives during a run that is not paused on an `ask_user` question
- **THEN** the action SHALL be routed to the surface, not through the question-answer path

### Requirement: Security — allowlist, no code execution, no secret echo

Rendering SHALL be limited to the pre-approved catalog (allowlist). The system SHALL NOT transmit or execute arbitrary code. Secret-bearing fields (API keys, auth headers) SHALL never be rendered. The client data model, when transmitted, SHALL be scoped to the surfaces the receiving agent owns.

#### Scenario: Only catalog components render
- **WHEN** a surface references a component outside the catalog
- **THEN** the client SHALL NOT render it and SHALL fall back instead

#### Scenario: Secrets are not echoed
- **WHEN** an emitted component body contains a secret field
- **THEN** no renderer SHALL display the secret
