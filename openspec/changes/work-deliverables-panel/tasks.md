## 1. Key lookup

- [x] 1.1 Add `LookupGraphObjectByKey(ctx, typeFilter, key)` to `MemoryClient` (`memory_graph.go`) hitting `GET /api/graph/objects/search` with `key`/`type`/`limit=1`/`include_total=false`/`exclude_fields=content`; return `(nil, nil)` on no match
- [x] 1.2 Add the method to `MemoryBackend` (`backend.go`) and `fakeMemory` (`handlers_test.go`)

## 2. Drawer view model

- [x] 2.1 Add `boardDeliverable` type and the pure `boardDeliverables` (flatten + dedupe, newest-first) and `boardCompletionSummary` helpers in `board.go`
- [x] 2.2 Resolve each deliverable via `LookupGraphObjectByKey` in `uiBoardItem`, degrading a miss or failure to `missing`

## 3. Render

- [x] 3.1 Add deliverables and completion-note sections to `BoardDrawer` (`board.templ`): resolved → object link, missing → muted chip, empty → explicit empty state; completion note rendered via `renderMarkdown` + `templ.Raw`

## 4. Tests

- [x] 4.1 Unit-test `boardDeliverables` (newest-wins dedupe, malformed entries dropped, missing key tolerated, empty input) and `boardCompletionSummary`
- [x] 4.2 Drawer contract test: resolved (link + object id), missing (muted marker, no link), empty (empty-state text)
- [x] 4.3 httptest `LookupGraphObjectByKey` (path + query params + no-match → `(nil, nil)`)

## 5. Blueprint

- [x] 5.1 Link deliverables at runtime with a `delivers` edge (Task → Note) instead of declaring a relationship type — the CLI install preflight rejects a relationship target that is not an objectType in the same pack
- [x] 5.2 Instruct the worker to create + link deliverables inline and declare them in `work_complete(deliverables=[...])`

## 6. Build and verify

- [x] 6.1 `templ generate` then `go build ./...` and `go test ./...`
- [x] 6.2 `task lint`
- [x] 6.3 `openspec validate work-deliverables-panel`
