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

- Add `kb.collections` + `kb.collection_items`, where a collection item may reference a
  `document_id` **or** a graph `canonical_id` **or** a source reference — a named
  subgraph filter, not a document-only list.
- Add `CollectionIDs []uuid.UUID` to `UnifiedSearchRequest` (`domain/search/dto.go`) and
  filter **all three search legs** (graph, text/chunk, relationship) by it.
- Wire the same collection primitive into agent knowledge scoping and chat retrieval so
  a collection is one reusable filter across surfaces.
- Requirements cover collection CRUD, item add/remove, search filtering by one or more
  collections (AND across multiple), project-scoped membership, empty-collection-yields-
  nothing, and `canonical_id` references resolving to the graph head.

## Capabilities

### New Capabilities

- `knowledge-collections`: a reusable, named, project-scoped subset of documents / graph
  objects / sources, usable as a search and retrieval filter.

### Modified Capabilities

- `search`: `UnifiedSearchRequest` accepts collection ids and applies them as a filter
  across graph, text, and relationship legs.

## Impact

- **DB** (`apps/server/migrations/`): `kb.collections` (id, project_id, name, description,
  timestamps) + `kb.collection_items` (collection_id, item_type ∈
  {document|canonical|source}, item_id, timestamps, unique on (collection_id, item_type,
  item_id)).
- **Server** (`apps/server/domain/`): new `collections` domain (CRUD + membership);
  `domain/search/dto.go` (`CollectionIDs`), `domain/search/repository.go` +
  `service.go` (filter all three legs), `domain/agents` (knowledge scoping accepts
  collection ids), `domain/chat` (retrieval accepts collection ids).
- **Canonical head resolution**: reuse `domain/graph` head-resolution (spec
  `graph-head-resolution`) for `canonical_id` items.
- **Gateway** (follow-on): collection picker reuse across agent settings and search is
  noted, not implemented here.

## Impact (related changes)

- None blocking. `resource-acl` will later gate *who* can see a collection; this change
  assumes project-scoped visibility only.
