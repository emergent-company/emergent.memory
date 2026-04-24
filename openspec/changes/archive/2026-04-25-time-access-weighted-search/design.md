## Context

`HybridSearchRequest` (apps/server/domain/graph/dto.go:424) currently supports `LexicalWeight` and `VectorWeight` for fusing FTS and vector scores. The scoring formula is a weighted sum. There are no recency or access signals.

Access analytics exist: `UnusedObjects` analytics already track `LastAccessedAt`, `AccessCount`, `DaysSinceAccess` on graph objects. `CreatedAt` is on every object. Both signals are available server-side at query time without new DB columns.

## Goals / Non-Goals

**Goals:**
- Add `recency_boost`, `recency_half_life`, `access_boost` to `HybridSearchRequest`
- Compute recency and access scores server-side at search time
- All params default to 0 — zero behavior change for existing callers
- CLI `memory query` exposes the new flags

**Non-Goals:**
- Changing the default ranking for existing queries
- Per-object-type decay curves
- Learning/adaptive ranking (future)
- `UpdatedAt` recency (use `CreatedAt` only for now — simpler, more predictable)

## Decisions

**1. Scoring injected into hybrid search SQL, not post-processing in Go**

Options: (a) fetch results from DB, re-score in Go, (b) include boost signals in the SQL ORDER BY.

Chose (b). Post-processing in Go breaks pagination — top-10 after re-rank differs from top-10 raw. SQL approach: join objects table (for `created_at`) and analytics table (for `access_count`, `days_since_access`), compute boost as expression in SELECT, add to score.

**2. Sigmoid for recency, normalized linear for access**

Recency: `1 / (1 + exp((hours_old - half_life) / (half_life / 4.0)))` — sigmoid centered at `half_life`. Default half_life: 168h (7 days). Objects newer than half_life score > 0.5; older score < 0.5. Smooth decay, no cliff.

Access: `LEAST(access_count / 100.0, 1.0) * 0.5 + GREATEST(0, 1 - days_since_access / 365.0) * 0.5`. Blends count and recency-of-access. Cap at 100 accesses to avoid domination by outliers.

Alternative: pure linear recency. Rejected — creates hard cliff at cutoff date.

**3. Analytics join is LEFT JOIN**

Objects without analytics rows (never accessed) get `access_score = 0`. Access boost only rewards objects that have been accessed.

## Risks / Trade-offs

- **[SQL complexity]** → The scoring expression increases query complexity. Mitigate: only add JOIN and expression when `recency_boost > 0` or `access_boost > 0`.
- **[half_life tuning]** → 7-day default may be wrong for long-horizon memory use cases. Mitigate: fully configurable per-request; document recommended values.
- **[analytics coverage]** → Objects never accessed have no analytics row → access_score = 0. This is correct behavior, not a bug.
