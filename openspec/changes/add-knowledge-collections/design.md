## Context

Memory is graph-first. "Document sets" (Onyx) model a subset of *documents*; Memory's
equivalent must model a subset of *addressable knowledge* — graph objects, document
chunks, and relationships. The existing project-only filter is applied independently in
three places: `domain/search` (text + relationship legs via `repository.go`/`service.go`
and the graph-object leg via `domain/graph.Service.HybridSearch`), agent knowledge
scoping (`domain/agents`), and chat retrieval (`domain/chat`). A collection is a named
subgraph filter that can be reused in all three.

## Goals / Non-Goals

**Goals**

- CRUD for named, project-scoped collections with heterogeneous item references
  (document / canonical).
- A single collection-filter primitive applied to all three search legs with AND
  semantics across multiple collections.
- Reuse in agent scoping and chat retrieval.
- Canonical-id items resolve to the graph head (no stale snapshots).

**Non-Goals**

- A `source` item type (no `kb.sources` table exists; source ingestion is future work).
- Per-item ACL (deferred to `resource-acl`); collections are project-visible here.
- Automatic membership (e.g. "all docs matching a query"). Items are explicit.
- Nested/hierarchical collections.
- Collection metadata beyond name + description.

## Decisions

### D1 — Collection as a subgraph filter, not a document list

`collection_items.item_type ∈ {document, canonical}`. This is the single decision that
makes collections graph-first: a collection can hold a document or a graph object (by
`canonical_id`). A document-only model (Onyx-style) was rejected because it cannot
express "these specific graph objects" and would silently lose the relationship/object
legs of search. `source` is **excluded** from v1 (no `kb.sources` table exists).

### D2 — AND semantics across multiple collections

A search filtered by collections A and B returns the intersection. This matches Onyx's
document-set combination and is the least surprising reading of "filter by these sets".
OR semantics (union) would require an explicit operator field and was rejected as
premature; if needed later it can be added behind a `collectionMode` field without a
migration.

### D3 — Filtering plumbing (search legs + graph hybrid search)

`UnifiedSearchRequest` gains `CollectionIDs []uuid.UUID`. A shared resolver
(`resolveCollectionItemIDs(ctx, projectID, collectionIDs)`) returns the allowed
`document_id`s and head-resolved `canonical_id`s for the requested collections, resolved
once per request. Each surface then applies its own predicate:

- **Text/chunk leg** (`domain/search` → `kb.documents`): `document_id IN (document_set)`.
- **Graph-object leg** (`domain/graph.Service.HybridSearch`): a new id-set filter field
  `CanonicalIDs []uuid.UUID` on `HybridSearchRequest` (`graph/dto.go:472-489`), with a
  predicate inside `domain/graph` restricting object candidates to that canonical set.
  This leg is **not** filtered in `domain/search/repository.go` — it runs through
  `domain/graph`.
- **Relationship leg** (`domain/search` → `kb.graph_relationships`): canonical-to-
  canonical — `r.src_id IN (canonical_set) OR r.dst_id IN (canonical_set)`. (Note
  `graph_relationships.src_id`/`dst_id` already store `canonical_id`, per migration
  `00015`.) A relationship matches a collection when either endpoint belongs to it.

Head resolution (D4) applies to the **graph-object leg only**: `canonical_id` items are
resolved to head to produce the `canonical_set`, which the relationship leg then matches
against `src_id`/`dst_id` directly (no further head resolution on the relationship leg).

Membership is applied as a filter — never as a post-hoc in-memory filter on the full
candidate set (that would break candidate caps and scores).

**Enforcement split (write vs read).** Cross-project item membership is rejected at
**write time** (the `domain/collections` service refuses to add an item whose object
belongs to a different project). Cross-project *search* filtering is a **read** concern:
a search that names a collection id from another project simply resolves to an empty
allowed-id set and returns nothing — it is not a write-time error. These are two distinct
surfaces and do not diverge: write rejects, read filters.

### D4 — Canonical head resolution (graph-object leg only)

A `canonical` item stores the `canonical_id`; at filter-build time the resolver expands
it to the current head via `domain/graph` head resolution (spec `graph-head-resolution`),
and that head set becomes the graph-object leg's `CanonicalIDs` filter and the
relationship leg's `canonical_set`. This keeps collections live for the object leg. The
raw `canonical_id` is still stored (so history is stable) but the *effective* membership
used for search is the head. Head resolution is **narrowed**: it follows the current
head of the stored `canonical_id`, but does **not** follow a merge/rename into a *new*
`canonical_id` — a merged object with a fresh canonical id is not auto-added.

### D5 — Empty / dangling membership = empty result, explicitly

An empty collection produces zero allowed ids, so every leg returns nothing. This is
specified explicitly (not left to SQL `IN ()` semantics, which vary across the legs'
query shapes). A collection whose items all resolve to soft-deleted or dangling objects
(e.g. a `canonical` item whose head no longer exists) behaves the same as an empty
collection: it contributes no results and is not an error.

## Risks / Trade-offs

- **Membership drifts from source.** A deleted document's membership row must be cleaned.
  Mitigation: `item_id` has a real FK to `kb.documents.id` (`ON DELETE CASCADE`) **only**
  for `item_type='document'`; `canonical` items use application-side, soft-delete-aware
  membership cleanup (a job/check that drops items whose head no longer resolves). There
  is no graph-object cascade — `graph_objects.canonical_id` is non-unique and graph
  objects are soft-deleted, so a polymorphic FK is impossible.
- **Head resolution cost.** Expanding every canonical item to head adds a lookup per
  collection at filter-build. Mitigation: resolve once per request and cache within the
  request; collection sizes are expected small (hundreds, not millions).
- **Cross-surface reuse pressure.** Agents and chat must call the same resolver, not a
  copy. Mitigation: the resolver lives in `domain/search` (or a shared `pkg`) and is
  imported, with a unit test asserting the three surfaces use the same id-set for the
  same collection.
- **AND vs OR ambiguity.** Documented in D2; the choice is explicit and changeable later.
