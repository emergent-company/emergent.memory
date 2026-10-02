## 1. entity-type-list relationship aggregation opt-in

- [ ] 1.1 Add `include_relationships` boolean property to the `entity-type-list` InputSchema and extend the tool description to state relationship types are only returned when `include_relationships=true`. Verify: `go build ./...`.
- [ ] 1.2 In `executeListEntityTypes`, parse `include_relationships` and gate the relationship-types query so it only runs when true (leaving `relTypes` nil otherwise). Verify: `go build ./...`.
- [ ] 1.3 Fix the relationship-types SQL to add `gr.supersedes_id IS NULL`, `src.project_id = ?`, and `dst.project_id = ?`, and rebuild the argument slice in exact placeholder order (`[projectUUID, projectUUID, projectUUID]` no-branch; `[projectUUID, branchID, projectUUID, branchID, projectUUID, branchID]` with branch). Verify: `go test ./domain/mcp/...` DB-backed test passes.
- [ ] 1.4 Update `readEntityTypesResource` to call `executeListEntityTypes` with `include_relationships: true` so the `memory://schema/entity-types` resource output is preserved. Verify: `go build ./...`.

## 2. search-hybrid relationship candidates opt-in

- [ ] 2.1 Add `include_relationships` boolean property to the `search-hybrid` InputSchema. Verify: `go build ./...`.
- [ ] 2.2 In `executeHybridSearch`, parse `include_relationships`, cap `limit` to `effectiveEntityQueryFullMaxLimit()` when `field_strategy="full"`, and set `IncludeRelationships` on the unified request. Verify: `go test ./domain/mcp/...`.

## 3. Unified search skips the relationship leg and bounds embedding

- [ ] 3.1 Add `IncludeRelationships *bool` to `search.UnifiedSearchRequest`. Verify: `go build ./...`.
- [ ] 3.2 In `runParallelSearches`, skip the relationship goroutine when `IncludeRelationships != nil && !*IncludeRelationships`. Verify: `go test ./domain/search/...`.
- [ ] 3.3 Wrap `embedQuery`'s provider call in a timeout (e.g. `queryEmbedTimeout = 20s`), keeping the lexical-only fallback. Verify: `go build ./...`.

## 4. Graph-query-agent prompt

- [ ] 4.1 Update `graphQueryAgentSystemPrompt` to warn against `entity-type-list` for simple lookups and to use `include_relationships` only for relationship-centric questions. Verify: `go build ./...`.

## 5. Build, test, lint

- [ ] 5.1 Run `go build ./...`, `go test ./domain/mcp/... ./domain/search/... ./domain/agents/... ./domain/graph/...`, and `task lint`; fix until clean.
