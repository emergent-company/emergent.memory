## 1. Blueprint Unapply project scope (#1361 item 2)

- [x] 1.1 Add `project_id` predicate to `Repository.DeleteAgentsBySourceBlueprint` and thread `projectID` from `blueprints.Service.Unapply` (`apps/server/domain/agents/repository.go`, `apps/server/domain/blueprints/service.go`).
- [x] 1.2 Update the `blueprints.AgentRepo` interface and the `apply_test.go` fake to the project-scoped signature.
- [x] 1.3 DB-backed test: the same blueprint source id applied in two projects; Unapply in project A removes only project A's agent.

## 2. Overwrite restore teardown (#1361 item 1)

- [x] 2.1 Export `Repository.NotifyAgentsDeleted` as the seam for delete paths outside the repository.
- [x] 2.2 Resolve, inside the restore transaction, the project's agent ids the wipe removes and the snapshot does not re-create (`removedAgentIDs`).
- [x] 2.3 Fire the deletion seam for those ids only after `tx.Commit()`, leaving re-created ids' registrations intact; add `Restorer.SetAgentDeletionNotifier` (nil-safe).
- [x] 2.4 Wire the restorer notifier to the agents repository in `cmd/server/main.go`, only when both Backups and Agents features are enabled.
- [x] 2.5 DB-backed test: overwrite restore tears down the dropped id's cron + reaction registrations and leaves the re-created id's registrations intact.

## 3. Spec

- [x] 3.1 Extend `agent-reaction-triggers` "Deleting a runtime agent removes its trigger registrations" to cover the overwrite restore and the re-created-id carve-out.
- [x] 3.2 Add the `backup-restore` overwrite-restore agent-registration reconciliation requirement.

## 4. Verify

- [x] 4.1 `cd apps/server && go build ./...`.
- [x] 4.2 `TEST_DATABASE_URL=... REQUIRE_DB=1 go test -count=1 ./domain/agents/... ./domain/backups/...`.
- [x] 4.3 `bash apps/server/scripts/lint-ratchet.sh`.
- [x] 4.4 `openspec validate fix-agent-delete-seam-gaps --strict`.
