## MODIFIED Requirements

### Requirement: Run Logging

The system MUST log every execution of an agent.

#### Scenario: Successful Run

Given an agent executes successfully
Then an `AgentRun` record should be created with `status: completed`
And `completed_at` should be set.

#### Scenario: Failed Run

Given an agent throws an exception during execution
Then the `AgentRun` record should be updated to `status: failed`
And the error details should be captured in `logs`.

#### Scenario: Paused Run Awaiting User Input

- **WHEN** an agent calls the `ask_user` tool during execution
- **THEN** the `AgentRun` record SHALL be updated to `status: paused`
- **AND** the run summary SHALL include `reason: awaiting_user_input` and the `question_id`

#### Scenario: Resumed Run After User Response

- **WHEN** a user responds to an agent question linked to a paused run
- **THEN** a new `AgentRun` record SHALL be created with `resumed_from` set to the paused run's ID
- **AND** the `UserMessage` for the resumed run SHALL contain the original question and the user's response
