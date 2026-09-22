## Context

`kb.graph_objects.fts` is a plain `tsvector` column populated by a `BEFORE INSERT OR UPDATE`
trigger. It is the lexical leg of graph search (FTS endpoint, hybrid search, search-with-
neighbors, retrieval). Chunk search uses a different column (`kb.chunks.tsv`) and is out of
scope.

## Decisions

### D1 — Keep `fts` as a single `tsvector` column, concatenate two configurations

`fts` stays one column. Identifier fields are built with `simple`; prose with `norwegian`;
the weighted vectors are concatenated. Query-time matches OR the two configurations and
ranks with `GREATEST(ts_rank_cd(simple), ts_rank_cd(norwegian))`. A separate column per
configuration was rejected: it would double the index and force every call site to change.

**Measured on scratch Postgres 17:** a row with key `lov/1997-06-13-44` and prose
`Lov om aksjeselskaper (aksjeloven)`:

| query | old shape (`simple` only) | new shape (dual) |
|---|---|---|
| `aksjeloven lov 1997-06-13-44` | false | true |
| `lov 1997 06 13 44` | false | true |
| `lov/1997-06-13-44` | false* | true |
| `aksjeselskaper` | true | true |

\* a query containing only the hyphenated identifier is rewritten by `websearch_to_tsquery`
into `'lov/1997-06-13-44'` (matches the raw lexeme) — component queries were the broken ones.

`websearch_to_tsquery('norwegian','aksjeloven lov 1997-06-13-44')` =
`'aksj' & 'lov' & '1997' <-> '-06' <-> '-13' <-> '-44'`; only the `norwegian` leg supplies
`'aksj'`, which is why matching one configuration alone is insufficient.

### D2 — Separator-normalised key, including a signed-hyphen variant

`/`, `#` → space supplies component tokens (`lov 1997 06 13 44`). Because
`websearch_to_tsquery` turns `1997-06-13-44` into the phrase
`'1997' <-> '-06' <-> '-13' <-> '-44'`, a second normalisation maps `-` → `" -"` so the
signed numeric lexemes exist in the same order (`'lov':1 '1997':2 '-06':3 '-13':4 '-44':5`).
Without the signed variant the headline query still fails. Both variants are weighted `A`,
alongside the raw key.

### D3 — Bound the indexed input

Only `key`, `type`, `properties->>'title'`, `properties->>'name'` and
`properties->>'description'` enter `fts`; each prose field is `left(..., 4000)`. A 4000-char
field is at most ~2000 tokens, so the three fields plus key/type stay well below
MAXENTRYPOS (16383). Rejected: indexing the whole `properties` blob (current behaviour,
which saturates) and indexing arbitrary nested fields (unbounded, unpredictable).

**Measured:** a synthetic object whose description held 40000 distinct tokens produced a
617 918-char tsvector containing position `16383` under the old expression, and a 9 776-char
vector with no saturated position under the new one. The migration's own whitelist removed
the blob from the vector entirely.

### D4 — `norwegian` for prose

`to_tsvector('norwegian','aksjeselskap aksjeselskaper')` collapses both to `'aksjeselskap'`,
fixing the `aksjeselskap`/`aksjeselskaper` split noted in the issue. Identifier fields stay
`simple` so numbers and punctuation components survive verbatim. Over-stemming is possible
(`aksjeloven` → `aksj`), which is why the `simple` identifier space is kept and the query
matches both.

### D5 — Migration safety

`-- +goose NO TRANSACTION` (required for `CREATE INDEX CONCURRENTLY`). The GIN index is
dropped before the backfill (so the rewrite does not maintain GIN entries row by row) and
rebuilt CONCURRENTLY after. The helper `kb.graph_object_fts` is immutable and shared by the
trigger and the backfill so they cannot drift. The trigger keeps the migration 00032
source-field guard (skip recomputation on UPDATE when key/type/properties are unchanged), so
status/label/embedding updates do not reparse the JSON or rebuild the vector. The backfill's
`WHERE fts IS DISTINCT FROM ...` makes re-runs idempotent.

**Lock impact:** replacing functions takes no table lock; the CONCURRENT index DDL takes only
`SHARE UPDATE EXCLUSIVE`; the backfill is one `UPDATE` (`RowExclusiveLock`) that does not
block reads or unrelated writes. For a much larger table the `UPDATE` can be re-run in
`ctid` batches by hand.

**Rollback:** Down restores the exact 00032 function and expression (including the
source-field guard), re-backfills, rebuilds the index CONCURRENTLY, and drops the helper
last. Verified forward and backward with goose on a scratch database.

### D6 — Keep the #704 fallback

`ftsquery.Relax` remains in `FTSSearch`. With the new index the strict dual query satisfies
the composite-key case directly, so the fallback is no longer the primary recovery path, but
it still helps for tokens the index cannot represent. Removing it would be a separate,
riskier change; it is documented here rather than silently left in place.
