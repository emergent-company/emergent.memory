## ADDED Requirements

### Requirement: Client SSE event framing
The A2A SDK client SHALL parse `text/event-stream` bodies using SSE field framing rather than an exact `data: ` prefix match. A `data` field value SHALL have at most one leading space stripped; a leading tab is payload and SHALL NOT be stripped or treated as a separator. Consecutive `data` fields belonging to the same event SHALL be concatenated with `\n`, and the event SHALL be dispatched only when a blank line terminates it. Lines SHALL be terminated by CR, LF, or CRLF. Non-`data` fields (`event`, `id`, `retry`) and comment (`:`) lines SHALL be ignored without stalling the stream. A payload of `[DONE]` SHALL terminate the stream, and an event not terminated by a blank line SHALL be discarded.

#### Scenario: At most one leading space is stripped
- **WHEN** the stream contains `data:{...}`, `data: {...}`, or `data:  {...}`
- **THEN** the client strips at most one leading space, so `data:{...}` and `data: {...}` decode to the same value while `data:  {...}` keeps one leading space in the payload

#### Scenario: A leading tab is payload, not a separator
- **WHEN** the stream contains `data:\t{...}` or `data: \t{...}`
- **THEN** the client dispatches the event with the leading tab preserved in the field value (only one leading space, if present, is stripped)

#### Scenario: CR, LF, and CRLF framing are all accepted
- **WHEN** the stream terminates event lines with LF, CRLF, or a lone CR
- **THEN** the client dispatches the same decoded event for each framing, including when a CRLF or lone-CR boundary straddles a scanner buffer refill

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
