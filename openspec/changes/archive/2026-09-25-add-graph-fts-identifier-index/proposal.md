## Why

`kb.graph_objects.fts` cannot represent this platform's identifiers and silently kills
phrase search (issue #706). Two independent defects, both verified against dev:

1. **Composite keys index as one opaque lexeme.** The default parser treats `x/y-z` as a
   file/host token, so `to_tsvector('simple', 'lov/1997-06-13-44')` is the single lexeme
   `'lov/1997-06-13-44'`. The primary identifier scheme (`lov/1997-06-13-44`,
   `forskrift/2007-06-29-876#kapittel-1-paragraf-2`) is therefore unreachable in component
   form: no query containing `lov 1997 06 13 44` can match it.
2. **Positions saturate at MAXENTRYPOS.** `kb.update_graph_objects_fts` (migration 00016)
   concatenated every `properties` value unbounded, so large objects exhausted the
   tsvector position budget (16383) and later lexemes collapsed onto the same position.
   `websearch_to_tsquery` emits `<->` for any hyphenated run, so `a <-> b` could never be
   satisfied on such rows — phrase search was structurally dead, not just for one query.

#704 added a query-side workaround (`ftsquery.Relax`: retry without numeric terms when the
strict query returns nothing). That mitigates the symptom but leaves the index shape wrong;
the retry still cannot return a document when the numeric identifier is the only
discriminator.

## What Changes

- Migration `00174_graph_objects_fts_identifiers.sql` replaces the trigger function with a
  builder that indexes:
  - the **raw key** (exact identifier lookups), plus a **separator-normalised key**
    (`/`, `#` → space and a signed `-` → `" -"` variant) so component tokens exist and
    `websearch_to_tsquery`'s phrase over `1997-06-13-44` is satisfiable;
  - the object **type**;
  - a **bounded whitelist** of prose fields (`properties->>'title' | 'name' |
    'description'`, each capped at 4000 chars) using the **`norwegian`** configuration,
    instead of the whole `properties` blob.
- Identifier fields keep `simple` semantics; prose uses `norwegian`; the two weighted
  tsvectors are concatenated (A/B identifiers, C prose).
- The backfill (~107k rows) and GIN index rebuild run in the migration; the rebuild uses
  `CREATE INDEX CONCURRENTLY` (`-- +goose NO TRANSACTION`).
- `graph.Repository.FTSSearch` matches **both** configurations
  (`fts @@ websearch_to_tsquery('simple', q) OR fts @@ websearch_to_tsquery('norwegian', q)`)
  and ranks with the greater of the two `ts_rank_cd` values. Lexemes carry no
  configuration, so a single-configuration `@@` would only see half the index.
- The #704 `ftsquery.Relax` fallback is **kept** as a safety net for tokens the index still
  cannot represent; it is no longer the path that makes the composite-key case work.

## Capabilities

### New Capabilities

- `graph-full-text-index`: how `kb.graph_objects.fts` is built (identifier normalisation,
  bounded prose whitelist, dual `simple`/`norwegian` configurations) and how the
  graph-object full-text search matches against it.

### Modified Capabilities

<!-- None: no existing spec covers the graph-object FTS index shape. -->

## Impact

- **Migration** `apps/server/migrations/00174_graph_objects_fts_identifiers.sql` (new):
  helper `kb.graph_object_fts`, redefined trigger `kb.update_graph_objects_fts`, table
  rewrite for the backfill, CONCURRENT GIN rebuild; reversible Down.
- **Server** `apps/server/domain/graph/repository.go` (`FTSSearch`/`ftsSearch`): dual
  configuration match and rank.
- **Tests** `apps/server/domain/graph/fts_identifier_index_db_test.go` (new): composite-key
  reachability on the strict pass (no fallback), and position bounding.
- **Behaviour change**: non-whitelisted `properties` values are no longer searchable
  lexically. This is the intended bound; identifier + prose recall improves.
- **Operational**: a one-time full-table UPDATE and GIN rebuild on a ~107k-row table.
