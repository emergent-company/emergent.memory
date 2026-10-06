# mcp-registry Specification

## Purpose
Harden the official MCP registry browse path against a slow or transiently
degraded upstream: tolerate latency within a bounded budget, serve repeated
identical browse queries from a short-lived cache, and still surface failures
as errors rather than an empty success.

## Requirements

### Requirement: Registry browse tolerates a slow upstream within a bounded budget

Memory SHALL give the official MCP registry a request budget long enough to
cover its observed slow-but-successful latency (~38.5s) while staying under the
gateway's 60s client timeout. A response that arrives within that budget SHALL
be returned to the caller rather than being reported as an upstream failure.

#### Scenario: Slow-but-successful upstream response is returned

- **WHEN** the official registry returns HTTP 200 only after ~38.5s
- **THEN** Memory returns the results instead of failing with a 502

#### Scenario: Upstream exceeds the budget

- **WHEN** the official registry does not respond within the bounded budget
- **THEN** Memory reports a clear upstream error and does not return an empty success

### Requirement: Repeated identical browse queries are served from a short-lived cache

Memory SHALL cache successful registry browse results (search and single-server
lookup) in memory, keyed by the full request identity — search keyed by query,
limit, and cursor; lookup keyed by name — for a short TTL (~60s). Within the
TTL, an identical query SHALL be served from the cache without re-hitting the
origin. Only successful results SHALL be cached.

#### Scenario: Identical search is served from cache

- **WHEN** a client repeats the same search (same query, limit, and cursor) within the TTL
- **THEN** the result is returned from the cache and the upstream registry is not called again

#### Scenario: Pagination hits the origin per cursor

- **WHEN** a client requests a different page (different cursor) of a search
- **THEN** Memory fetches that page from the upstream registry

#### Scenario: Transient failure is not sticky

- **WHEN** a browse request fails (upstream error) and then succeeds on a later retry
- **THEN** the error is not cached and the later retry returns the successful result

#### Scenario: Failure is still surfaced as an error

- **WHEN** the official registry is unreachable or returns an error
- **THEN** Memory reports the failure clearly and does not return a successful empty result
