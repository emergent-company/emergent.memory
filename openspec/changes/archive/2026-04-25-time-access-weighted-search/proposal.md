## Why

The hybrid search API fuses FTS and vector scores but ignores two highly relevant signals for AI agent memory: recency (recent knowledge is more likely correct) and access frequency (frequently-referenced facts are more salient). Applications must re-rank client-side, which breaks pagination and wastes bandwidth.

## What Changes

- Extend `HybridSearchRequest` with optional `recency_boost`, `recency_half_life`, and `access_boost` parameters
- Server-side scoring formula incorporates recency (from `created_at`) and access frequency (from existing analytics: `access_count`, `days_since_access`)
- All new params are opt-in with default 0 — fully backward compatible
- CLI `memory query` exposes `--recency-boost`, `--recency-half-life`, `--access-boost` flags

## Capabilities

### New Capabilities

- `search-ranking-signals`: Recency and access-frequency boost parameters on hybrid search with configurable decay

### Modified Capabilities

- (none — purely additive to existing search capability)

## Impact

- `apps/server/domain/search/` — scoring logic in hybrid search handler/service
- `apps/server/domain/graph/` — search endpoint request struct
- `tools/cli/` — new flags on `memory query`
- No breaking changes — all new params are optional with zero defaults
