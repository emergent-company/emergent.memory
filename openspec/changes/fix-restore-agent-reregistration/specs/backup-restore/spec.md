## MODIFIED Requirements

### Requirement: Overwrite restore reconciles in-memory agent trigger registrations

An overwrite restore whose project wipe removes runtime agent rows SHALL tear down the in-memory trigger registrations (scheduler cron tasks and reaction event listeners) for exactly the agent ids that existed in the project before the wipe and are not re-created by the snapshot. For every runtime agent id the snapshot re-creates or newly adds, the restore SHALL reconcile that agent's in-memory trigger registrations from the restored row after commit — tearing down any stale registration and registering the restored configuration — so a re-created agent's changed trigger config takes effect and a newly added agent is registered. Reconciliation SHALL be idempotent, so an overlapping startup trigger sync or a repeated restore converges on the restored configuration. Because the wipe and insert run in one transaction, both teardown and reconciliation SHALL happen only after that transaction commits, so a rolled-back restore changes no registrations. An agent id the snapshot re-creates SHALL remain registered, from its restored configuration.

#### Scenario: Removed agents are unregistered after commit

- **GIVEN** a project with runtime agents that register cron and/or reaction triggers
- **WHEN** an overwrite restore wipes the project and its snapshot re-creates only some of those agents
- **THEN** the trigger registrations for each agent id absent from the snapshot SHALL be removed
- **THEN** the registrations for each agent id present in the snapshot SHALL remain

#### Scenario: Re-created agent is reconciled from the restored row

- **GIVEN** a project with a runtime agent whose in-memory trigger registration reflects its pre-restore configuration
- **WHEN** an overwrite restore re-creates that agent id with a changed cron schedule or reaction config
- **THEN** the stale registration SHALL be torn down and the agent SHALL be registered from the restored configuration
- **THEN** the agent SHALL fire on the restored configuration, not the pre-restore one

#### Scenario: Newly added agent is registered

- **WHEN** an overwrite restore's snapshot contains a runtime agent id that did not exist in the project before the restore
- **THEN** that agent's trigger registrations SHALL be created from the restored row

#### Scenario: Rolled-back restore does not tear down registrations

- **WHEN** an overwrite restore fails before its transaction commits
- **THEN** no agent trigger registrations SHALL be removed or re-registered
