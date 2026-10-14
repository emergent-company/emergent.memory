# Tasks — fix-rework-dispatch-ownership

## 1. Thread the owning agent id

- [x] 1.1 Add `ReworkAgentID *uuid.UUID` to `graph.WorkObjectTransition`.
- [x] 1.2 Add `reworkAgentID *uuid.UUID` param to `graph.Service.RequestChangesWorkObject`; set `t.ReworkAgentID`.
- [x] 1.3 Add `rework_agent_id` to `statusChangeData` and the `status_changed` emit payload.
- [x] 1.4 Update `WorkObjectStore.RequestChangesWorkObject` interface + `RequestChanges` to pass `agent.ID`.

## 2. Suppress owner re-dispatch

- [x] 2.1 In `enqueueWorkRun`, after assignee routing + `workPickupMatch`, skip enqueue when `data["rework_agent_id"] == agent.ID`.
- [x] 2.2 Keep the `status_changed` emit and the explicit rework enqueue (feedback carrier).

## 3. Migration

- [x] 3.1 `00240_allow_work_pickup_event_types.sql`: relax `chk_agent_processing_log_event_type` to admit `status_changed`/`assigned`; Down restores + deletes affected rows.

## 4. Tests

- [x] 4.1 Owner exactly one run carrying `work_feedback` (no double-dispatch).
- [x] 4.2 Second work agent still dispatches on the revision trigger status.
- [x] 4.3 Plain transition into a trigger status dispatches normally (no `rework_agent_id`).
- [x] 4.4 Existing rework claim path (`TestClaimWorkObject_ReworkClaimsFromRevision`) unaffected.

## 5. Verify

- [x] 5.1 `go build ./...`
- [x] 5.2 `go vet ./...`
- [x] 5.3 `go test -short -count=1 ./domain/agents/... ./domain/graph/...`
- [x] 5.4 `REQUIRE_DB=1 ... go test -p 4 -count=1 ./domain/agents/... ./domain/graph/...`
- [x] 5.5 `bash scripts/lint-ratchet.sh`
- [x] 5.6 `go run ./cmd/migration-order-guard -base origin/main`
- [x] 5.7 `openspec validate fix-rework-dispatch-ownership`
