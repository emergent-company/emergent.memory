## Why

`Service.Create` enforces the board-enabled stable-key rule (`isBoardEnabledConfig(workCfg) && (req.Key == nil || *req.Key == "")` → `workStatusKeyRequired()`) before persisting an object. `Service.CreateSubgraph` — the atomic bulk path behind `POST /api/graph/subgraph` — performs the same property validation and insert, but never applied that guard.

As a result a board-enabled type (e.g. `Task`) could be created keyless through the subgraph/bulk path. Because the claim lock keys on `(project, type, key)`, such an object can be dispatched by a listener but can never be claimed — a silent dead-end that the single-create path already prevents (issue #1655).

## What Changes

- Extract the rule into one shared helper, `requireBoardEnabledKey(cfg, key)`, and call it from both `Create` and the subgraph path so the rule cannot drift between entry points.
- Add `validateSubgraphObjectKeys(schemas, objects)`, a pure function that applies the guard to **every** object in a `CreateSubgraphRequest`, and call it in `CreateSubgraph` **before** `BeginTx`, so a keyless board-enabled object rejects the whole batch atomically with **no partial writes** (no objects, no relationships). Rejecting mid-transaction would also roll back, but up-front validation makes "nothing was written" explicit and avoids opening a transaction for an already-invalid batch.
- The rejection is the canonical `workStatusKeyRequired()` error — HTTP 400, code `work_status_key_required` — identical to the single-create path.
- No other subgraph semantics change. In particular the guard inherits the subgraph path's existing fail-open behaviour when the schema provider is unavailable (schemas `nil` → guard inert), matching its property-validation handling; the single-create path continues to fail closed on a provider error.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `object-driven-agent-work`: the "Correct, crash-safe claim" requirement's board-enabled stable-key rule now explicitly covers the subgraph/bulk create path, not only single-object creation.

## Impact

- `apps/server/domain/graph/work_status_guard.go` — new `requireBoardEnabledKey` and `validateSubgraphObjectKeys` helpers.
- `apps/server/domain/graph/service.go` — `Create` reuses the shared helper; `CreateSubgraph` validates all objects up front before the transaction.
- `apps/server/domain/graph/subgraph_key_guard_test.go` — hermetic unit tests for the guard matrix (board-enabled/not, keyed/keyless) and the real `CreateSubgraph` entry point.
- `e2e/tests/api/graph_subgraph_test.go` — the board-enabled `Task` objects these subgraph tests created keyless now carry keys (they previously relied on the very hole being fixed, mirroring #1650 for the single-create tests); new negative test asserts a keyless board-enabled object is rejected.
- `openspec/changes/fix-subgraph-key-guard/` — this change.
- No API shape, response shape, or migration change. Behaviour change is limited to rejecting previously-accepted keyless board-enabled objects via the subgraph path.
