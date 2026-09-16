# search-similarity-threshold Specification

## Purpose
Lets every search path (unified search, graph search, chunk text search, relationship search) drop results whose final fused similarity score falls below a caller-supplied `min_score`, instead of force-filling a default candidate floor with weak matches. The threshold is validated to the `[0, 1]` range, applied to each candidate set before fusion and again to the fused results, and omitting it preserves pre-change behavior.

## Requirements

### Requirement: Minimum similarity-score cutoff on search
Every search path (unified search, graph search, chunk text search, relationship search) SHALL support an optional minimum similarity-score threshold. When a threshold is set, results whose final fused score falls below it MUST be dropped from the response instead of returned.

#### Scenario: All candidates below threshold return empty
- **WHEN** a search is run with `min_score=0.75`
- **AND** every candidate scores below `0.75`
- **THEN** the response MUST contain zero results
- **AND** MUST NOT force-fill candidates to satisfy the default result floor

#### Scenario: Threshold filters noise but preserves strong matches
- **WHEN** a search is run with `min_score=0.6`
- **AND** 10 candidates score above `0.6` and 40 score below
- **THEN** the response MUST contain only the 10 above-threshold candidates (bounded by the request limit)

#### Scenario: Omitted threshold preserves baseline behavior
- **WHEN** `min_score` is omitted or zero
- **THEN** results MUST be identical to pre-change behavior (no filtering, existing floors apply)

#### Scenario: Threshold applies to each search mode independently
- **WHEN** a unified search with `mode=both` sets `min_score`
- **THEN** graph, text, and relationship candidate sets MUST each be filtered by `min_score` before fusion
- **AND** fused results MUST also respect `min_score`

### Requirement: Score threshold is configurable and clamp-validated
The threshold SHALL be a float in the range `[0, 1]`. Values outside the range MUST be rejected or clamped with a validation error, never silently coerced to a surprising value.

#### Scenario: Out-of-range threshold rejected
- **WHEN** a request provides `min_score=1.5` or `min_score=-0.2`
- **THEN** the server MUST return a validation error (HTTP 400) with a clear message
