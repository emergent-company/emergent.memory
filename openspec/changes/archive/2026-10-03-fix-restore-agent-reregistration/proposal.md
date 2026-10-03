## Why

The overwrite backup restore rewrites `kb.agents` with raw project-scoped SQL,
bypassing the agents repository's trigger-registration seam. #1418 closed part
of that gap: it tears down the in-memory registrations of agent ids the wipe
removes and the snapshot does not re-create. But agents the snapshot *does*
re-create or newly add are still wrong (issue #1426):

- A re-created agent whose cron schedule or reaction config changed keeps its
  **stale** in-memory registration (the old schedule / old event keys) until a
  process restart, so it fires on the wrong events.
- A newly added restored agent gets **no** registration at all until the next
  restart, so its triggers are silently dead.

The `backup-restore` and `agent-reaction-triggers` specifications only describe
the removal half of this contract, so the re-registration half is spec drift.

## What Changes

- After a successful overwrite-restore commit, reconcile the in-memory trigger
  registrations of every agent id the snapshot re-created or added, from the
  restored rows: tear down any stale registration and register the restored
  configuration. The existing post-commit teardown of removed-and-not-recreated
  ids is unchanged.
- Add a repository restore-listener seam (`AddAgentRestoreListener` /
  `NotifyAgentsRestored`) mirroring the existing `AddAgentDeletionListener` /
  `NotifyAgentsDeleted` pattern; `TriggerService` registers a listener that
  reloads each restored id and runs `SyncAgentTrigger`, which is idempotent.
- Add `Restorer.SetAgentRestoreNotifier` (nil-safe) and wire it in
  `cmd/server/main.go` when both the Backups and Agents features are enabled.
- Extend the `backup-restore` overwrite-restore reconciliation requirement and
  the `agent-reaction-triggers` runtime-agent deletion requirement to cover
  re-registration of re-created/added agents.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `backup-restore`: the overwrite-restore agent-registration reconciliation
  requirement now also covers re-registering agents the snapshot re-creates or
  adds, from the restored rows, idempotently and only after commit.
- `agent-reaction-triggers`: the runtime-agent deletion requirement no longer
  says a re-created id's registrations stay untouched; it now states that a
  re-created or added id remains registered from the restored configuration.

## Impact

- `apps/server/domain/agents/repository.go`: add `AddAgentRestoreListener` /
  `NotifyAgentsRestored` (context-aware, nil-safe) alongside the deletion seam.
- `apps/server/domain/agents/triggers.go`: `TriggerService` registers
  `reconcileRestoredAgents`, which reloads and `SyncAgentTrigger`s each restored
  id.
- `apps/server/domain/backups/restorer.go`: resolve `restoredAgentIDs` and call
  `reconcileRestoredAgents` after `tx.Commit()`; add
  `Restorer.SetAgentRestoreNotifier`.
- `apps/server/cmd/server/main.go`: wire `agentRepo.NotifyAgentsRestored` into
  the restorer when both features are enabled.
- Tests: `apps/server/domain/backups/restorer_agent_reconcile_db_test.go`
  (DB-backed; fails without the fix).
