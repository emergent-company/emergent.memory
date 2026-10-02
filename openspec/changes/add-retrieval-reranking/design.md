## Context

Fusion (`domain/search/service.go` `fuse` → `fuseResults` → `fuseWeighted`/`fuseRRF`/
`fuseInterleave`/`fuseGraphFirst`/`fuseTextFirst`) only combines the scores each leg
already produced. It never re-reads the query against a candidate with a strong model.
Onyx applies a post-fusion rerank (cross-encoder / LLM) precisely to close that gap.
This change inserts a rerank stage into `Search` at a single, well-defined point.

## Goals / Non-Goals

**Goals**

- Pluggable, provider-agnostic reranker with an LLM adapter and a Cohere/cross-encoder
  adapter.
- Default OFF; config-gated via `RerankModel` + `RerankTopN`.
- Bounded cost/latency (top-N only) and graceful fallback.
- Trace records rerank metadata for later evaluation.

**Non-Goals**

- Tuning/changing the fusion strategies themselves.
- A streaming or chunk-level rerank (that is a different, costlier stage).
- Any training/fine-tuning of a rerank model.

## Decisions

### D1 — Stage placement: after `filterBelowMinScore`, before assembly

In `service.go` `Search`, the pipeline is currently:

```
fuse(...) → filterBelowMinScore(...) → countTypes → makeMetadata → buildDebugInfo → response
```

The rerank stage slots between `filterBelowMinScore` and `countTypes`/assembly. This is
the correct point because (a) the min-score filter has already discarded clearly-bad
candidates, so rerank only pays for survivors; (b) the output is still a single ordered
`[]UnifiedSearchResultItem` list, so the reranker sees the same shape the caller will.
Reranking before `filterBelowMinScore` would waste provider calls on candidates that
would be dropped; reranking after assembly would require tearing apart the response.

### D2 — Interface: `pkg/rerank`

```go
// pkg/rerank
type Candidate struct { ID string; Text string; Score float32 }
type Reranker interface {
    // Rerank re-scores the given candidates against the query and returns them
    // re-ordered (with updated scores) or an error.
    Rerank(ctx context.Context, query string, candidates []Candidate) ([]Candidate, error)
}
```

Adapters: `llmReranker` (sends query + candidate texts to a chat/LLM model, parses a
ranked list) and `cohereReranker` (Cohere `rerank` / cross-encoder API). Both resolve
their model/credentials through `domain/modelconfig` + `domain/provider`. The search
service holds a `Reranker` (nil when unconfigured) and calls it only when non-nil.

`Candidate.Text` is derived per result type at the stage boundary:

- **graph** → a serialization of the object's `Key` and `Fields` (the same surface the
  caller sees);
- **text** → `Snippet`;
- **relationship** → `TripletText` (the relationship's subject/predicate/object rendering).

### D3 — Cost/latency bound: top-N only

The reranker receives at most `RerankTopN` (default **exactly 50**, and **SHALL NOT
exceed 100**) fused candidates, truncated after the min-score filter. Candidates beyond
top-N are not re-scored; their relative order is preserved and they are appended after
the reranked block. This bounds provider cost to a constant per search regardless of
candidate-set size, which is what makes rerank viable in production. Validation rejects
`RerankTopN <= 0` or `> 100` at startup.

### D3b — One canonical config source, resolved per project

`RerankModel` and `RerankTopN` SHALL be resolved from **one** canonical source:
`domain/modelconfig` (`ProjectModelConfig.rerank_model`), resolved **per project** via
the existing modelconfig resolution chain using the request's `projectID` — exactly like
generative/embedding model selection, not a single global setting. Environment variables
(`RERANK_MODEL`, `RERANK_TOP_N`) exist only as a bootstrap fallback to seed a modelconfig
entry when none is present. Field/flag casing SHALL be `RerankModel` / `RerankTopN`
everywhere. There is **no per-request scope** field: whether the stage runs is determined
by the resolved per-project config, and the stage either runs (when a model resolves) or
is skipped (when it does not).

### D4 — Fallback is lexical/no-op

On any provider error, timeout, or malformed response, the stage returns the pre-rerank
fused order and sets a `fallback` flag on the trace. Rerank can never fail a search. When
unconfigured, the stage is skipped entirely (the interface value is nil) so the hot path
is byte-for-byte today's behaviour.

### D5 — Trace integration

`RetrievalTrace` gains a `rerank` JSONB field (or a `rerank_metadata` jsonb column)
recording `{model, topN, fallback, scores}`. This reuses the existing trace write in
`persistTraceAsync` rather than a new store, and keeps the exact-packet-reproducibility
guarantee of `retrieval-trace-persistence` intact for reranked searches.

## Risks / Trade-offs

- **Provider latency on the search path.** A rerank round-trip adds latency. Mitigation:
  top-N bound (D3) + a hard timeout + fallback (D4). Default OFF means no regression for
  operators who don't opt in.
- **Cost blow-up under load.** Unbounded reranking would multiply token spend. Mitigation:
  constant top-N per search; `RerankTopN` ceiling of 100.
- **Quality of LLM-as-reranker parsing.** LLM ranked-list output can be malformed.
  Mitigation: strict parser + fallback to fused order on parse failure (D4).
- **Config drift.** One canonical config source (`domain/modelconfig`), env as bootstrap
  fallback only (D3b); a single validation path rejects invalid model / out-of-range
  top-N.
