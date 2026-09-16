# session-trace-viewer Specification

## Purpose
A gateway web UI that lists recorded agent sessions and lets a user inspect one session's timeline — user and assistant turns, tool calls with their arguments, output, and error status — together with per-run token usage.

## Requirements

### Requirement: List recorded sessions

The session viewer SHALL list recorded sessions, most recent first, each linking to its detail view.

#### Scenario: Sessions listed

- **WHEN** the session viewer loads and sessions have been recorded
- **THEN** the sessions are listed most recent first, each showing its title or room name

#### Scenario: No sessions

- **WHEN** no sessions have been recorded
- **THEN** the viewer shows a clear empty state

### Requirement: View a session timeline

The session viewer SHALL display a selected session's timeline records in chronological order, including user and assistant turns and tool calls.

#### Scenario: Timeline returned

- **WHEN** a user opens a recorded session
- **THEN** the session's timeline records are shown in chronological order

#### Scenario: Unknown session

- **WHEN** a user opens a session that does not exist
- **THEN** the viewer shows an empty timeline, not an error

#### Scenario: Message-only session

- **WHEN** a session has stored messages but no run timeline
- **THEN** the viewer shows the session's messages as timeline records rather than an empty timeline

### Requirement: Show tool call details

For each tool call in a session timeline, the viewer SHALL show the tool name, its arguments, its output, and whether it errored.

#### Scenario: Tool call detail present

- **WHEN** a session contains a tool call
- **THEN** the tool call's name, arguments, output, and error status are shown

#### Scenario: Tool call errored

- **WHEN** a tool call completed with an error
- **THEN** the error is called out distinctly from successful calls

### Requirement: Show per-run token usage

The session viewer SHALL display, for each run in a session, the input and output token counts and estimated cost when the backend provides them.

#### Scenario: Usage present

- **WHEN** a session's run records include token usage
- **THEN** each run shows its input tokens, output tokens, and estimated cost

#### Scenario: Usage absent

- **WHEN** a session's run records have no token usage
- **THEN** the viewer omits usage for those runs without failing the rest of the view

### Requirement: Surface load failures without crashing

The session viewer SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable

### Requirement: Show a run's trace waterfall

For each run in a session whose run DTO carries trace spans, the viewer SHALL show a waterfall of the run's spans — each span indented by its depth in the trace tree (root = 0, child = parent + 1; spans whose parent is absent are depth 1), with its operation name, its duration, and for `call_llm` spans its `input → output` token counts when the run records them. Spans arrive inline in the project-scoped agent-run response (no separate trace endpoint).

#### Scenario: Run with spans

- **WHEN** a session run's DTO includes spans
- **THEN** the run shows a collapsible Trace section listing its spans indented by depth, each with operation name and duration, and `call_llm` spans additionally show their input/output token counts

#### Scenario: No spans recorded

- **WHEN** a run DTO has no spans (tracing disabled/absent or an empty trace)
- **THEN** the run shows no Trace section at all

#### Scenario: Run fetch fails

- **WHEN** fetching a run errors
- **THEN** the affected run shows no Trace section and the rest of the session timeline renders normally without a page error
