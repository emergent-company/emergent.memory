## 1. Search: namespace scoping for the chunk leg (#1902)

- [x] 1.1 Add `Namespace *string` to `search.TextSearchParams` in `apps/server/domain/search/repository.go`.
- [x] 1.2 Add `chunkNamespacePredicate(namespace *string) (string, []any)` returning the fail-closed `EXISTS` over `kb.object_chunks` → `kb.graph_objects` (`oc.chunk_id = c.id`, `o.project_id = d.project_id`, `o.deleted_at IS NULL`, `o.namespace = ?`), and `("", nil)` for a nil/empty namespace. Branch/version scoping is deliberately not applied — namespace belongs to the object identity.
- [x] 1.3 Apply it in `runChunkLexical` (so the relax/disjoin retries inherit it), `VectorSearch`, and both legs of `HybridSearch` — the vector leg included, since embedding similarity alone can reach an out-of-namespace chunk.
- [x] 1.4 Forward `req.Namespace` into `TextSearchParams` in `executeTextSearch` (`apps/server/domain/search/service.go`).
- [x] 1.5 Correct the `mapUnifiedToSearchResponse` chunk comment (`apps/server/domain/mcp/graph_tools.go`) — the namespace restriction is now enforced in the chunk query, not the mapper — and the `UnifiedSearchRequest.Namespace` comment in `domain/search/dto.go`.
- [x] 1.6 Add `apps/server/domain/search/chunk_namespace_test.go`: predicate shape + args (and nil/empty no-op), plus sqlmock assertions that the lexical, vector, and both hybrid queries are project-bounded, exclude soft-deleted objects, and carry the requested namespace at the 2x hybrid limit.

## 2. Web UI: single-line expression children (#1903)

- [x] 2.1 Fix the board drawer title in `apps/web-ui/gateway/board.templ` to a multi-line children block so templ emits `templ.JoinStringErrs(item.Key)` (verified in the generated `board_templ.go`).
- [x] 2.2 Add a gate over the generated `*_templ.go` output failing on a raw-text write whose literal is a code-shaped Go expression (selector/call/index that also parses with `go/parser.ParseExpr`), with a per-glob non-vacuity guard.
- [x] 2.3 Add a rendered-output assertion that `BoardDrawer` renders the item's key value and never the literal `item.Key`, plus a table test pinning the expression classifier on both sides.
- [x] 2.4 Prove all guards non-vacuous by reverting the template to the single-line form (rendered-output assertion and generated-output gate both fail), then restoring it byte-identical.

## 3. Verification

- [x] 3.1 `go build ./...` in `apps/server` and `apps/web-ui/gateway` (the latter after `templ generate` + `task css`).
- [x] 3.2 `go test ./domain/search/... ./domain/mcp/...` and the gateway package.
- [x] 3.3 `templ generate` produces no literal-expression output (`board_templ.go` carries `JoinStringErrs(item.Key)`; the generated-output gate reports 0 hits across 67 generated files).
- [x] 3.4 Repo lint (`task lint`) green.
- [x] 3.5 `openspec validate fix-mcp-chunk-namespace-and-templ-title --strict` passes.
