## ADDED Requirements

### Requirement: Reranking is config-gated and default-off

Reranking SHALL be off by default. It SHALL only activate when a rerank model is configured (`RerankModel`) and, when applicable, a top-N bound (`RerankTopN`). An unconfigured or empty `RerankModel` SHALL mean "no rerank", producing behaviour identical to a build without the rerank stage.

#### Scenario: Unconfigured defaults to no rerank

- **WHEN** `RerankModel` is unset or empty
- **THEN** no rerank call is made and the fused order is returned unchanged

#### Scenario: Configured model activates rerank

- **WHEN** `RerankModel` is set to a valid model
- **THEN** the top-N fused candidates SHALL be passed to the reranker

### Requirement: Config validation rejects invalid rerank settings

The rerank configuration SHALL be validated at server startup: an unknown rerank provider/model, or an out-of-range `RerankTopN` (`<= 0` or `> 100`), SHALL be rejected with a clear error rather than silently failing or unboundedly reranking.

#### Scenario: Unknown model rejected

- **WHEN** `RerankModel` references a model the system cannot resolve
- **THEN** configuration validation SHALL fail with an actionable error

#### Scenario: Out-of-range top-N rejected

- **WHEN** `RerankTopN` is `<= 0` or greater than `100`
- **THEN** configuration validation SHALL fail with an actionable error

### Requirement: Rerank is bounded to the top-N fused candidates

The reranker SHALL only re-score the top-N fused candidates (N configurable, default exactly 50, and SHALL NOT exceed 100), never the full candidate set, to bound cost and latency. When the fused result set is smaller than top-N, the entire set SHALL be reranked.

#### Scenario: Only top-N candidates are sent

- **WHEN** a fused result set has more than `RerankTopN` candidates
- **THEN** only `RerankTopN` candidates SHALL be sent to the reranker

#### Scenario: Fewer candidates than top-N reranks all

- **WHEN** the fused result set has fewer than `RerankTopN` candidates
- **THEN** all candidates SHALL be reranked

### Requirement: Graceful fallback on provider error

If the rerank provider call fails, times out, or returns malformed output, the search SHALL NOT fail: it SHALL fall back to the pre-rerank fused order, SHALL record the fallback on the trace, and SHALL return the results to the caller.

#### Scenario: Provider error falls back to fused order

- **WHEN** the reranker errors or times out
- **THEN** the search returns the fused order unchanged and records a fallback flag

#### Scenario: Search never fails due to rerank

- **WHEN** the reranker is unreachable
- **THEN** the search SHALL still complete successfully with the fused results

### Requirement: Reranker is provider-agnostic with multiple adapters

Reranking SHALL be expressed through a provider-agnostic `Reranker` interface, with at least an LLM-as-reranker adapter and a Cohere/cross-encoder adapter. Adding a new adapter SHALL not require changes to the search pipeline.

#### Scenario: Adapters implement one interface

- **WHEN** a new rerank adapter is added
- **THEN** it implements the same `Reranker` interface and is selectable by configuration without pipeline changes

#### Scenario: Credentials resolve through provider

- **WHEN** the reranker needs credentials
- **THEN** credentials SHALL resolve through `domain/provider` credential resolution and model selection through `domain/modelconfig`

### Requirement: Rerank metadata is recorded on the trace

When reranking runs, the retrieval trace SHALL record rerank metadata: the model used, the top-N bound, the pre- and post-rerank scores/order, and the fallback flag, so a reranked result packet can be reconstructed and its quality later evaluated.

#### Scenario: Trace records rerank metadata

- **WHEN** a reranked search completes
- **THEN** the persisted trace includes the rerank model, top-N, reranked order/scores, and fallback flag
