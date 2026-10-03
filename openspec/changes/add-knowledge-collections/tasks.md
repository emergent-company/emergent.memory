## 1. Migration — collections tables

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_collections.sql`: `kb.collections` (id, project_id, name, description, created_at, updated_at) and `kb.collection_items` (id, collection_id, item_type ∈ {document, canonical}, item_id, created_at, `UNIQUE (collection_id, item_type, item_id)`). `item_id` carries a real FK to `kb.documents.id` (`ON DELETE CASCADE`) **only** for `item_type='document'`; `canonical` items have no FK (soft-delete-aware app cleanup). Index on `(collection_id)`.
- [ ] 1.2 (TDD) Migration round-trip test: up creates both tables with the unique constraint; down drops cleanly; cascade delete from a document removes only its `document`-type membership rows.

## 2. Collections domain — CRUD + membership

- [ ] 2.1 New `domain/collections` package: Bun entities, `store.go` (create/update/delete/list, add/remove item, resolve member ids, soft-delete-aware membership cleanup for dangling `canonical` items), `service.go` (project-scope checks, duplicate-membership idempotency, write-time cross-project rejection), `handler.go`, `module.go`.
- [ ] 2.2 (TDD) Store unit test: create/update/delete persist; delete removes items; duplicate add does not create a second row; remove deletes only the membership row.
- [ ] 2.3 (TDD) Service unit test: cross-project item add rejected at write time; cross-project collection access rejected; list returns only the caller's project collections.
- [ ] 2.4 (TDD) Handler unit test: routes for collection CRUD and item add/remove validate and map to the DTOs.

## 3. Membership resolver + canonical head

- [ ] 3.1 Add `resolveCollectionItemIDs(ctx, projectID, collectionIDs)` in `domain/search` (or shared `pkg`) returning allowed `document_id`s and head-resolved `canonical_id`s for the requested collections.
- [ ] 3.2 Resolve `canonical` items to current head via `domain/graph` head resolution (following the current head, NOT a merge into a new canonical_id).
- [ ] 3.3 (TDD) Unit test: resolver returns the allowed item ids per requested collection and per type, and the search combines multiple collections with AND (intersection) semantics (spec `search`: Multiple collections are AND-combined) — NOT a union; a canonical item returns its head; a merge into a new canonical id is not followed; an empty collection returns empty sets; cross-project collection ids return empty sets; an all-dangling collection returns empty sets (no error).

## 4. Search filtering across three legs

- [ ] 4.1 Add `CollectionIDs []uuid.UUID` to `UnifiedSearchRequest` in `domain/search/dto.go`.
- [ ] 4.2 In `domain/search/repository.go`, apply the collection filter to the text/chunk leg (`document_id IN (document_set)`) and the relationship leg (`src_id IN (canonical_set) OR dst_id IN (canonical_set)`), each via the resolver's allowed id sets.
- [ ] 4.3 Add `CanonicalIDs []uuid.UUID` to `domain/graph.HybridSearchRequest` (`graph/dto.go:472-489`) and apply the predicate inside `domain/graph` hybrid search — the graph-object leg is filtered here, NOT in `domain/search/repository.go`.
- [ ] 4.4 (TDD) Unit test: a search with a collection returns only members on each leg (graph-object via `domain/graph`, text + relationship via `domain/search`); multiple collections AND-combine; a cross-project collection returns nothing; an empty collection returns nothing.
- [ ] 4.5 (TDD) Unit test: no collection ids → behaviour identical to today (project-id filter only).

## 5. Reuse in agent scoping and chat retrieval

- [ ] 5.1 `domain/agents`: knowledge scoping accepts collection ids and resolves them through the same resolver.
- [ ] 5.2 `domain/chat`: retrieval request accepts collection ids and resolves them through the same resolver.
- [ ] 5.3 (TDD) Unit test: agent scoped to a collection retrieves only members; chat retrieval scoped to a collection retrieves only members; both surfaces use the resolver (no divergent membership logic).

## 6. Verify

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `go test ./...` for the touched modules (`domain/collections`, `domain/search`, `domain/graph`).
- [ ] 6.3 `task lint` for the touched modules.
- [ ] 6.4 Deferred (documented): gateway collection picker reuse across agent settings and search surfaces.
