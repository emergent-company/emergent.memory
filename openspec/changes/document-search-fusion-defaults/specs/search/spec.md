## Purpose

State the default search fusion semantics shipped by #1002 so the effective behaviour — especially the relationship leg's exclusion from weighted fusion when its weight is unset — is discoverable from the specs, and so an operator can restore the pre-#1002 behaviour without a code change (Refs #1002, #996).

## ADDED Requirements

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
