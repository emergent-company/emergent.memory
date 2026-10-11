## Why

Hybrid search fuses the lexical and vector channels with a weighted sum. When one channel returns no candidates, its configured weight (default 0.5) is silently discarded — the surviving channel's scores are scaled down by only its own weight. Because `recency_boost` and `access_boost` are *added* afterwards, that scaling perturbs final ranking. A surviving channel should not be penalised by the empty channel's share of the weight.

## What Changes

- Add a pure helper `EffectiveFusionWeights` in `apps/server/pkg/mathutil` that, when exactly one channel produced no scores, assigns effective weight `1.0` to the surviving channel and `0.0` to the empty channel; otherwise it returns the weights unchanged.
- Use it at both hybrid-fusion sites: `domain/graph/service.go` (`Service.HybridSearch`) and `domain/search/repository.go` (`Repository.HybridSearch`).

## Impact

- `apps/server/pkg/mathutil/math.go` (+ table-driven unit test)
- `apps/server/domain/graph/service.go`
- `apps/server/domain/search/repository.go`
- Spec: `search` capability gains a single-channel fusion requirement.

No schema, API, or migration changes.
