## ADDED Requirements

### Requirement: Optional vendor rerank stage, default off

The search pipeline SHALL support an optional post-fusion rerank stage keyed off the provider registry. The stage SHALL be off by default: when no vendor reranker is configured, the post-fusion, min-score-filtered order SHALL be returned unchanged. When a vendor reranker is configured, only the top-N fused candidates SHALL be re-scored and re-ordered by their rerank scores, while candidates beyond top-N SHALL retain their fused order.

#### Scenario: No reranker configured

- **WHEN** no vendor reranker is configured
- **THEN** the final result order is exactly the post-fusion, min-score-filtered order

#### Scenario: Reranker configured

- **WHEN** a vendor reranker is configured with a top-N bound
- **THEN** only the top-N fused candidates are re-scored and re-ordered, and the remaining candidates keep their fused order

### Requirement: Rerank failure never fails a search

If the configured rerank provider errors, times out, or returns an unusable response, the search SHALL complete successfully using the pre-rerank fused order.

#### Scenario: Provider error falls back

- **WHEN** the rerank provider call errors or times out
- **THEN** the search returns the pre-rerank fused order and does not surface the error to the caller
