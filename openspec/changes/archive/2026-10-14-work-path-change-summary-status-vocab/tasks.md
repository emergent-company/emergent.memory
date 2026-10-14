## 1. Server — record the work-path change summary (#1850)

- [x] 1.1 `workTransitionChangeSummary(oldProps, oldStatus, newProps, oldAssignee, newAssignee)` — reuse `computeChangeSummary` (normalizing the previous status onto the old properties so the move is an updated `/status` even on the first transition), append a synthetic `/assignee` update when the assignee moves, and return nil when nothing changed.
- [x] 1.2 `transitionWorkObject` sets `newVersion.ChangeSummary` from the helper; the status-transition and reassign paths record `/status` and `/assignee` respectively, and the unassign path records both.
- [x] 1.3 Tests (`work_transition_test.go`): complete records `/status` from→to; reassign records `/assignee` and no `/status`; unassign records both (no hidden assignee change).

## 2. Server — close the subgraph status-vocabulary door (#1851)

- [x] 2.1 `validateSubgraphWorkStatus(actorType, schemas, objects)` — pure guard applying `rejectAgentWorkStatusWrite` + `validateTypeStatus` per object, unknown/unconstrained types inert, nil schema set inert (matches `validateSubgraphObjectKeys`).
- [x] 2.2 `CreateSubgraph` resolves the actor and calls the guard before the transaction, so a rejected status fails the batch atomically with no partial writes.
- [x] 2.3 Tests: `work_status_guard_test.go` (`TestValidateSubgraphWorkStatus` unit) and `work_config_db_test.go` (`TestCreateSubgraph_RejectsStatusOutsideAllowedSet` / `..._AcceptsStatusInAllowedSet`, atomic rejection).

## 3. Verification

- [x] 3.1 Red-before / green-after confirmed by stashing the production wiring (subgraph reject test created the object; the three summary tests saw an empty summary).
- [x] 3.2 `go build ./...` + `go vet ./domain/graph/... ./domain/agents/...` + `gofmt -l` clean from `apps/server`.
- [x] 3.3 `go test ./domain/graph/... ./domain/agents/...` green against real Postgres.
- [x] 3.4 `openspec validate work-path-change-summary-status-vocab`.
