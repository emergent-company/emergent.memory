## ADDED Requirements

### Requirement: Stream assistant text as raw deltas

The gateway SHALL forward each upstream `token` event to the client as a `{"type":"token","token":"<delta>"}` event carrying the raw text delta, without rendering markdown for that delta. The gateway SHALL NOT emit more than one markdown-rendered `html` snapshot per turn.

#### Scenario: Deltas are forwarded unrendered

- **WHEN** the upstream emits three `token` events with the deltas `"Hel"`, `"lo "`, and `"world"`
- **THEN** the client receives three `token` events carrying exactly those deltas and no `html` event is emitted for any of them

#### Scenario: Markdown is rendered once per turn

- **WHEN** a turn streams an answer delivered in N token events and ends with `done`
- **THEN** exactly one markdown render is performed for that turn and exactly one `html` event is emitted

#### Scenario: Empty turn emits no snapshot

- **WHEN** a turn ends with `done` and no token delta was ever received
- **THEN** no `html` snapshot is emitted for that turn

### Requirement: Emit one authoritative markdown snapshot per turn

Immediately before the gateway passes the upstream `done` event through, it SHALL emit a single `{"type":"html", …}` event containing the markdown render of the turn's accumulated text. That snapshot SHALL be produced by the same renderer and sanitizer used for conversation history, so that the live final render and the history render of the same text are identical.

#### Scenario: Snapshot precedes done

- **WHEN** a turn accumulates text and the upstream sends `done`
- **THEN** the client receives the `html` snapshot before it receives `done`

#### Scenario: Snapshot matches history

- **WHEN** the same turn text is rendered by the final snapshot and later by the conversation history renderer
- **THEN** both outputs are byte-identical, because both call the same sanitized markdown renderer

#### Scenario: Snapshot ordering is explicit in the event sequence

- **WHEN** a turn emits any `token` deltas and then ends normally
- **THEN** the event sequence is the deltas, then exactly one `html` snapshot, then `done`

### Requirement: Snapshot is emitted even when the stream does not end cleanly

If the upstream stream ends without a `done` event — on `error`, on EOF, or on scanner failure — and the accumulated turn text is non-empty, the gateway SHALL emit the final `html` snapshot before the stream terminates. The snapshot SHALL be emitted at most once per turn.

#### Scenario: Fallback on error termination

- **WHEN** a turn accumulates non-empty text and the upstream stream ends with an `error` event instead of `done`
- **THEN** the gateway emits the final `html` snapshot before the stream closes

#### Scenario: Fallback on EOF without done

- **WHEN** the upstream stream ends at EOF with accumulated non-empty text and no `done`
- **THEN** the gateway emits the final `html` snapshot

#### Scenario: No duplicate snapshot

- **WHEN** the final snapshot was already emitted for a `done` event
- **THEN** the post-loop fallback does not emit a second snapshot for that turn

### Requirement: Render raw deltas without interpreting them as markup

The shared streaming engine SHALL accumulate raw token deltas and render them using a text sink that does not interpret markup. Raw delta content SHALL NOT be assigned to `innerHTML` or `insertAdjacentHTML` under any circumstance. Markup interpretation SHALL remain confined to the authoritative `html` snapshot, which is server-rendered and sanitized.

#### Scenario: Delta content is never interpreted as markup

- **WHEN** a token delta contains markup such as `<img src=x onerror=alert(1)>`
- **THEN** the live bubble displays that text literally and no element is created from it

#### Scenario: Authoritative snapshot takes over the bubble

- **WHEN** the `html` snapshot arrives after a sequence of raw deltas
- **THEN** the bubble is replaced by the snapshot's rendered markdown, and the raw-text path no longer writes to that bubble

#### Scenario: Render paths are mutually exclusive

- **WHEN** a turn is streaming raw deltas, before any `html` snapshot has arrived
- **THEN** only the raw-text path writes to the bubble, and after the snapshot only the snapshot path writes to it

### Requirement: Delta handling is additive across all chat hosts

The token-delta path SHALL be implemented in the shared streaming engine and the shared event dispatcher, so that the `/chat` page, the global sidepanel, and the recorded-run transcript view all gain it from one implementation. A host that does not wire the delta hook SHALL continue to function, and an unhandled event type SHALL be ignored rather than treated as an error.

#### Scenario: A host without the hook still works

- **WHEN** a host renders a stream but does not wire the delta hook
- **THEN** the host still renders the authoritative `html` snapshot and does not error on the `token` events

#### Scenario: All three hosts share one implementation

- **WHEN** the delta path is wired
- **THEN** the `/chat` page, the sidepanel, and the recorded-run transcript view all use the same engine code path rather than host-specific copies

#### Scenario: The sidepanel retains the reply

- **WHEN** a sidepanel turn ends and the sidepanel persists the reply rather than re-fetching history
- **THEN** the persisted content is the authoritative server-rendered snapshot, so the reply is present after reload

### Requirement: Every host ends on the authoritative render

Each host SHALL end a completed turn with the server-rendered snapshot applied, so that no host is left displaying raw unformatted text. The `/chat` page's existing history re-fetch SHALL remain the authoritative swap for persisted transcript state.

#### Scenario: Chat page ends on the rendered snapshot

- **WHEN** a `/chat` turn completes
- **THEN** the bubble shows the rendered snapshot before the history re-fetch replaces the transcript with the authoritative persisted state

#### Scenario: Sidepanel ends on the rendered snapshot

- **WHEN** a sidepanel turn completes with no history re-fetch
- **THEN** the bubble shows the rendered snapshot and the persisted reply is not raw text
