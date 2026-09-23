## Why

`entity-search` (MCP tool `executeSearchEntities`) matched with three leading-wildcard `ILIKE` predicates (`key`, `properties->>'name'`, `properties->>'description'`). No index can accelerate `ILIKE '%…%'`, so every call was an O(N) seq scan plus per-row JSONB extraction — profiled on dev (107,938 objects) at >40s for a broad query and ~2-3s even for a narrow `type`.

The replacement path is the GIN-indexed full-text search already used by `graph.FTSSearch`. That is **not** a behaviour-preserving optimisation: `ILIKE '%foo%'` is substring matching, while `fts @@ websearch_to_tsquery(…)` is lexeme matching against the tokenised `kb.graph_objects.fts` vector. The matching set narrows (no substring/prefix matching, Norwegian stop words produce no lexemes) and widens (`title` and `type` are indexed, so they become searchable). Those changes were previously undocumented.

## What Changes

- `entity-search` matches with a single full-text predicate (`fts @@ websearch_to_tsquery('simple', ?) OR fts @@ websearch_to_tsquery('norwegian', ?)`), mirroring `graph.FTSSearch`, plus the existing strict→relaxed retry via `ftsquery.Relax`.
- The `go.key = ?` predicate is **removed**. It was redundant — migration 00174 indexes the raw and separator-normalised key into `fts`, so a composite key and its component tokens are both resolvable by the FTS predicate — and it defeated the GIN index: no index covers `key` in this query shape, and an OR arm that cannot be served by an index forces a seq scan of the whole disjunction instead of `idx_graph_objects_fts`.
- The tool description now states what is actually searchable (key, type, title, name, description) and that matching is lexeme-based.
- The matching semantics — both the narrowing and the widening — are specified in the `mcp-entity-search` capability.
- Tests install the migration 00174 trigger on the throwaway test database (the schema snapshot still embeds the pre-00174 trigger) and cover component-token lookup, the relaxed retry, and `title` matching.

### Deliberately accepted narrowing

These are accepted, documented changes, consistent with the platform-wide `graph.FTSSearch` contract:

- **Substring and prefix matching are gone** (`%ali%` no longer matches "Alice"; mid-word substrings no longer match). A query term must match an indexed lexeme. Prefix search (`foo:*`) is a potential follow-up, not added here so the semantics stay consistent with `graph.FTSSearch`.
- **Numeric substrings are gone** (`%1997%` matched `Paragraf 1997` and `Paragraf 11997`; now only the lexeme `1997`).
- **Norwegian stop-word-only queries return nothing** (`og` previously matched every row; now the tsquery has no lexemes). Requiring a non-stop-word term yields a useful result set instead of a full-table match.
- **Whitespace-only queries return nothing** (previously `%  %` matched every row).

## Capabilities

### New Capabilities
- `mcp-entity-search`: the matching semantics of the `entity-search` MCP tool, now explicitly full-text (lexeme) based, including the accepted narrowing and the newly searchable `title`/`type` fields.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/server/domain/mcp/service.go` — full-text predicate replaces the `ILIKE` OR and the redundant `key = ?` arm; tool description updated.
- `apps/server/domain/mcp/entity_search_test.go` — tests install the 00174 trigger and cover component-token lookup, relaxed retry, and `title` matching.
- No migration, API shape, response shape, filter, ordering or limit change.
- Known out-of-scope staleness: `apps/server/internal/testdb/schema.sql` still embeds the pre-00174 trigger, so other suites that rely on the fixture's `fts` vector do not exercise production tokenisation. Reported, not regenerated here.
