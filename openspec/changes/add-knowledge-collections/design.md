## Context

Memory is graph-first. "Document sets" (Onyx) model a subset of *documents*; Memory's
equivalent must model a subset of *addressable knowledge* — graph objects, document
chunks, and sources. The existing project-only filter is applied independently in three
places: `domain/search` (all three legs via `repository.go`/`service.go`), agent
knowledge scoping (`domain/agents`), and chat retrieval (`domain/chat`). A collection is
a named subgraph filter that can be reused in all three.

## Goals / Non-Goals

**Goals**

- CRUD for named, project-scoped collections with heterogeneous item references
  (document / canonical / source).
- A single collection-filter primitive applied to all three search legs with AND
  semantics across multiple collections.
- Reuse in agent scoping and chat retrieval.
- Canonical-id items resolve to the graph head (no stale snapshots).

**Non-Goals**

- Per-item ACL (deferred to `resource-acl`); collections are project-visible here.
- Automatic membership (e.g. "all docs matching a query"). Items are explicit.
- Nested/hierarchical collections.
- Collection metadata beyond name + description.

## Decisions

### D1 — Collection as a subgraph filter, not a document list

`collection_items.item_type ∈ {document, canonical, source}`. This is the single
decision that makes collections graph-first: a collection can hold a document, a graph
object (by `canonical_id`, resolved to head), or a source reference. A document-only
model (Onyx-style) was rejected because it cannot express "these specific graph objects"
and would silently lose the relationship/object legs of search.

### D2 — AND semantics across multiple collections

A search filtered by collections A and B returns the intersection. This matches Onyx's
document-set combination and is the least surprising reading of "filter by these sets".
OR semantics (union) would require an explicit operator field and was rejected as
premature; if needed later it can be added behind a `collectionMode` field without a
migration.

### D3 — Filtering plumbing in `domain/search`

`UnifiedSearchRequest` gains `CollectionIDs []uuid.UUID`. Each leg's repository query
adds an `EXISTS` / `IN` predicate against `kb.collection_items` for the requested
collection ids and project. Because the three legs have different tables
(`kb.documents` for text, graph object tables + `kb.graph_relationships` for
graph/relationship), the predicate is translated per leg but shares one resolver
function (`resolveCollectionItemIDs(ctx, projectID, collectionIDs)`) that returns the
allowed `document_id`s / `canonical_id`s / source refs. Membership is resolved once per
request, then applied as a filter — never as a post-hoc in-memory filter on the full
candidate set (that would break candidate caps and scores).

### D4 — Canonical head resolution

A `canonical` item stores the `canonical_id`; at filter-build time the resolver expands
it to the current head via `domain/graph` head resolution (spec `graph-head-resolution`).
This keeps collections live: renaming/merging a graph object does not orphan the
membership. The raw `canonical_id` is still stored (so history is stable) but the
*effective* membership used for search is the head.

### D5 — Empty collection = empty result, explicitly

An empty collection produces zero allowed ids, so every leg returns nothing. This is
specified explicitly (not left to SQL `IN ()` semantics, which vary across the three
legs' query shapes).

## Risks / Trade-offs

- **Collection membership drifts from source.** A deleted document's membership row
  dangles unless cascade-cleaned. Mitigation: `ON DELETE CASCADE` from documents/graph
  objects to `collection_items`, plus a store test asserting deletion cleanup.
- **Head resolution cost.** Expanding every canonical item to head adds a lookup per
  collection at filter-build. Mitigation: resolve once per request and cache within the
  request; collection sizes are expected small (hundreds, not millions).
- **Cross-surface reuse pressure.** Agents and chat must call the same resolver, not a
  copy. Mitigation: the resolver lives in `domain/search` (or a shared `pkg`) and is
  imported, with a unit test asserting the three surfaces use the same id-set for the
  same collection.
- **AND vs OR ambiguity.** Documented in D2; the choice is explicit and changeable later.
