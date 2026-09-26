# fix-fts-multiterm-query

## Why

Issue #996 (ranked cause #3, the keyword/FTS leg only). The graph-object keyword
search (`GET /api/graph/objects/fts` → `graph.FTSSearch`) builds its predicate with
`websearch_to_tsquery`, which ANDs every non-stopword term. A natural multi-term query —
e.g. the issue's `aksjeloven innbetaling av aksjekapitalen innskudd penger andre formuesgoder`
— is satisfiable only if a single object contains *every* stemmed term together. In the
Norwegian Law corpus no single object does, so the lexical leg returns **0 rows** and cannot
correct the vector leg, which surfaces the wrong statute. The same query under OR semantics
matches ~99 objects.

The existing escape hatch (`ftsquery.Relax`) only strips numeric/hyphenated identifier terms;
it never relaxes AND→OR, so a pure-text multi-term query stays at zero.

## What Changes

- New `ftsquery.Disjoin(query)` helper: joins the surviving letter-bearing terms with `|`
  for a caller to feed to `to_tsquery` (OR semantics). It declines when fewer than two terms
  survive, or when the query carries `websearch_to_tsquery` operator syntax (phrase,
  negation, explicit OR) that an OR-disjunction would strip or invert. `ftsquery.Relax` is
  refactored to share the same `terms()` tokeniser, so the two fallbacks cannot drift.
- `graph.FTSSearch` and `search.LexicalSearch` (the chunks path) both fall back in the same
  order: strict (AND) → relaxed (identifier strip) → disjoined (OR), each only after the
  previous pass returned zero rows and only on the first page. The disjoined pass ranks by
  `ts_rank_cd`, which still rewards objects covering more of the query's terms, so recall is
  restored without collapsing ranking to "any one term". Single-term precision is untouched:
  a single-term query already satisfies AND, so the OR fallback never engages for it.

### Shared builder rationale

The FTS query construction was duplicated across `domain/graph/repository.go` and
`domain/search/repository.go`. `ftsquery` is the shared builder both call sites use, so the
two cannot drift — the same principle as the shared authorization helper in #1040 (tracked
by #1041). Both call sites use `ftsquery.Relax` **and** `ftsquery.Disjoin`; the disjoin
construction is exercised by unit tests in `pkg/ftsquery` and end-to-end by the graph
domain's DB-backed regression test.

## Capabilities

### Modified Capabilities

- `graph-full-text-index`: the graph-object keyword search now falls back to an OR
  disjunction (ranked by term coverage) after the strict and relaxed passes match nothing.

## Impact

- `apps/server/pkg/ftsquery/ftsquery.go` — add `Disjoin`, extract shared `terms()`.
- `apps/server/pkg/ftsquery/ftsquery_test.go` — `TestDisjoin` unit coverage.
- `apps/server/domain/graph/repository.go` — strict→relax→disjoin fallback in `FTSSearch`.
- `apps/server/domain/graph/fts_disjoin_db_test.go` — regression test (recall, coverage
  ordering, single-term precision, first-page gating).
- `apps/server/domain/search/repository.go` — same strict→relax→disjoin fallback in
  `lexicalSearch` (chunks path).
- No migration, API shape, response shape, filter or limit change.

## Out of scope (reported, not fixed — the other ranked causes of #996)

1. Relationship leg dominating weighted fusion + `injectRelationshipNodes` (ranked #1).
2. Relationship embeddings near-uniform (`has_paragraph` triplets) (ranked #2).
3. Whole-object embedding with 0 documents/0 chunks (ranked #4).
4. `gemini-embedding-001` not legal-tuned (ranked #5).
5. `namespace` NULL on all objects making namespace filtering a silent no-op (ranked #6).
