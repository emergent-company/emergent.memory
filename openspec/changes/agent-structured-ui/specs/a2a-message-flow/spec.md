## ADDED Requirements

### Requirement: A2UI surface data parts

A structured-UI (A2UI) surface SHALL be transported over A2A as a `data` part whose `metadata.mimeType` is `application/a2ui+json` and whose `data` is an array of A2UI messages (`createSurface`, `updateComponents`, `updateDataModel`, `deleteSurface`). Each surface SHALL be carried by a `data` part with a stable `artifactId` derived from the `surfaceId`.

#### Scenario: A2UI messages are an array
- **WHEN** an agent emits an A2UI surface over A2A
- **THEN** the `data` member is an array of A2UI messages, never a single object

#### Scenario: Mime type marks the part
- **WHEN** an A2UI surface is serialized into an A2A part
- **THEN** the part's `metadata.mimeType` is `application/a2ui+json`

#### Scenario: Surface id is stable
- **WHEN** a surface receives multiple `updateComponents`/`updateDataModel` messages
- **THEN** they SHALL be carried by data parts with the same stable `artifactId`

### Requirement: A2UI streaming via artifact updates

A2UI deltas SHALL be emitted over `message:stream` as `artifactUpdate` events, ordered so `createSurface` precedes any `updateComponents`/`updateDataModel` for that surface, and interleaved with the existing text and tool-call artifacts without reordering.

#### Scenario: createSurface precedes updates
- **WHEN** a surface is streamed
- **THEN** the `createSurface` message is emitted before any update message for that surface

#### Scenario: A2UI deltas keep stream order
- **WHEN** a run streams text deltas, tool calls, and A2UI surfaces
- **THEN** the `artifactUpdate` events preserve the executor's emission order

### Requirement: A2UI action resume

A follow-up A2A message carrying a surface action (`surfaceId` + action payload in `metadata`) on the same `contextId`/`taskId` SHALL resume the run and produce updated A2UI messages for that surface. The surface-action resume SHALL be distinct from the `INPUT_REQUIRED` question-answer resume.

#### Scenario: Surface action resumes the run
- **WHEN** a client sends a follow-up message carrying a surface action for an active task
- **THEN** the server resumes the run and emits updated A2UI messages for the referenced `surfaceId`

#### Scenario: Surface action is not answered as a question
- **WHEN** a surface action arrives and the task is not paused on a pending question
- **THEN** the action SHALL NOT be routed through the question-answer (`AnswerQuestion`) path
