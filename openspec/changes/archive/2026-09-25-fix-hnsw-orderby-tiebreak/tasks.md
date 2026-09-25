## 1. Graph repository — drop the id tiebreak

- [x] 1.1 `VectorSearch`: `ORDER BY distance ASC, id ASC` → `ORDER BY distance ASC`.
- [x] 1.2 `FindSimilarObjects`: `ORDER BY distance ASC, id ASC` → `ORDER BY distance ASC`.
- [x] 1.3 `FindSimilarObjectInBranch`: rewrite as overfetch subquery (`ORDER BY embedding_v2 <=> ?::vector LIMIT 64`) + outer `ORDER BY _dist ASC, id ASC LIMIT 1`; append the third `vectorStr` arg last.
- [x] 1.4 `FindSimilarRelationshipInBranch`: same overfetch + outer-sort shape over `kb.graph_relationships.embedding`; append the third `vectorStr` arg last.

## 2. Search repository — drop the id tiebreak

- [x] 2.1 Chunk `VectorSearch`: `ORDER BY c.embedding <=> ?::vector, c.id ASC` → `ORDER BY c.embedding <=> ?::vector`.
- [x] 2.2 Chunk hybrid vector search: same drop.
- [x] 2.3 `buildRelationshipSearchQuery`: `ORDER BY r.embedding <=> ?::vector, r.id ASC` → `ORDER BY r.embedding <=> ?::vector`.

## 3. OpenSpec

- [x] 3.1 Add the `retrieval-performance-config` MODIFIED requirement documenting the ORDER-BY constraint with scenarios for graph objects, relationships, and chunks.

## 4. Verification

- [x] 4.1 `go build ./...` in `apps/server`.
- [x] 4.2 `go test ./domain/graph/... ./domain/search/...` (DB-backed tests skipped if no test Postgres).
- [x] 4.3 `go vet ./domain/graph/... ./domain/search/...`.
- [x] 4.4 `gofmt -l` on the two changed files (empty).
