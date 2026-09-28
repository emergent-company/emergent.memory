## ADDED Requirements

### Requirement: search-knowledge fails loudly on budget exhaustion

The `search-knowledge` MCP tool SHALL NOT report success (`ok: true`) when its per-call budget is exhausted before the nested query run completes. When the query context is done, the tool SHALL distinguish the two termination causes:

- `context.DeadlineExceeded`: return an error naming the configured budget, derived from the `queryKnowledgeTimeout` constant and never from a hardcoded literal.
- `context.Canceled`: propagate the cancellation as a cancellation — an error for which `errors.Is(err, context.Canceled)` holds — rather than reporting a timeout or an empty success.

Because the tool call fails, the caller cannot mistake an exhausted or cancelled query for a corpus with nothing to say. A normally completed call SHALL continue to return a successful result with the assembled `answer` and `truncated: false`. Only the deadline and cancellation paths stop reporting success.

#### Scenario: Deadline yields a failed call naming the configured budget

- **WHEN** the nested query run does not complete within `queryKnowledgeTimeout`
- **THEN** the tool call SHALL fail rather than return `ok: true` with an empty answer
- **THEN** the error SHALL name the configured budget derived from `queryKnowledgeTimeout`
- **THEN** the error SHALL NOT contain the stale hardcoded `60s` literal

#### Scenario: Cancellation propagates as a cancellation

- **WHEN** the calling context is cancelled before the nested query run completes
- **THEN** the tool call SHALL return an error satisfying `errors.Is(err, context.Canceled)`
- **THEN** the failure SHALL NOT be reported as a timeout and SHALL NOT be returned as `ok: true`

#### Scenario: Successful query is unchanged

- **WHEN** the nested query run completes normally
- **THEN** the tool result SHALL remain a success carrying the assembled `answer`
- **THEN** `truncated` SHALL remain `false`
