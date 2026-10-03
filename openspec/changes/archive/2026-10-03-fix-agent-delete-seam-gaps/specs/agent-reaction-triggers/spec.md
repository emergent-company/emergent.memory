## MODIFIED Requirements

### Requirement: Deleting a runtime agent removes its trigger registrations

Deleting a runtime agent SHALL remove that agent's in-memory trigger registrations — its cron schedule in the scheduler and its reaction event listeners — in the same logical step as the row delete, so no deleted agent id keeps dispatching. This SHALL hold for every delete path: the API agent-delete endpoint, the agent MCP delete tool, a blueprint Unapply that deletes blueprint-owned runtime agents, and an overwrite backup restore whose project wipe removes agent rows the snapshot does not re-create. When one delete removes many agents, every deleted agent id SHALL be unregistered. An overwrite restore SHALL unregister exactly the agent ids it removes and does not re-create, and SHALL NOT unregister an id the snapshot re-creates (the row reappears with the same id, so tearing it down would strand a live agent); because the restore's row changes are committed as one transaction, that teardown SHALL happen only after a successful commit, so a rolled-back restore removes no registrations. A failed row delete SHALL NOT remove the registrations.

#### Scenario: API delete unregisters

- **WHEN** a runtime agent with a registered cron and/or reaction trigger is deleted through the API
- **THEN** its scheduler task and event listener entries are removed

#### Scenario: Blueprint Unapply unregisters every deleted runtime agent

- **WHEN** a blueprint Unapply deletes multiple blueprint-owned runtime agents
- **THEN** each deleted agent id's trigger registrations are removed
- **THEN** only agents owned by the Unapply's project are deleted and unregistered

#### Scenario: Overwrite restore unregisters agents it does not re-create

- **WHEN** an overwrite backup restore wipes a project's agent rows and the snapshot does not re-create some of them
- **THEN** each removed agent id's trigger registrations are removed after the restore transaction commits
- **THEN** each agent id the snapshot re-creates keeps its trigger registrations

#### Scenario: Failed delete leaves registrations intact

- **WHEN** the row delete fails
- **THEN** the agent's trigger registrations remain

#### Scenario: Rolled-back restore leaves registrations intact

- **WHEN** an overwrite backup restore fails before its transaction commits
- **THEN** no agent trigger registrations are removed
