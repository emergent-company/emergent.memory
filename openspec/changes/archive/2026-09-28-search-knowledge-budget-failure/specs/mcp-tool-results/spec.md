## ADDED Requirements

### Requirement: search-knowledge fails loudly on budget exhaustion

The `search-knowledge` MCP tool SHALL NOT report success (`ok: true`) when its per-call budget is exhausted before the nested query run completes, or when the response stream ends without reaching its terminal event. When the query context is done, the tool SHALL distinguish the termination causes:

- The tool's own budget (`queryKnowledgeTimeout`): return an error naming the configured budget, derived from the constant and never from a hardcoded literal.
- A caller-supplied deadline that expires before the tool budget: return an error that names the caller deadline as the cause, rather than mis-attributing it to `queryKnowledgeTimeout`.
- `context.Canceled`: propagate the cancellation as a cancellation — an error for which `errors.Is(err, context.Canceled)` holds — rather than reporting a timeout or an empty success.

Because the tool call fails, the caller cannot mistake an exhausted, cancelled, or disconnected query for a corpus with nothing to say. A normally completed call SHALL return a success payload containing the assembled `answer` (and `session_id`/`run_id` when known) and SHALL NOT carry a `truncated` field. Only the deadline, cancellation, and incomplete-stream paths stop reporting success.

A response stream is complete only when it emitted its terminal event — either a `{"type":"done"}` chunk (the event the query endpoint writes) or the `[DONE]` sentinel. A stream that ends without that marker (for example a proxy/backend disconnect, or a deadline landing before the marker) SHALL be an error, not a partial or empty success.

#### Scenario: Tool budget expiry yields a failed call naming the configured budget

- **WHEN** the nested query run does not complete within `queryKnowledgeTimeout` and the caller imposed no earlier deadline
- **THEN** the tool call SHALL fail rather than return `ok: true` with an empty answer
- **THEN** the error SHALL name the configured budget derived from `queryKnowledgeTimeout`
- **THEN** the error SHALL NOT contain the stale hardcoded `60s` literal

#### Scenario: Caller deadline is reported as a caller deadline

- **WHEN** the caller's context deadline expires before `queryKnowledgeTimeout`
- **THEN** the tool call SHALL fail with an error naming the caller deadline
- **THEN** the error SHALL NOT be reported as the internal `queryKnowledgeTimeout` budget

#### Scenario: Cancellation propagates as a cancellation

- **WHEN** the calling context is cancelled before the nested query run completes
- **THEN** the tool call SHALL return an error satisfying `errors.Is(err, context.Canceled)`
- **THEN** the failure SHALL NOT be reported as a timeout and SHALL NOT be returned as `ok: true`

#### Scenario: Stream without a terminal event is an error

- **WHEN** the response stream ends without a `{"type":"done"}` chunk or `[DONE]` sentinel (for example the connection drops mid-stream)
- **THEN** the tool call SHALL fail
- **THEN** any partial tokens received SHALL NOT be returned as a successful answer

#### Scenario: Successful query is unchanged apart from the removed truncated field

- **WHEN** the nested query run completes and the stream emits its terminal event
- **THEN** the tool result SHALL remain a success carrying the assembled `answer`
- **THEN** the success payload SHALL NOT contain a `truncated` field
