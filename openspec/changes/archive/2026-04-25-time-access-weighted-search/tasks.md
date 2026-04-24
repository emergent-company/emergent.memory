## 1. DTO Extension

- [x] 1.1 Add `RecencyBoost *float32`, `RecencyHalfLife *float32`, `AccessBoost *float32` fields to `HybridSearchRequest` in `domain/graph/dto.go`
- [x] 1.2 Add default values: RecencyHalfLife defaults to 168 when RecencyBoost > 0 and RecencyHalfLife is nil

## 2. Search Scoring Logic

- [x] 2.1 Extend hybrid search SQL in `domain/graph/repository.go` to conditionally LEFT JOIN analytics table when AccessBoost > 0
- [x] 2.2 Add recency score expression to SELECT when RecencyBoost > 0: `1.0 / (1.0 + exp((extract(epoch from now() - created_at)/3600 - half_life) / (half_life / 4.0)))`
- [x] 2.3 Add access score expression to SELECT when AccessBoost > 0: `LEAST(access_count / 100.0, 1.0) * 0.5 + GREATEST(0, 1 - days_since_access / 365.0) * 0.5`
- [x] 2.4 Add boost terms to final score in ORDER BY: `score + (recency_boost * recency_score) + (access_boost * access_score)`
- [x] 2.5 Ensure boosts only applied when > 0 (no JOIN or expression overhead on standard queries)

## 3. CLI

- [x] 3.1 Add `--recency-boost`, `--recency-half-life`, `--access-boost` flags to `memory query` command
- [x] 3.2 Pass flags through to HybridSearchRequest

## 4. Tests

- [x] 4.1 Unit test: recency score formula produces ~0.5 at half_life hours, >0.5 for newer, <0.5 for older
- [x] 4.2 Unit test: access score formula with known inputs
- [x] 4.3 Integration test: search with recency_boost=1.0 returns newer object above older equally-relevant object
- [x] 4.4 Integration test: recency_boost=0 produces identical results to baseline (no boost)
