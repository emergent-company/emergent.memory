## ADDED Requirements

### Requirement: Structured UI events stream alongside text

The gateway SHALL forward A2UI surface messages to the web client as a `ui` event carrying `surfaceId` and the ordered `messages` array, interleaved with `token`/`html` events without reordering. An event dispatcher that does not handle `ui` SHALL ignore it rather than error.

#### Scenario: ui event is forwarded
- **WHEN** the upstream emits an A2UI surface during a chat turn
- **THEN** the client receives a `ui` event carrying the `surfaceId` and ordered messages

#### Scenario: Unhandled ui event is ignored
- **WHEN** a host renders a stream but does not wire the `ui` handler
- **THEN** the host still renders text and does not error on the `ui` event

#### Scenario: ui events keep stream order
- **WHEN** a turn emits text deltas and A2UI surfaces
- **THEN** the `ui` events preserve the executor's emission order relative to `token` events
