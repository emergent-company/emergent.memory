## ADDED Requirements

### Requirement: Cron trigger SHALL check pending jobs before execution

When a cron schedule fires, the system SHALL check if the agent already has pending jobs before creating a new run.

#### Scenario: Cron skips execution when agent has pending jobs
- **WHEN** cron schedule fires for agent
- **AND** repo.CountPendingJobsForAgent returns count >= AGENT_MAX_PENDING_JOBS
- **THEN** system SHALL log "skipping cron trigger, queue full" with agent_id and count
- **AND** system SHALL return nil (no error)
- **AND** system SHALL NOT call executeTriggeredAgent
- **AND** system SHALL NOT create agent_run record

#### Scenario: Cron proceeds when agent queue has capacity
- **WHEN** cron schedule fires for agent
- **AND** repo.CountPendingJobsForAgent returns count < AGENT_MAX_PENDING_JOBS
- **THEN** system SHALL proceed with executeTriggeredAgent
- **AND** system SHALL create new agent_run

### Requirement: Cron registration SHALL validate minimum interval

When registering a cron trigger, the system SHALL validate that the cron schedule meets minimum interval requirements.

#### Scenario: Invalid cron rejected during registration
- **WHEN** TriggerService.registerCronTrigger is called with cron "* * * * *"
- **THEN** system SHALL call validateCronInterval with cron expression
- **AND** validation SHALL return error "cron interval 1m is below minimum 15m"
- **AND** system SHALL return error from registerCronTrigger
- **AND** cron trigger SHALL NOT be registered in scheduler

#### Scenario: Valid cron accepted during registration
- **WHEN** TriggerService.registerCronTrigger is called with cron "*/15 * * * *"
- **THEN** validation SHALL pass
- **AND** system SHALL call scheduler.AddCronTask
- **AND** cron trigger SHALL be registered successfully

### Requirement: SyncAgentTrigger SHALL re-validate cron on agent update

When an agent is updated, the system SHALL re-validate its cron schedule before re-registering the trigger.

#### Scenario: Agent update with invalid cron fails trigger sync
- **WHEN** agent is updated with new cron_schedule="*/5 * * * *"
- **AND** TriggerService.SyncAgentTrigger is called
- **THEN** system SHALL validate cron interval
- **AND** validation SHALL fail
- **AND** system SHALL log error "failed to register cron trigger on sync"
- **AND** trigger SHALL NOT be registered
- **AND** agent SHALL remain with old cron schedule

### Requirement: Disabled agents SHALL have triggers removed

When an agent is disabled (manually or auto-disabled), the system SHALL remove its cron trigger from the scheduler.

#### Scenario: Auto-disable removes cron trigger
- **WHEN** agent is auto-disabled due to consecutive failures
- **AND** agent has trigger_type=schedule
- **THEN** system SHALL call TriggerService.RemoveAgentTrigger with agent ID
- **AND** system SHALL call scheduler.RemoveTask with task name "agent:{agent_id}"
- **AND** cron trigger SHALL no longer fire

#### Scenario: Manual disable removes cron trigger
- **WHEN** user sets agent enabled=false via API
- **AND** TriggerService.SyncAgentTrigger is called
- **THEN** system SHALL call RemoveAgentTrigger
- **AND** cron trigger SHALL be removed from scheduler

### Requirement: Re-enabling agent SHALL re-register trigger

When a disabled agent is re-enabled, the system SHALL re-validate and re-register its cron trigger.

#### Scenario: Re-enable registers cron trigger
- **WHEN** previously disabled agent is set to enabled=true
- **AND** agent has trigger_type=schedule and valid cron_schedule
- **AND** TriggerService.SyncAgentTrigger is called
- **THEN** system SHALL validate cron schedule
- **AND** system SHALL call registerCronTrigger
- **AND** cron trigger SHALL be added to scheduler
- **AND** future cron fires SHALL execute the agent

### Requirement: Trigger service SHALL handle pending job count errors gracefully

If counting pending jobs fails, the cron trigger SHALL proceed to avoid blocking valid executions.

#### Scenario: Count error allows cron to proceed
- **WHEN** cron fires for agent
- **AND** repo.CountPendingJobsForAgent returns database error
- **THEN** system SHALL log warning "failed to check pending jobs"
- **AND** system SHALL proceed with agent execution
- **AND** system SHALL NOT skip the cron trigger
