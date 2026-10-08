# Tasks — fix-subgraph-key-guard

## 1. Shared guard

- [x] 1.1 Extract the board-enabled stable-key rule into `requireBoardEnabledKey(cfg, key)` in `work_status_guard.go` and call it from `Service.Create` (behaviour-preserving).
- [x] 1.2 Add `validateSubgraphObjectKeys(schemas, objects)` applying the guard to every object in a subgraph request.

## 2. Subgraph entry point

- [x] 2.1 Call `validateSubgraphObjectKeys` in `CreateSubgraph` after schemas are loaded and before `BeginTx`, so a keyless board-enabled object rejects the batch with no partial writes.
- [x] 2.2 Return the canonical `workStatusKeyRequired()` error (HTTP 400, code `work_status_key_required`).

## 3. Spec

- [x] 3.1 Add the `object-driven-agent-work` delta restating the "Correct, crash-safe claim" requirement and its scenarios.
- [x] 3.2 Add the scenario: a board-enabled object created via the subgraph/bulk path without a key is rejected with `work_status_key_required` and creates nothing.

## 4. Tests

- [x] 4.1 `TestValidateSubgraphObjectKeys`: board-enabled without/with-empty key rejected; whole batch rejected when any object is keyless; board-enabled with key passes; non-board and unknown types without key pass; nil schemas inert.
- [x] 4.2 `TestCreateSubgraph_RejectsKeylessBoardEnabledBeforeWrite`: the real `CreateSubgraph` entry point rejects a keyless board-enabled object before any DB work (nil repo proves no transaction is reached).
- [x] 4.3 Update `e2e/tests/api/graph_subgraph_test.go`: add keys to the board-enabled `Task` objects it created keyless (ObjectsOnly/SameBranch/DifferentBranches/NilBranches) and add `TestGraphSubgraph_CreateSubgraph_KeylessBoardEnabledRejected` asserting the keyless board-enabled subgraph create is rejected with 400.

## 5. Verification

- [x] 5.1 `go build ./...` and `go vet ./...` from `apps/server`.
- [x] 5.2 `go test ./domain/graph/... -count=1`.
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main ./domain/graph/...` (0 new).
- [x] 5.4 `openspec validate fix-subgraph-key-guard --strict`.
