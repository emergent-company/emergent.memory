## 1. Diagnose the per-call cost (issue #1191)

- [x] 1.1 Reproduce on a hermetic throwaway Postgres seeded like the issue (120k `LegalParagraph` rows): time `entity-query` with `key_prefix` and `field_strategy="full"`.
- [x] 1.2 `EXPLAIN (ANALYZE, BUFFERS)` the current predicate: `starts_with(go.key, ?)` plans a Parallel Seq Scan of the whole `kb.graph_objects` heap (cost ~25–42k); the cost is the constant scan, not per-row JSONB projection (affected `content` averaged 674 bytes, not 14 KB).
- [x] 1.3 Confirm the plan-identical `key COLLATE "C" >= ? AND key COLLATE "C" < ?` range plans an Index Scan (cost 8.45, execution 0.06 ms) and returns exactly the `starts_with` row set.

## 2. Index the key-prefix range

- [x] 2.1 Add `keyPrefixRangeClause` / `prefixUpperBound`; replace the `starts_with` clause in `executeQueryEntities` (SELECT and COUNT share the clause).
- [x] 2.2 Add migration `00198_add_graph_objects_project_type_key_c.sql`: partial `idx_graph_objects_project_type_key_c (project_id, type, key COLLATE "C")` on head-main rows, built CONCURRENTLY with an `ANALYZE` afterwards.
- [x] 2.3 Prove with `EXPLAIN (ANALYZE, BUFFERS)` that the new path uses `Index Scan using idx_graph_objects_project_type_key_c` and contains no `Seq Scan`.
- [x] 2.4 Keep the bound valid UTF-8: increment the last rune to the next code point (skipping surrogates) instead of editing raw bytes, so non-ASCII prefixes ending in `0x7F`/`0xBF` cannot trip SQLSTATE 22021 or mis-match.

## 3. Honest deadline handling

- [x] 3.1 Stop discarding the relationship-enrichment error in `executeQueryEntities`: on deadline return the explicit timeout error; otherwise wrap and return the error. This makes behaviour match the existing `entity-query calls are bounded` requirement.
- [x] 3.2 Keep the `MCP_ENTITY_QUERY_TIMEOUT` default (30 s); with the scan removed the full-strategy cap (25 rows) completes in milliseconds, and timeout expiry already returns an explicit error rather than silently truncating.

## 4. Schema/behaviour agreement (#1188)

- [x] 4.1 State the effective full-strategy cap in the `entity-query` `limit` property description (interpolating the configured cap), so the advertised `Maximum: 200` no longer contradicts the runtime clamp.

## 5. Tests

- [x] 5.1 `TestKeyPrefixRangeClause` — rune-increment bound, punctuation suffix, DEL/`0xBF`/U+00FF/multibyte, carry past a trailing max rune, no-successor fallback; asserts the bound is valid UTF-8.
- [x] 5.2 `TestExecuteQueryEntities_KeyPrefixUsesBytewiseIndex` — seeds 50k rows, asserts the plan uses the new index and contains no seq scan, and that the range returns exactly the `starts_with` rows.
- [x] 5.3 `TestEntityQueryLimitSchemaStatesFullCap` — the default and a configured cap both appear in the `limit` schema description.
- [x] 5.4 `TestExecuteQueryEntities_KeyPrefixNonASCIIUpperBound` — adversarial non-ASCII prefixes return exactly the `starts_with` set without error.
- [x] 5.5 `TestKeyPrefixLegacyBoundRejectedAsInvalidUTF8` — pins the old byte-increment bound as invalid UTF-8 (SQLSTATE 22021 on a simple-protocol client) and the new bound as accepted.
- [x] 5.6 `TestExecuteQueryEntities_RelationshipEnrichmentErrorIsSurfaced` — the fail-loud enrichment path returns the wrapped error instead of `ok:true`.

## 6. Out of scope (reported, not fixed)

- [ ] 6.1 The `search-knowledge` timeout-masking defect (#1187) lives in `query_tools.go`, not entity-query; unchanged here.
