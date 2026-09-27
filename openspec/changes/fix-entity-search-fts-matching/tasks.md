## 1. Index-backed matching

- [x] 1.1 Remove the redundant, unindexed `go.key = ?` arm so the match predicate is the full-text OR alone (`service.go`).
- [x] 1.2 Keep the dual `simple`/`norwegian` `websearch_to_tsquery` predicate and the strict→relaxed retry.
- [x] 1.3 Confirm with `EXPLAIN (ANALYZE, BUFFERS)` on a seeded scratch DB that the predicate is served by `Bitmap Index Scan on idx_graph_objects_fts`.

## 2. Documented behaviour change

- [x] 2.1 Update the `entity-search` tool description to name the searchable fields and state that matching is lexeme-based.
- [x] 2.2 Add the `mcp-entity-search` delta spec covering indexed matching, searchable fields, composite keys, lexeme-vs-substring, relaxed retry, empty-lexeme queries and metacharacter safety.
- [x] 2.3 Record the accepted narrowing and widening in the change proposal and the PR body.

## 3. Tests

- [x] 3.1 Install the migration 00174 builder/trigger on the throwaway test database (`connectFTSearchTestDB`).
- [x] 3.2 Add `TestEntitySearchComponentKeyMatch` (component tokens against a composite key).
- [x] 3.3 Add `TestEntitySearchRelaxesUnsatisfiableIdentifier` (strict→relaxed retry).
- [x] 3.4 Add `TestEntitySearchTitleMatch` (widened `title` field).

## 4. Multi-term OR fallback (issue #996)

- [x] 4.1 After the strict and relaxed passes both match nothing, retry once with `ftsquery.Disjoin` matched via `to_tsquery` (OR) against both the `simple` and `norwegian` configurations.
- [x] 4.2 Add `TestEntitySearchDisjoinsMultiTermQuery` (issue's exact multi-term query: strict = 0, recall restored).
- [x] 4.3 Add the `mcp-entity-search` delta requirement documenting the OR-disjunction fallback.

## 5. Out of scope (reported, not fixed)

- [ ] 5.1 Regenerate `apps/server/internal/testdb/schema.sql`, which still embeds the pre-00174 fts trigger and affects other suites that rely on the fixture vector.
