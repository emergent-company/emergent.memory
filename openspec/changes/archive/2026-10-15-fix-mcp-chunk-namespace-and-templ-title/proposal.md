## Why

Two unrelated, self-contained defects found while reviewing the gateway and the MCP search tools.

**`#1902` — the namespace filter never reached the chunk leg.** `search-hybrid` /
`search-semantic` accept a `namespace` argument. `executeHybridSearch` /
`executeSemanticSearch` put it on `UnifiedSearchRequest.Namespace`, and
`hybridSearchRequestFromUnified` forwards it to the **graph** leg only. The
text/chunk leg builds `search.TextSearchParams{ProjectID, Query, Vector, Mode,
LexicalWeight, VectorWeight, Limit}` — a struct with no `Namespace` field — so
`executeTextSearch` queried `kb.chunks` unscoped. On top of that,
`mapUnifiedToSearchResponse` deliberately exempts `text_chunk` items from the
namespace-derived allow/block sets, because a chunk carries no schema type.

Net effect: with any restricted `namespace` (for example `"system"`) **and**
`include_chunks=true`, the response contained document chunks from *every*
namespace alongside the correctly scoped graph objects. A namespace-scoped agent
tool could read text it was never scoped to.

**`#1903` — a single-line children block renders a bare expression as text.**
Inside a component children block, `{ item.Key }` is the **children delimiter
plus raw text**, not an expression: templ emits
`templruntime.WriteString(buf, n, "item.Key ")`. `#1875` consolidated
`<h3>{ item.Key }</h3>` into `@ui.DialogTitle(...) { item.Key }`, and the board
drawer title silently started rendering the literal string `item.Key` instead of
the work-item key.

## What Changes

### Namespace scoping for the chunk leg (`#1902`)

- `search.TextSearchParams` gains a `Namespace *string`.
- A single `chunkNamespacePredicate` helper renders the extra `WHERE` fragment:
  an `EXISTS` over `kb.object_chunks` → `kb.graph_objects` matching
  `oc.chunk_id = c.id AND o.project_id = d.project_id AND o.deleted_at IS NULL
  AND o.namespace = ?`. A chunk has no namespace of its own; the extraction link
  is what makes it belong to one. The predicate is **fail-closed** — a chunk with
  no live extracted object in the requested namespace is excluded. Branch and
  version scoping are deliberately not applied: namespace is inherited across
  versions within a branch, so a chunk extracted into a superseded or
  namespace-carrying branch version of an object in the requested namespace still
  belongs to it, and a branch-forked copy (which carries no namespace of its own)
  cannot admit a chunk under a namespace it was never extracted into.
- The predicate is applied to **every** chunk query: `runChunkLexical` (and its
  relax/disjoin retries), `VectorSearch`, and both legs of `HybridSearch`. The
  vector leg matters on its own: a chunk can be reached by embedding similarity
  without matching the lexical query.
- `executeTextSearch` passes `req.Namespace` into `TextSearchParams`.
- No change to the default (`""`/`"all"`) paths, and no change to
  `mapUnifiedToSearchResponse` — with the query scoped, chunks that come back
  already belong to the requested namespace. Its comment is corrected to record
  that the namespace restriction is now enforced at the query, not the mapper.

### Single-line expression children (`#1903`)

- `board.templ`: the drawer title children move to their own line inside a
  multi-line block, so templ parses the expression
  (`templ.JoinStringErrs(item.Key)`) instead of literal text.
- A repo-wide gate over the generated `*_templ.go` output fails when a
  raw-text write carries a code-shaped Go expression (selector, call, or index).
  Sweeping the generated output is exact: an expression compiled through
  `JoinStringErrs` never appears as a `WriteString` literal, and prose never
  parses as a Go expression, so no allowlist is needed. A non-vacuity guard
  fails the gate if a glob matches no generated file.
- A rendered-output assertion renders `BoardDrawer` and requires the item's key
  value to appear and the literal `item.Key` not to. A table test pins the
  expression classifier on both sides so it cannot silently widen into prose or
  narrow away from the bug shape.

## Impact

- `apps/server/domain/search/repository.go` — `TextSearchParams`, new
  `chunkNamespacePredicate`, the three chunk queries.
- `apps/server/domain/search/service.go` — `executeTextSearch` forwards the
  namespace.
- `apps/server/domain/search/dto.go` — `UnifiedSearchRequest.Namespace` comment
  now records that the filter covers both legs.
- `apps/server/domain/mcp/graph_tools.go` — comment correction only.
- `apps/web-ui/gateway/board.templ` — the drawer title.
- `apps/web-ui/gateway/board_drawer_title_test.go` — new guards.
- `apps/server/domain/search/chunk_namespace_test.go` — new tests.
