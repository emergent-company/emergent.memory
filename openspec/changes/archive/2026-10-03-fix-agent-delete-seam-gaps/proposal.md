## Why

Runtime-agent deletion must tear down the agent's in-memory trigger
registrations (scheduler cron task + reaction event listeners) in the same
logical step as the row delete. Two delete paths bypassed that seam (issue
#1361): a blueprint `Unapply` deleted blueprint-owned runtime agents across
every project the blueprint source id was applied to (not just the Unapply
project), and an overwrite backup restore wipes `kb.agents` with raw SQL, so a
removed agent id kept dispatching until a process restart.

The `agent-reaction-triggers` and `backup-restore` specifications do not
describe these paths, so the shipped behaviour is spec-drift.

## What Changes

- Scope `DeleteAgentsBySourceBlueprint` by `project_id`, so a blueprint
  `Unapply` in one project never deletes another project's agents.
- Have the overwrite backup restore resolve, inside its transaction, the agent
  ids its project wipe removes and the snapshot does not re-create, then fire
  the agents repository deletion seam for those ids only after the transaction
  commits. Ids the snapshot re-creates keep their registrations.
- Extend the `agent-reaction-triggers` requirement "Deleting a runtime agent
  removes its trigger registrations" to name the overwrite-restore path and the
  re-created-id carve-out.
- Add a `backup-restore` reconciliation requirement for in-memory agent trigger
  registrations on overwrite restore.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `agent-reaction-triggers`: the runtime-agent deletion requirement now also
  covers the overwrite backup-restore wipe and states that re-created ids keep
  their registrations.
- `backup-restore`: adds an overwrite-restore requirement to reconcile in-memory
  agent trigger registrations for removed agents, after commit.

## Impact

- `apps/server/domain/agents/repository.go`: project-scoped
  `DeleteAgentsBySourceBlueprint`, exported `NotifyAgentsDeleted` seam.
- `apps/server/domain/blueprints/service.go`: pass `projectID` to the delete.
- `apps/server/domain/backups/restorer.go`: `removedAgentIDs` resolution +
  post-commit teardown; `SetAgentDeletionNotifier`.
- `apps/server/cmd/server/main.go`: wire the agents deletion seam into the
  restorer when both features are enabled.
- Tests: `apps/server/domain/backups/restorer_agent_delete_db_test.go`,
  `apps/server/domain/blueprints/unapply_triggers_db_test.go`, plus updates to
  the agents deletion-listener tests and the `apply_test.go` fake repository.
