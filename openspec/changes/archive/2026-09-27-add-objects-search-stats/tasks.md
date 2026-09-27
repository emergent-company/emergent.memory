## 1. OpenSpec artifacts

- [x] 1.1 Author `proposal.md`, `design.md`, `tasks.md`, and the `object-browser` delta spec `specs/object-browser/spec.md` describing the shipped search, stats, cursor-pagination, and authorization posture. Verify: `openspec validate add-objects-search-stats --strict` passes.

## 2. Gateway client methods

- [x] 2.1 Add `ListGraphObjectsPage` (`GET /api/graph/objects/search` with `limit`, `cursor`, `include_total=false`), `CountObjects` (`GET /api/graph/objects/count`), and `SearchObjects` (fulltext `GET /api/graph/objects/fts` / hybrid `POST /api/graph/search`) in `memory_graph.go`, plus `ObjectSearchResult`. Verify: `go test ./...` passes.

## 3. Objects browser handlers

- [x] 3.1 Rewrite `uiObjects` to branch search vs browse, fetch branches/types/page/count/progress in parallel, and populate `objectsStats`/`objectsPageData`. Verify: `go test ./...` passes.
- [x] 3.2 Add `uiObjectsPartial` (load-more fragment; 502 on failure so the button is retained) and `objectsPartialURL` carrying cursor/query/mode/type/branch forward. Verify: `TestUIObjectsPartialFailure` passes.

## 4. Template

- [x] 4.1 Render the search form (query + fulltext/hybrid segmented control, `aria-label="Search objects"`), the three-tile stats section (unavailable `—` on error), ranked search rows with muted relevance score, the three empty states, and the HTMX load-more. Verify: `TestRenderObjectsPageStatsUnavailable` passes.

## 5. Build, test, lint

- [x] 5.1 `templ generate`, `go build ./...`, `go test ./...`, `task lint` from `apps/web-ui/gateway` all pass (0 issues).

## 6. Commit and push

- [x] 6.1 Stage only the change directory and commit as `docs(openspec): add objects-search-stats change (search, stats, cursor pagination)`, push to `feat/objects-search-stats`.
