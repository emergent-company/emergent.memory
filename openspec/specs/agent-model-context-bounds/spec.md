# agent-model-context-bounds Specification

## Purpose
Defines a bounded, honest policy for the tool-result payloads that re-enter an
agent's model context: an oversized result is truncated with an actionable
marker, and a per-request total budget elides the oldest results, while the
persisted and streamed payloads remain complete.

## Requirements

### Requirement: Tool results entering the model context are bounded

Before each model call, every tool result (`genai.FunctionResponse`) in the
model request SHALL be bounded to a configurable maximum size. A result that
exceeds the cap SHALL be replaced, for the model only, by a bounded envelope that
retains the tool's `ok` status, a `truncated: true` flag, an actionable
truncation marker, and a valid-UTF-8 preview prefix. A result at or under the
cap SHALL be passed through unchanged.

#### Scenario: Oversized result is truncated with an actionable marker

- **WHEN** a tool returns a payload whose JSON encoding exceeds the configured per-result cap
- **THEN** the payload the model sees SHALL encode to at most the cap
- **THEN** it SHALL carry `truncated: true` and a non-empty marker naming how to narrow (filters, `fields[]`, `limit`, `offset`, or a specific key)
- **THEN** it SHALL retain the original `ok` value
- **THEN** it SHALL include a preview prefix of the original payload

#### Scenario: Result under the cap is unchanged

- **WHEN** a tool result encodes to no more than the configured per-result cap
- **THEN** the model SHALL receive the result unchanged

#### Scenario: Every tool backend is covered

- **WHEN** a tool result from any backend (builtin, external, relay, workspace) becomes a function response in the model request
- **THEN** the same per-result bound SHALL apply to it

### Requirement: The total tool-result budget elides oldest results first

The sum of tool results in a single model request SHALL be bounded by a
configurable total budget, with one documented exception: the budget is **soft**
for the most recent result. While the sum exceeds the budget, the oldest tool
results SHALL be replaced, for the model only, by a short `elided: true` marker.
The most recent tool result SHALL NOT be elided, so if it alone exceeds the
budget it SHALL be retained whole and the request MAY remain over budget in that
single-result case. A request that remains over budget only because of the
retained newest result SHALL NOT drop it silently.

#### Scenario: Oldest result is elided when the total budget is exceeded

- **WHEN** the combined size of tool results in a model request exceeds the total budget
- **THEN** the oldest results SHALL be elided until the sum is within budget or only the most recent result remains
- **THEN** each elided result SHALL carry an `elided: true` marker telling the model to re-run the tool narrowly if needed

#### Scenario: Most recent result is retained

- **WHEN** the total budget would require eliding the most recent tool result
- **THEN** the most recent result SHALL be retained in full

#### Scenario: The most recent result alone exceeds the total budget

- **WHEN** the request contains a single tool result, or its most recent result alone, whose size exceeds the total budget
- **THEN** every older result SHALL be elided
- **THEN** the most recent result SHALL be retained whole and SHALL NOT be silently dropped
- **THEN** the request MAY remain over the total budget in this single-result case

### Requirement: Only the model context is bounded

The full tool result SHALL remain available everywhere it is persisted or
streamed. Bounding SHALL apply only to the model request; the session events,
the persisted run tool-call and message rows, and the streamed UI events SHALL
retain the complete payload.

#### Scenario: Persisted and streamed payloads stay complete

- **WHEN** a tool returns an oversized payload that is truncated for the model
- **THEN** the persisted tool-call row SHALL contain the full payload
- **THEN** the streamed tool-call-end event SHALL contain the full payload

#### Scenario: The model-facing copy does not mutate the source

- **WHEN** a result is bounded for the model
- **THEN** the source result map SHALL NOT be mutated

### Requirement: The bound is configurable

The per-result cap, the total budget, and per-tool overrides SHALL be
configurable through environment variables consistent with the existing MCP
guardrails. Zero values SHALL fall back to documented generous defaults; a
negative value SHALL disable the corresponding layer. An override SHALL apply
only to the named tool and SHALL be ignored when non-positive.

#### Scenario: Defaults apply when unset

- **WHEN** no bound-related environment variables are set
- **THEN** the per-result cap and total budget SHALL use their documented defaults

#### Scenario: Per-tool override takes precedence

- **WHEN** a per-tool override is configured for a tool and the global per-result cap is enabled
- **THEN** that tool SHALL use the override instead of the global per-result cap
- **THEN** every other tool SHALL use the global cap

#### Scenario: Per-tool override cannot re-enable a disabled layer

- **WHEN** the global per-result cap is disabled (negative) and a positive per-tool override is configured for a tool
- **THEN** that tool's per-result truncation SHALL also be disabled and the result SHALL pass through unchanged

#### Scenario: Negative value disables a layer

- **WHEN** the per-result cap is set negative
- **THEN** per-result truncation SHALL be disabled and results SHALL pass through unchanged
- **THEN** per-tool overrides SHALL be ignored

#### Scenario: Documented defaults bound the extreme tail

- **WHEN** no bound-related environment variables are set
- **THEN** the per-result cap SHALL default to 128 KiB (≈32k tokens) and the total budget SHALL default to 512 KiB (≈128k tokens)
- **THEN** the total budget SHALL be at least the per-result cap, so with defaults the total budget elides results only once more than four capped results have accumulated in one request
