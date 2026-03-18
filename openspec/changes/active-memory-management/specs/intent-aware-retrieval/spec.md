## ADDED Requirements

### Requirement: recall_memories supports intent-aware retrieval when enabled
When the feature flag `MEMORY_INTENT_AWARE_RETRIEVAL=true` is set, `recall_memories` SHALL classify the query intent before selecting a retrieval strategy, rather than always using the default hybrid strategy.

#### Scenario: Intent-aware retrieval disabled — default hybrid search used
- **WHEN** `MEMORY_INTENT_AWARE_RETRIEVAL=false` (default)
- **AND** `recall_memories` is called
- **THEN** the system SHALL use the standard hybrid (FTS + vector) search strategy
- **THEN** no intent classification LLM call SHALL be made

#### Scenario: Intent-aware retrieval enabled — intent classified before search
- **WHEN** `MEMORY_INTENT_AWARE_RETRIEVAL=true`
- **AND** `recall_memories` is called with query "How did I solve that memory leak last month?"
- **THEN** the system SHALL classify the query intent as `CHRONOLOGICAL` or `ANALYTICAL`
- **THEN** the system SHALL apply the corresponding retrieval strategy (date-range filter + keyword)
- **THEN** the response SHALL include an `intent` field indicating the classified type

### Requirement: Intent classification produces a structured retrieval plan
The intent classification step SHALL produce a structured retrieval plan with: intent type, entity hints, time range (if applicable), and recommended search strategy.

#### Scenario: PREFERENCE query classified correctly
- **WHEN** the query is "what are the user's TypeScript preferences?"
- **THEN** intent type SHALL be `PREFERENCE`
- **THEN** the retrieval plan SHALL include `category_filter: ["preference", "convention"]`
- **THEN** the search SHALL be semantic-primary with category filter applied

#### Scenario: CHRONOLOGICAL query classified correctly
- **WHEN** the query is "what happened last week with the deployment?"
- **THEN** intent type SHALL be `CHRONOLOGICAL`
- **THEN** the retrieval plan SHALL include a date range filter: `event_time >= now - 7 days`
- **THEN** keyword matching on entity hints SHALL be applied before semantic ranking

#### Scenario: INSTRUCTIONAL query classified correctly
- **WHEN** the query is "how should I handle errors in this codebase?"
- **THEN** intent type SHALL be `INSTRUCTIONAL`
- **THEN** the retrieval plan SHALL prioritize `category = instruction` and `category = convention` memories first
- **THEN** semantic search across all categories SHALL supplement if fewer than 3 instructional memories found

#### Scenario: Intent classification failure falls back to hybrid search
- **WHEN** the intent classification LLM call fails or times out
- **THEN** the system SHALL fall back to standard hybrid search
- **THEN** the failure SHALL be logged
- **THEN** the response SHALL not include an `intent` field

### Requirement: Intent classification respects the same user scoping as standard recall
The intent classification step SHALL NOT leak memory content or user IDs across user boundaries.

#### Scenario: Query context does not expose other users' memories
- **WHEN** intent classification is performed for user A
- **THEN** the classification prompt SHALL only receive the query text, NOT other users' memory content
- **THEN** entity hints in the retrieval plan SHALL only be matched against the current user's memories
