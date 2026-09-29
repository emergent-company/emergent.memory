# web-chat-streaming Specification

(Delta for the `chat-object-citations` change. Only the requirements this change
touches are restated; all other requirements in the capability are unchanged.)

## ADDED Requirements

### Requirement: Forward the turn's citations

The gateway SHALL forward the upstream terminal `citations` event to the client
verbatim, and SHALL capture the turn's citations for use when rendering that
turn's markdown snapshot. A turn without a `citations` event SHALL stream exactly
as before.

#### Scenario: Citations event is forwarded

- **WHEN** the upstream emits a `citations` event before `done`
- **THEN** the client SHALL receive that event with its citations intact

#### Scenario: No citations event changes nothing

- **WHEN** the upstream emits no `citations` event for a turn
- **THEN** the token/delta and snapshot event sequence SHALL be unchanged

## MODIFIED Requirements

### Requirement: Emit one authoritative markdown snapshot per turn

Immediately before the gateway passes the upstream `done` event through, it SHALL
emit a single `{"type":"html", …}` event containing the markdown render of the
turn's accumulated text. That snapshot SHALL be produced by the same renderer and
sanitizer used for conversation history, so that the live final render is not a
weaker or differently sanitized render than the history render of the same text.
When the turn carries citations, the snapshot SHALL apply the citation link rule:
a `/objects/<id>` link whose id is not a citation SHALL be rendered as plain text,
and a `#relationship-<rel>` fragment whose relationship is not a citation SHALL
be dropped.

#### Scenario: Snapshot precedes done

- **WHEN** a turn accumulates text and the upstream sends `done`
- **THEN** the client receives the `html` snapshot before it receives `done`

#### Scenario: Snapshot matches history

- **WHEN** the same turn text is rendered by the final snapshot and later by the conversation history renderer
- **THEN** both outputs come from the same sanitized markdown renderer and apply the same citation link rule

#### Scenario: Snapshot ordering is explicit in the event sequence

- **WHEN** a turn emits any `token` deltas and then ends normally
- **THEN** the event sequence is the deltas, then exactly one `html` snapshot, then `done`

#### Scenario: Unvalidated object links are demoted in the snapshot

- **WHEN** a turn with citations renders text linking to an id that is not a citation
- **THEN** the snapshot SHALL show that link's label as plain text and SHALL NOT emit an anchor for it
