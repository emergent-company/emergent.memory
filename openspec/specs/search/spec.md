# search Specification

## Purpose
State the default search fusion semantics shipped by #1002 so the effective behaviour — especially the relationship leg's exclusion from weighted fusion when its weight is unset — is discoverable from the specs, and so an operator can restore the pre-#1002 behaviour without a code change (Refs #1002, #996). Also state the query-embedding bound and its lexical-only fallback, so a slow or hung embedding provider cannot silently stall or hard-fail unified search (Refs #1392).

## Requirements

### Requirement: Weighted fusion excludes the relationship leg when unweighted

Weighted fusion is the default strategy. When the relationship weight is unset (or `0`), the relationship leg SHALL NOT be promoted to `graphWeight`: relationship candidates score `0` and sort last, remain present in `Results`, and are truncated only past `limit`.

#### Scenario: Unweighted relationship candidates sort last

- **WHEN** a weighted-fusion search runs with no relationship weight set
- **AND** graph, text, and relationship candidates all match
- **THEN** relationship candidates MUST score `0` and sort after all positively-scored graph and text candidates
- **AND** relationship candidates MUST remain in the response, truncated only past `limit`

#### Scenario: Relationship endpoints are not injected when unweighted

- **WHEN** a weighted-fusion search runs with no relationship weight set
- **THEN** relationship src/dst endpoint node stubs MUST NOT be injected into the graph candidate pool

### Requirement: Explicit per-request relationship weight re-enables the relationship leg

An explicit per-request `relationshipWeight > 0` SHALL make the relationship leg a first-class contributor: graph, text, and relationship weights are normalised three-way, and relationship candidates score proportionally to their similarity.

#### Scenario: Explicit weight ranks a strong relationship hit

- **WHEN** a request sets `relationshipWeight=0.5` with default graph (`0.25`) and text (`0.75`) weights
- **AND** a relationship candidate has similarity `0.729`
- **THEN** its fused score SHALL be approximately `0.729 × 0.5 / 1.5 = 0.243` under the three-way normalisation

#### Scenario: Explicit weight restores endpoint injection

- **WHEN** a request sets `relationshipWeight > 0`
- **THEN** relationship src/dst endpoint node stubs SHALL be injected into the graph candidate pool

### Requirement: Relationship weight is configurable by environment

The relationship weight SHALL default to `0` and SHALL be overridable via the `SEARCH_RELATIONSHIP_WEIGHT` environment variable (read in `NewService` via `envFloatDefault`). Setting it above `0` SHALL restore relationship contributions without any code change.

#### Scenario: Operator restores the old behaviour

- **WHEN** the server starts with `SEARCH_RELATIONSHIP_WEIGHT` set to a positive value
- **THEN** weighted fusion SHALL treat the relationship leg as a first-class contributor, as before #1002

#### Scenario: Unset env preserves the new default

- **WHEN** `SEARCH_RELATIONSHIP_WEIGHT` is unset
- **THEN** the relationship weight SHALL default to `0`, excluding the relationship leg from weighted fusion

### Requirement: Non-weighted fusion strategies are unaffected

The `rrf`, `interleave`, `graph_first`, and `text_first` strategies SHALL behave independently of the relationship weight: `rrf` SHALL rank-fuse relationships positively (reciprocal-rank fusion across graph, text, and relationship sets), while `interleave` / `graph_first` / `text_first` SHALL surface relationships directly without weighting.

#### Scenario: rrf still fuses relationships

- **WHEN** a search runs with `fusionStrategy=rrf`
- **THEN** relationship candidates SHALL be rank-fused alongside graph and text candidates

#### Scenario: Weightless strategies ignore the relationship weight

- **WHEN** a search runs with `interleave`, `graph_first`, or `text_first`
- **THEN** relationship candidates SHALL surface directly and MUST NOT be scored via `relationshipWeight`

### Requirement: Query embedding is bounded and degrades to lexical-only

Unified search SHALL bound each query-embedding call it issues with `queryEmbedTimeout = 20s`. The bound covers two sequential stages: the shared pre-computed query embedding, and — when the shared embedding yields no vector — one bounded re-embed per enabled leg (graph, text, and relationship), which run in parallel. A slow provider can therefore add a second bounded wait, so the search's own embedding stages settle after roughly at most 2×`queryEmbedTimeout`, not a single 20-second stage. When a leg is left without a query vector, it SHALL proceed without one: the text leg SHALL serve lexical (full-text) results only, the graph leg SHALL remain available by delegating to graph hybrid search with no query vector (yielding lexical full-text results when no vector can be produced), and the vector-dependent relationship leg SHALL produce no candidates — rather than waiting indefinitely on the provider or failing the search solely because an embedding call failed or exceeded the bound. A transient shared-embedding failure that succeeds on a leg's re-embed yields that leg's hybrid (lexical + vector) results, not lexical-only. This is the same fallback the search already applied on embedding error (the timeout path and the error path are indistinguishable to the caller).

#### Scenario: Slow provider adds a second bounded wait, not a single stage

- **WHEN** the shared query-embedding call does not return within `queryEmbedTimeout` (20 seconds)
- **THEN** unified search SHALL stop waiting on that call at the bound and issue one bounded re-embed per enabled leg (graph, text, and relationship), running in parallel
- **AND** the search's embedding stages SHALL settle after roughly at most 2×`queryEmbedTimeout`
- **AND** the search SHALL NOT fail solely because an embedding call failed or exceeded the bound

#### Scenario: Per-leg re-embed recovers hybrid results after a transient shared failure

- **WHEN** the shared query-embedding call fails or exceeds the bound
- **AND** a leg's bounded re-embed then succeeds
- **THEN** that leg SHALL serve hybrid (lexical + vector) results — not lexical-only — and the vector-dependent relationship leg SHALL return candidates

#### Scenario: A failed per-leg re-embed degrades only that leg

- **WHEN** both the shared embedding and a leg's re-embed yield no query vector
- **THEN** the text leg SHALL serve lexical (full-text) results only, exactly as the pre-existing embedding-error fallback did
- **AND** the graph leg SHALL remain available via graph hybrid search with no query vector, yielding lexical (full-text) results when no vector can be produced
- **AND** the vector-dependent relationship leg SHALL produce no candidates
