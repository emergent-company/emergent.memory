## MODIFIED Requirements

### Requirement: Expose tool call details

The API SHALL include, for each tool call in a session, the tool name, the tool-call id, its arguments, its result, whether it errored, and its execution duration when the recorder captured one.

#### Scenario: Tool call detail present

- **WHEN** a session contains a tool call
- **THEN** the timeline record includes the tool name, the tool-call id, arguments, output, and error flag

#### Scenario: Tool call duration present

- **WHEN** the recorded tool call has a measured execution duration
- **THEN** the timeline record includes that duration

## ADDED Requirements

### Requirement: Record the composed agent instruction

The system SHALL persist, for each agent run, the composed system instruction actually sent to the model — the agent's resolved instruction plus any skills block, appendix, or workspace augmentation — as a `system` message record in the run's transcript, at the start of the run.

#### Scenario: Composed instruction recorded

- **WHEN** an agent run executes
- **THEN** the run's transcript contains one `system` message whose text is the composed instruction handed to the model for that run

#### Scenario: Instruction not treated as a reply

- **WHEN** a consumer reads the run's messages to extract the agent's reply
- **THEN** the `system` instruction record is excluded, exactly as before this change

#### Scenario: Historical run without a recorded instruction

- **WHEN** a run predates this change and has no `system` record
- **THEN** its transcript is returned without a system instruction record and without error
