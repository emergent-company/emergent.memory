## ADDED Requirements

### Requirement: Overwrite restore reconciles in-memory agent trigger registrations

An overwrite restore whose project wipe removes runtime agent rows SHALL tear down the in-memory trigger registrations (scheduler cron tasks and reaction event listeners) for exactly the agent ids that existed in the project before the wipe and are not re-created by the snapshot. Because the wipe and insert run in one transaction, teardown SHALL happen only after that transaction commits, so a rolled-back restore removes no registrations. An agent id the snapshot re-creates SHALL keep its registrations.

#### Scenario: Removed agents are unregistered after commit

- **GIVEN** a project with runtime agents that register cron and/or reaction triggers
- **WHEN** an overwrite restore wipes the project and its snapshot re-creates only some of those agents
- **THEN** the trigger registrations for each agent id absent from the snapshot SHALL be removed
- **THEN** the registrations for each agent id present in the snapshot SHALL remain

#### Scenario: Rolled-back restore does not tear down registrations

- **WHEN** an overwrite restore fails before its transaction commits
- **THEN** no agent trigger registrations SHALL be removed
