## Why

`domain/search` fuses five ways (RRF, weighted, interleave, graph-first, text-first —
`service.go` `fuseResults`/`fuse*`) but has **no rerank stage**. Fusion only reorders
the candidate scores already produced by each leg; it cannot re-read the query against
each candidate with a strong model and re-order by relevance. That is the single
highest-leverage retrieval-quality gap after fusion, and Onyx's pipeline applies exactly
this post-fusion cross-encoder/LLM rerank. We want a pluggable, config-gated reranker
that is **off by default** and bounds its cost/latency to the top-N fused candidates.

## What Changes

- Add a post-fusion `Reranker` stage between `filterBelowMinScore` and result assembly
  in `domain/search/service.go`, gated by config (`RerankModel`, `RerankTopN`), default
  **OFF** (behaviour identical to today when unconfigured).
- Reuse `domain/provider` credential resolution and `domain/modelconfig` for model
  selection; add a new `pkg/rerank` with a provider-agnostic `Reranker` interface and at
  least two adapters: an LLM-as-reranker and a Cohere/cross-encoder adapter path.
- Rerank **only** the top-N fused candidates (N≈20-50) to bound cost and latency; use a
  lexical fallback (keep existing fused order) when unconfigured or on provider error.
- Record rerank metadata on the retrieval trace so reranked order and scores are
  reconstructable.

## Capabilities

### New Capabilities

- `retrieval-reranking`: a config-gated, pluggable post-fusion rerank stage over the
  top-N fused candidates, defaulting to off, with bounded cost/latency and graceful
  fallback.

### Modified Capabilities

- `search`: the fused-result pipeline gains an optional rerank stage before assembly
  (delta in `specs/search/spec.md`).

### Related / Consumed Capabilities

- `retrieval-trace-persistence`: the trace records rerank metadata (model, top-N, scores,
  fallback flag). This is described only in the `retrieval-reranking` delta; no delta is
  written for `retrieval-trace-persistence`.

## Impact

- **Server** (`apps/server/`): new `pkg/rerank` (interface + LLM adapter + Cohere/cross-
  encoder adapter); `domain/search/service.go` (stage placement + config read);
  `domain/search/dto.go` (config fields); `domain/search/trace_store.go` (rerank
  metadata); `domain/modelconfig` + `domain/provider` (reuse).
- **Config** (`domain/search/service.go` env/config): `RERANK_MODEL`, `RERANK_TOP_N`,
  and/or modelconfig-driven `RerankModel`.
- **Tracing**: rerank latency and model recorded on the search span and trace.

## Dependency

Benefits from `add-answer-feedback` for evaluation (rerank quality can only be measured
against an answer-quality signal), but is not blocked by it — reranking ships with
deterministic unit tests against a canned/fake reranker.
