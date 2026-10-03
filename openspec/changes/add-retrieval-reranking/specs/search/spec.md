## ADDED Requirements

### Requirement: Post-fusion rerank stage

The search pipeline SHALL support an optional post-fusion rerank stage that runs between min-score filtering and result assembly when configured, re-scoring and re-ordering only the top-N fused candidates. When configured, every upstream leg SHALL fetch up to `max(limit, RerankTopN)` candidates so the top-N block is actually available before fusion, and the final result SHALL be truncated to the caller's `limit`. When not configured, the fused order SHALL be returned unchanged.

#### Scenario: Rerank disabled preserves order

- **WHEN** no reranker is configured
- **THEN** the final result order SHALL be exactly the post-fusion, min-score-filtered order

#### Scenario: Rerank re-orders the top candidates

- **WHEN** a reranker is configured
- **THEN** the top-N fused candidates SHALL be re-scored by the reranker and re-ordered by their rerank scores, and results beyond top-N SHALL retain their fused order

#### Scenario: Candidate width preserves the top-N block

- **WHEN** a reranker is configured and the caller's response `limit` is smaller than `RerankTopN`
- **THEN** every upstream leg SHALL fetch up to `max(limit, RerankTopN)` candidates (not the caller's `limit`), fusion SHALL run over that wider set so the reranker receives its full top-N block, and the final result SHALL be truncated to the caller's `limit`
