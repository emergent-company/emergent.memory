## MODIFIED Requirements

### Requirement: Show tool call details

For each tool call in a session timeline, the viewer SHALL show the tool name, its arguments, its output, whether it errored, the tool-call id, and its execution duration when the record carries one.

#### Scenario: Tool call detail present

- **WHEN** a session contains a tool call
- **THEN** the tool call's name, arguments, output, and error status are shown

#### Scenario: Tool call id and duration shown

- **WHEN** a tool-call record carries an id or an execution duration
- **THEN** the tool call card shows the id and the duration alongside the name and status

#### Scenario: Tool call errored

- **WHEN** a tool call completed with an error
- **THEN** the error is called out distinctly from successful calls

## ADDED Requirements

### Requirement: Show the conversation's agent prompt

The session viewer SHALL show the conversation's composed agent instruction — the `system` record captured for the conversation's first run — as a collapsible card at the very beginning of the timeline, presented like a tool-call card, and SHALL omit later duplicate instruction records.

#### Scenario: Prompt present

- **WHEN** a session's transcript contains a `system` instruction record
- **THEN** the viewer shows an agent-prompt card at the top of the timeline with the instruction text in a collapsed, expandable body

#### Scenario: Prompt absent

- **WHEN** a session's transcript has no `system` instruction record (older runs)
- **THEN** the viewer shows no agent-prompt card and renders the timeline normally
