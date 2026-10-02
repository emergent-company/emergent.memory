## 1. Migration — collections tables

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_collections.sql`: `kb.collections` (id, project_id, name, description, created_at, updated_at) and `kb.collection_items` (id, collection_id, item_type ∈ {document, canonical, source}, item_id, created_at, `UNIQUE (collection_id, item_type, item_id)`), FKs with `ON DELETE CASCADE` from referenced documents/graph objects, index on `(collection_id)` and `(project_id)`.
- [ ] 1.2 (TDD) Migration round-trip test: up creates both tables with the unique constraint; down drops cleanly; cascade delete from a document removes its membership rows.

## 2. Collections domain — CRUD + membership

- [ ] 2.1 New `domain/collections` package: Bun entities, `store.go` (create/update/delete/list, add/remove item, resolve member ids), `service.go` (project-scope checks, duplicate-membership idempotency), `handler.go`, `module.go`.
- [ ] 2.2 (TDD) Store unit test: create/update/delete persist; delete removes items; duplicate add does not create a second row; remove deletes only the membership row.
- [ ] 2.3 (TDD) Service unit test: cross-project item add rejected; cross-project collection access rejected; list returns only the caller's project collections.
- [ ] 2.4 (TDD) Handler unit test: routes for collection CRUD and item add/remove validate and map to the DTOs.

## 3. Membership resolver + canonical head

- [ ] 3.1 Add `resolveCollectionItemIDs(ctx, projectID, collectionIDs)` in `domain/search` (or shared `pkg`) returning allowed `document_id`s, head-resolved `canonical_id`s, and source refs for the requested collections.
- [ ] 3.2 Resolve `canonical` items to current head via `domain/graph` head resolution.
- [ ] 3.3 (TDD) Unit test: resolver returns the union of item ids per type; a canonical item returns its head; an empty collection returns empty sets; cross-project collection ids return empty sets.

## 4. Search filtering across three legs

- [ ] 4.1 Add `CollectionIDs []uuid.UUID` to `UnifiedSearchRequest` in `domain/search/dto.go`.
- [ ] 4.2 In `domain/search/repository.go`, apply the collection filter to the text/chunk leg (`kb.documents`), the graph leg (graph object tables), and the relationship leg (`kb.graph_relationships`), each via the resolver's allowed id set. The relationship leg SHALL be restricted to relationships whose `src` OR `dst` head-resolved canonical id is in the collection's canonical set.
- [ ] 4.3 (TDD) Unit test: a search with a collection returns only members on each leg; multiple collections AND-combine; a cross-project collection returns nothing; an empty collection returns nothing.
- [ ] 4.4 (TDD) Unit test: no collection ids → behaviour identical to today (project-id filter only).

## 5. Reuse in agent scoping and chat retrieval

- [ ] 5.1 `domain/agents`: knowledge scoping accepts collection ids and resolves them through the same resolver.
- [ ] 5.2 `domain/chat`: retrieval request accepts collection ids and resolves them through the same resolver.
- [ ] 5.3 (TDD) Unit test: agent scoped to a collection retrieves only members; chat retrieval scoped to a collection retrieves only members; both surfaces use the resolver (no divergent membership logic).

## 6. Verify

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `task lint` for the touched modules.
- [ ] 6.3 Deferred (documented): gateway collection picker reuse across agent settings and search surfaces.
