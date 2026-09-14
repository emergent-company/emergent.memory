## Purpose

Lets the owner run an agent automatically on a recurring schedule: create a scheduled agent from a prompt and a cron expression, manage it (edit, enable/disable, delete), trigger it manually, and distinguish its runs from on-demand sessions.

## ADDED Requirements

### Requirement: List scheduled agents

The system SHALL list the project's scheduled agents, each showing its name, cron schedule, linked agent definition, enabled state, and last-run status.

#### Scenario: Scheduled agents listed

- **WHEN** the owner opens the schedules view
- **THEN** the system lists the project's scheduled agents, most recently updated first

#### Scenario: No scheduled agents

- **WHEN** no scheduled agents exist for the project
- **THEN** the system shows an empty state, not an error

### Requirement: Create a scheduled agent

The system SHALL let the owner create a scheduled agent by providing a name, a prompt, and a cron schedule, and MAY link it to an existing agent definition that supplies the model, system prompt, and tools for each run.

#### Scenario: Create with a valid schedule

- **WHEN** the owner submits a scheduled agent with a name, a prompt, and a valid cron expression
- **THEN** the scheduled agent is created and enabled, and its prompt runs on the configured schedule

#### Scenario: Create with an invalid schedule

- **WHEN** the owner submits a scheduled agent with an invalid or empty cron expression
- **THEN** the system rejects the submission with an error and does not create the agent

### Requirement: Edit a scheduled agent

The system SHALL let the owner change an existing scheduled agent's prompt and cron schedule.

#### Scenario: Change prompt and schedule

- **WHEN** the owner edits a scheduled agent's prompt or schedule and saves
- **THEN** the new prompt and schedule take effect for subsequent runs

### Requirement: Enable and disable a scheduled agent

The system SHALL let the owner enable or disable a scheduled agent. A disabled agent MUST NOT run on its schedule.

#### Scenario: Disable stops scheduling

- **WHEN** the owner disables a scheduled agent
- **THEN** the agent no longer runs on its schedule until re-enabled

#### Scenario: Enable resumes scheduling

- **WHEN** the owner re-enables a disabled scheduled agent
- **THEN** the agent resumes running on its schedule

### Requirement: Delete a scheduled agent

The system SHALL let the owner delete a scheduled agent, after which it MUST NOT run again.

#### Scenario: Deleted agent stops running

- **WHEN** the owner deletes a scheduled agent
- **THEN** the agent is removed and no longer runs on its schedule

### Requirement: Trigger a scheduled agent manually

The system SHALL let the owner trigger a scheduled agent immediately, independent of its schedule.

#### Scenario: Manual trigger runs the prompt

- **WHEN** the owner triggers a scheduled agent manually
- **THEN** the agent's prompt runs immediately with the same configuration as a scheduled run

### Requirement: Show scheduled-run status

The system SHALL show each scheduled agent's last run status, including whether it succeeded or failed and when it last ran, so the owner can see whether the schedule is working.

#### Scenario: Last run status shown

- **WHEN** the owner views a scheduled agent
- **THEN** the agent's last run time and status (succeeded or failed) are shown

#### Scenario: Never run

- **WHEN** a scheduled agent has not run yet
- **THEN** the system shows a clear "never run" state

### Requirement: Mark scheduled runs by origin

The system SHALL record the origin of each agent run so a run triggered by a schedule can be distinguished from a manual, voice, or chat run.

#### Scenario: Scheduled run marked

- **WHEN** a scheduled agent runs on its schedule
- **THEN** the resulting session is marked as originating from a schedule
