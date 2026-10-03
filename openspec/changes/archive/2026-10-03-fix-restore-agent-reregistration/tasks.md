## 1. Re-registration seam

- [x] 1.1 Add `Repository.AddAgentRestoreListener` / `NotifyAgentsRestored` (context-aware, nil-safe) mirroring the deletion seam (`apps/server/domain/agents/repository.go`).
- [x] 1.2 `TriggerService` registers `reconcileRestoredAgents`, which reloads each restored id via `FindByID` and runs the idempotent `SyncAgentTrigger` (`apps/server/domain/agents/triggers.go`).
- [x] 1.3 Resolve `restoredAgentIDs` in `Restorer`, add `SetAgentRestoreNotifier`, and reconcile only after `tx.Commit()` (`apps/server/domain/backups/restorer.go`).
- [x] 1.4 Wire `agentRepo.NotifyAgentsRestored` into the restorer in `cmd/server/main.go` when both Backups and Agents features are enabled.

## 2. Tests (TDD)

- [x] 2.1 DB-backed test: an overwrite restore that re-creates an agent with a changed reaction config replaces the stale registration with the restored one, and an agent newly added by the snapshot is registered (`apps/server/domain/backups/restorer_agent_reconcile_db_test.go`).
- [x] 2.2 Verify the test fails without the reconcile call (mutation), then revert.

## 3. Spec

- [x] 3.1 Modify the `backup-restore` overwrite-restore reconciliation requirement to cover re-registration of re-created/added agents.
- [x] 3.2 Modify the `agent-reaction-triggers` runtime-agent deletion requirement for re-created/added ids.

## 4. Verify

- [x] 4.1 `cd apps/server && go build ./...`.
- [x] 4.2 `TEST_DATABASE_URL=... REQUIRE_DB=1 go test -count=1 ./domain/backups/... ./domain/agents/...`.
- [x] 4.3 `bash apps/server/scripts/lint-ratchet.sh`.
- [x] 4.4 `openspec validate fix-restore-agent-reregistration --strict`.
