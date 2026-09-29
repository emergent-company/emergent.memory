## ADDED Requirements

### Requirement: Incremental thinking segments

Thinking SHALL be transported as segments with a stable identifier and an explicit completion flag. Every delta belonging to one reasoning/planning segment SHALL share a single `id` and be emitted with `done:false`; the segment SHALL close with exactly one event carrying `done:true`. Segment boundaries SHALL be decided where the streaming semantics are known (the executor), not re-derived by the SSE mapper.

#### Scenario: Deltas of one segment share an identifier

- **WHEN** the executor streams three reasoning deltas for the same step and role
- **THEN** all three `thinking` events carry the same `id` and `done:false`

#### Scenario: A segment closes once

- **WHEN** a thinking segment ends (role change, step advance, tool start, final response, or run end)
- **THEN** exactly one `thinking` event for that `id` carries `done:true`

#### Scenario: A segment is not emitted twice

- **WHEN** a provider streams reasoning as partial `Thought` deltas and also supplies the whole non-partial block for the same step and role
- **THEN** the whole-block path does not append the reasoning a second time within the same segment

#### Scenario: Client stops live state on close

- **WHEN** the client receives a `thinking` event with `done:true`
- **THEN** the corresponding badge stops its in-progress animation and the client stops treating it as live

### Requirement: Correlated tool call lifecycle

`mcp_tool` events SHALL carry a stable call `id` that is identical on the tool's start and terminal events, so clients can correlate them even when several invocations of the same tool are in flight. Clients SHALL correlate by `id` when present and fall back to tool name only when it is absent.

#### Scenario: Parallel same-name calls do not collide

- **WHEN** two invocations of the same tool are in flight and their terminal events arrive
- **THEN** each terminal event updates the chip opened by its matching start event, and no chip is left running

#### Scenario: Result without a start still renders

- **WHEN** a terminal `mcp_tool` event arrives with an `id` for which no start chip exists
- **THEN** the client renders a single terminal chip for that invocation (no fabricated duplicate)

### Requirement: Faithful tool status

A tool that did not execute SHALL NOT be reported as `completed`. A tool paused awaiting user confirmation SHALL be reported with a non-terminal status, and a tool blocked by policy or share-allowlist SHALL be reported as an error.

#### Scenario: Confirmation pause is not green

- **WHEN** a tool call is intercepted by the confirmation gate and the run pauses
- **THEN** its chip shows a pending/awaiting state, not a success state, and the run remains visibly paused

#### Scenario: Policy-blocked tool reports an error

- **WHEN** a tool call is blocked by disabled policy or the share deny-list
- **THEN** its chip reports an error rather than success

### Requirement: Replay in-flight state on reconnect

The gateway SHALL retain, per conversation, the active run's open thinking segments and running tool calls, and SHALL replay them to a client that (re)connects to the conversation events stream. The replay SHALL be delivered as a single additive `live_replay` frame immediately after the initial `refresh` frame and before any live tail. Retained state SHALL drop a thinking segment when it closes, drop a tool call when it reaches a terminal status, and be cleared when the run is no longer running.

#### Scenario: Mid-run reload reconstructs activity

- **WHEN** a client reloads while a run is streaming with an open thinking segment and a running tool
- **THEN** after loading persisted history the client receives a `live_replay` frame and renders the open thinking segment and the running tool chip with their in-progress affordances

#### Scenario: Completed work is not duplicated

- **WHEN** a tool call already completed and rendered from persisted history
- **THEN** it is not included in `live_replay` and appears exactly once

#### Scenario: Replay stops at run end

- **WHEN** the run finishes
- **THEN** a subsequent connection receives an empty `live_replay` (or none) for that run

### Requirement: In-progress affordances are animated

The thinking indicator SHALL animate while a segment is open, and a tool chip SHALL animate from its start event until a terminal status. Replayed in-flight state SHALL animate identically to live state, and both animations SHALL be disabled under `prefers-reduced-motion: reduce`.

#### Scenario: Thinking animates while streaming

- **WHEN** a thinking segment is open
- **THEN** its indicator animates until the segment closes

#### Scenario: Tool spinner runs between start and terminal

- **WHEN** a tool start event has been received and no terminal status has arrived
- **THEN** the chip shows an animated spinner

#### Scenario: Reduced motion

- **WHEN** the user prefers reduced motion
- **THEN** the thinking indicator and tool spinner do not animate
