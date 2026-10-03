## Why

Scoping today is org/project only. A user cannot name a reusable subset of their
knowledge — "the 2024 compliance docs", "everything from the design system", "the
onboarding corpus" — and scope an agent or a search to it. Onyx solves this with
**Document Sets**; Memory's equivalent is a named, addressable **subgraph filter**,
because Memory is graph-first: results are graph objects, document chunks, and
relationships, not just documents.

The same primitive should be reused in three places that today each hand-roll a
project-level filter: unified search (all three legs), agent knowledge scoping, and
chat retrieval. Without a shared collection primitive, each surface would invent its
own membership notion and drift.

## What Changes

- Add `kb.collections` + `kb.collection_items`, where a collection item references a
  `document_id` **or** a graph `canonical_id` — a named subgraph filter, not a
  document-only list. (A `source` item type is **not** in v1: no `kb.sources` table
  exists; source ingestion is future work.)
- Add `CollectionIDs []uuid.UUID` to `UnifiedSearchRequest` (`domain/search/dto.go`) and
  filter the graph, text/chunk, and relationship legs by it.
- Add a graph-object id-set filter (e.g. `CanonicalIDs []uuid.UUID`) to
  `domain/graph.HybridSearchRequest` (`graph/dto.go:472-489`), since the graph leg runs
  through `domain/graph.Service.HybridSearch`, not `domain/search/repository.go`.
- Wire the same collection primitive into agent knowledge scoping and chat retrieval so
  a collection is one reusable filter across surfaces.
- Requirements cover collection CRUD, item add/remove, search filtering by one or more
  collections (AND across multiple), project-scoped membership, empty-collection-yields-
  nothing, and `canonical_id` references resolving to the graph head.

## Capabilities

### New Capabilities

- `knowledge-collections`: a reusable, named, project-scoped subset of documents / graph
  objects, usable as a search and retrieval filter.

### Modified Capabilities

- `search`: `UnifiedSearchRequest` accepts collection ids and applies them as a filter
  across graph, text, and relationship legs.
- `graph` (related, consumed): `HybridSearchRequest` gains a canonical-id filter field
  that the graph-object leg honors (described in the `knowledge-collections` delta; no
  separate `graph` delta is written).

## Impact

- **DB** (`apps/server/migrations/`): `kb.collections` (id, project_id, name, description,
  timestamps) + `kb.collection_items` (collection_id, item_type ∈ {document, canonical},
  item_id, timestamps, unique on (collection_id, item_type, item_id)). `item_id` has a
  real FK to `kb.documents.id` **only** for `item_type='document'`; `canonical` items are
  not FK-constrained (graph objects are soft-deleted and `canonical_id` is non-unique).
- **Server** (`apps/server/domain/`): new `collections` domain (CRUD + membership +
  soft-delete-aware membership cleanup); `domain/search/dto.go` (`CollectionIDs`),
  `domain/search/repository.go` + `service.go` (text + relationship legs);
  `domain/graph/dto.go` + graph hybrid search (graph-object leg via `CanonicalIDs`);
  `domain/agents` (knowledge scoping accepts collection ids), `domain/chat` (retrieval
  accepts collection ids).
- **Canonical head resolution**: reuse `domain/graph` head-resolution (spec
  `graph-head-resolution`) for `canonical_id` items, applied to the graph-object leg.
- **Gateway** (follow-on): collection picker reuse across agent settings and search is
  noted, not implemented here.

## Impact (related changes)

- None blocking. `resource-acl` will later gate *who* can see a collection; this change
  assumes project-scoped visibility only. (Dropping `source` also removes the resource-type
  collision with `add-resource-acl`.)
