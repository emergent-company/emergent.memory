## MODIFIED Requirements

### Requirement: Agent Persistence

The system MUST persist agent configurations and execution history to the database.

#### Scenario: Schema Definition

Given a fresh database
When migrations are run
Then the `agents` and `agent_runs` tables should exist
And `agents` should have `prompt`, `cron_schedule`, and `role` columns.

#### Scenario: Webhook Trigger Types

Given an agent entity
When it is configured to use an external webhook hook
Then the `TriggerType` enum should include `webhook` as a valid value.

#### Scenario: Agent Run Source Tracking

Given an agent run triggered via webhook
When the `AgentRun` record is created
Then the `TriggerSource` field should indicate `webhook` and the `TriggerMetadata` should store the webhook hook ID and request metadata.

## ADDED Requirements

### Requirement: Agent UI Trigger Types

The system SHALL allow project administrators to select the `webhook` trigger type for an agent in the Admin UI.

#### Scenario: Agent Creation

- **WHEN** an admin creates or edits an agent
- **THEN** they can select `webhook` from the trigger type dropdown.

### Requirement: Webhook Management UI

The system SHALL display a webhook hook management interface on the agent detail page.

#### Scenario: Webhook Details

- **WHEN** an admin navigates to the agent detail page for a `webhook` triggered agent
- **THEN** a section displaying all configured webhook hooks is rendered, including creation, listing, and deletion actions.
