## 1. Shared OR-disjunction builder

- [x] 1.1 Add `ftsquery.Disjoin(query)` returning an `|`-joined disjunction and an `ok` flag that declines on operator syntax and fewer-than-two terms.
- [x] 1.2 Extract `terms(query)` shared by `Relax` and `Disjoin` so the two fallbacks tokenise identically.
- [x] 1.3 Add `TestDisjoin` unit coverage (multi-term, single-term, operator syntax, numeric-only).

## 2. Graph keyword leg fallback

- [x] 2.1 In `graph.FTSSearch`, after the strict and relaxed passes match nothing and only on the first page, retry once with `ftsquery.Disjoin` matched via `to_tsquery` (OR) and ranked by `ts_rank_cd`.
- [x] 2.2 Keep the dual `simple`/`norwegian` predicate and the length-normalised rank for the disjoined pass.

## 3. Chunks keyword leg fallback

- [x] 3.1 In `search.lexicalSearch`, mirror the same strict→relax→disjoin order via the shared `ftsquery` helpers.

## 4. Tests

- [x] 4.1 Add `TestFTSSearchDisjoinsMultiTermQuery` (issue's exact query: strict = 0, recall restored).
- [x] 4.2 Add `TestFTSSearchDisjoinOrdersByCoverage` (higher term coverage ranks first).
- [x] 4.3 Add `TestFTSSearchSingleTermKeepsPrecision` (single-term query not widened).
- [x] 4.4 Add `TestFTSSearchDisjoinGatedToFirstPage` (fallback does not refill later pages).

## 5. Spec

- [x] 5.1 Add a `graph-full-text-index` delta requirement documenting the OR-disjunction fallback and its gating.

## 6. Out of scope (reported, not fixed)

- [ ] 6.1 Relationship-leg fusion dominance and score collapse (ranked #1/#2).
- [ ] 6.2 Whole-object embedding / 0 chunks (ranked #4).
- [ ] 6.3 `gemini-embedding-001` suitability (ranked #5).
- [ ] 6.4 `namespace` NULL on all objects (ranked #6).
