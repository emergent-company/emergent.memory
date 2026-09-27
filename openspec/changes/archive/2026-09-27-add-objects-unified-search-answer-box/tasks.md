## 1. OpenSpec artifacts

- [x] 1.1 Author `proposal.md`, `tasks.md`, and the `object-browser` delta spec `specs/object-browser/spec.md` describing the unified search mode, the knowledge-search answer box, and the membership-enforced authorization posture. Verify: `openspec validate add-objects-unified-search-answer-box --strict` passes.

## 2. Gateway client methods

- [x] 2.1 Add `SearchObjectsUnified` (`POST /api/search/unified`, `resultTypes=graph`) and `QueryKnowledge` (`POST /api/projects/{id}/query`, SSE) in `memory_graph.go` / `memory_knowledge.go`, plus the `unifiedSearchResult` wire struct. Verify: `go test ./...` passes.

## 3. Objects browser handlers

- [x] 3.1 Extend `uiObjects` to dispatch `mode=unified` to `SearchObjectsUnified`; add `uiObjectsKnowledge` POST handler (empty-question redirect, error partial on failure). Verify: `TestUIObjectsUnifiedRoute`, `TestUIObjectsKnowledge` pass.

## 4. Template

- [x] 4.1 Render the third "Unified" radio, the `objectsKnowledge` answer box (loading/error/answer states), the `objectUnifiedRow`, and the shared `objectScoreBadge`. Verify: `TestRenderObjectsPageSearchUnified` passes.

## 5. Build, test, lint

- [x] 5.1 `templ generate`, `go build ./...`, `go test ./...`, `task lint` from `apps/web-ui/gateway` all pass (0 issues).

## 6. Commit and push

- [x] 6.1 Stage the change directory and commit as `docs(openspec): add objects-unified-search-answer-box change (unified search, knowledge answer box)`.
