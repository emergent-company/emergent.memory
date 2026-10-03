## ADDED Requirements

### Requirement: Post-fusion rerank stage

The search pipeline SHALL support an optional post-fusion rerank stage that runs between min-score filtering and result assembly when configured, re-scoring and re-ordering only the top-N fused candidates. When not configured, the fused order SHALL be returned unchanged.

#### Scenario: Rerank disabled preserves order

- **WHEN** no reranker is configured
- **THEN** the final result order SHALL be exactly the post-fusion, min-score-filtered order

#### Scenario: Rerank re-orders the top candidates

- **WHEN** a reranker is configured
- **THEN** the top-N fused candidates SHALL be re-scored by the reranker and re-ordered by their rerank scores, and results beyond top-N SHALL retain their fused order

#### Scenario: Fuse width preserves the top-N block

- **WHEN** a reranker is configured and the caller's response `limit` is smaller than `RerankTopN`
- **THEN** fusion SHALL run over `max(limit, RerankTopN)` candidates so the reranker receives its full top-N block, and the final result SHALL be truncated to the caller's `limit`
