## 1. Partial index migration

- [ ] 1.1 Add `apps/server/migrations/00163_add_graph_objects_head_main_index.sql` creating partial index `idx_graph_objects_head_main ON kb.graph_objects (project_id, created_at DESC, id DESC) WHERE supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL`, plus `ANALYZE kb.graph_objects;`, with matching `-- +goose Down` dropping the index. Follow the `-- +goose NO TRANSACTION` + `CREATE INDEX CONCURRENTLY` convention (see `00162_add_graph_objects_type_search_index.sql`), including `DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_head_main;` before create. Verify: migration parses and applies on the dev DB.
- [ ] 1.2 Confirm the planner uses the new index: after applying, `EXPLAIN SELECT ... WHERE project_id = ? AND supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 101` shows an `Index Scan`, not `Seq Scan`. Verify: plan output.

## 2. Parallelize object detail handler

- [ ] 2.1 In `apps/web-ui/gateway/objects.go` `uiObject`, fetch the four independent sub-resources (compiled types, object edges, similar objects, label suggestions) concurrently via `errgroup`, following the existing `uiObjects` pattern (separate named error vars per call, `g.Go(func() error { ...; return nil })`, `_ = g.Wait()`, then `captureError`). Keep `GetGraphObject` up front (needed for the error page + `obj.Type`), and run `loadRelatedObjects` after `g.Wait()` (it depends on `edges`). Preserve all existing error/flash behavior. Verify: `go build ./...` compiles in `apps/web-ui/gateway`.

## 3. Cache label suggestions

- [ ] 3.1 Add a TTL-guarded in-memory label cache to `Server` (mutex + `map[string]labelCacheEntry`), initialized where `Server` is constructed. Key by the current project ID via `s.memory.projectIDFor(ctx)`. Rewrite `objectLabelSuggestions` to return a non-expired cached entry, else fetch via `ListGraphObjects`, cache with a ~60s TTL, and return. Skip caching when the project ID is empty. Verify: `go build ./...` compiles and `go test ./...` passes.

## 4. Build, lint, and verify

- [ ] 4.1 `go build ./...` and `go test ./...` in `apps/web-ui/gateway`; `go build ./...` in `apps/server`. Fix issues until clean. Verify: all commands succeed.
- [ ] 4.2 `task lint` in `apps/web-ui/gateway`. Verify: lint clean.
- [ ] 4.3 Run `openspec validate objects-list-performance` from the repo root. Verify: validation passes.
