## ADDED Requirements

### Requirement: Client SSE event framing
The A2A SDK client SHALL parse `text/event-stream` bodies using SSE field framing rather than an exact `data: ` prefix match. A `data` field MAY be followed by zero or more spaces or a tab; the client SHALL strip at most one leading space and/or one leading tab from each field value. Consecutive `data` fields belonging to the same event SHALL be concatenated with `\n`, and the event SHALL be dispatched only when a blank line terminates it. Non-`data` fields (`event`, `id`, `retry`) and comment (`:`) lines SHALL be ignored without stalling the stream. A payload of `[DONE]` SHALL terminate the stream, and an event not terminated by a blank line SHALL be discarded.

#### Scenario: Space and tab separators are accepted
- **WHEN** the stream contains `data:{...}`, `data: {...}`, `data:    {...}`, or `data:\t{...}`
- **THEN** the client dispatches the same decoded event for each form

#### Scenario: Multi-line data is concatenated
- **WHEN** one event's payload is split across consecutive `data:` lines and terminated by a blank line
- **THEN** the client joins the payload lines with `\n` and decodes the resulting event

#### Scenario: Non-data lines are ignored
- **WHEN** an event is preceded by `event:`, `id:`, `retry:`, or comment (`:`) lines
- **THEN** the client ignores them and still dispatches the subsequent `data:` event

#### Scenario: Event dispatched only on the terminating blank line
- **WHEN** a stream ends without a blank line terminating the final event
- **THEN** the incomplete event is discarded and the client reports end of stream

#### Scenario: Done sentinel terminates the stream
- **WHEN** a `data: [DONE]` payload is received
- **THEN** the client reports end of stream and does not dispatch any later payload
