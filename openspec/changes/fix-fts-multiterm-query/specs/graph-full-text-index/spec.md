## Purpose

Defines how the graph-object full-text search recovers from an AND-semantics query that
matches nothing because its terms are spread across many objects, without trading away
single-term precision.

## ADDED Requirements

### Requirement: A multi-term query falls back to an OR disjunction once

The graph-object full-text search SHALL run the strict query (`websearch_to_tsquery`, AND
semantics) first and, when it returns no rows on the first page, SHALL retry once with the
identifier-relaxed form (`ftsquery.Relax`). Only when the relaxed form also matches nothing
SHALL it retry once with the terms OR-joined (`ftsquery.Disjoin`), matched with `to_tsquery`
(OR semantics) against both the `simple` and `norwegian` configurations and ranked by the
greater cover-density score (`ts_rank_cd`), so objects covering more of the query's terms
rank higher.

The disjoined fallback SHALL NOT run when fewer than two letter-bearing terms survive, when
the query carries `websearch_to_tsquery` operator syntax (phrase, negation, or explicit OR)
that an OR-disjunction would strip or invert, or when paginating past the first page.

#### Scenario: A natural multi-term query with spread terms returns results

- **WHEN** no single object contains every term of a natural multi-term query
- **THEN** the strict AND query returns no rows
- **AND** the disjoined OR fallback returns the objects that match any term, ordered by term
  coverage

#### Scenario: Ordering rewards term coverage

- **WHEN** the disjoined fallback matches objects covering different numbers of query terms
- **THEN** an object covering more terms MUST rank above an object covering fewer

#### Scenario: A single-term query is not widened

- **WHEN** a single-term query already matches under the strict AND query
- **THEN** the disjoined fallback MUST NOT run, and the strict result MUST be returned verbatim

#### Scenario: The fallback is gated to the first page

- **WHEN** a request pages past every strict match (`Offset` > 0)
- **THEN** the disjoined fallback MUST NOT refill that page with matches the strict query
  never ranked
